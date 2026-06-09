// Package vdf parses Valve's KeyValues text format (a.k.a. VDF).
//
// Only the *text* form is handled — this is what loginusers.vdf,
// libraryfolders.vdf, appmanifest_<id>.acf and localconfig.vdf use. The binary
// appinfo.vdf is intentionally NOT supported.
//
// Grammar (informal):
//
//	node    := { pair }
//	pair    := key value
//	key     := string
//	value   := string | "{" node "}"
//	string  := '"' ... '"' | bareword
//
// Line comments (//) are ignored. Platform conditionals such as [$WIN32] that
// may trail a value are skipped. Keys are case-insensitive in Steam's own
// reader; callers that need that should use the Get* helpers which lower-case.
package vdf

import (
	"fmt"
	"strings"
)

// Node is a parsed KeyValues object. A value is either a string (leaf) or a
// *Node (subtree). Insertion order is preserved in Keys for stable iteration.
type Node struct {
	vals map[string]any // string | *Node
	Keys []string       // keys in the order first seen
}

func newNode() *Node { return &Node{vals: map[string]any{}} }

func (n *Node) set(key string, v any) {
	if _, ok := n.vals[key]; !ok {
		n.Keys = append(n.Keys, key)
	}
	n.vals[key] = v
}

// Get returns the raw value for key (case-sensitive) and whether it existed.
func (n *Node) Get(key string) (any, bool) {
	if n == nil {
		return nil, false
	}
	v, ok := n.vals[key]
	return v, ok
}

// GetString returns a leaf string value, case-insensitively.
func (n *Node) GetString(key string) (string, bool) {
	if n == nil {
		return "", false
	}
	for _, k := range n.Keys {
		if strings.EqualFold(k, key) {
			if s, ok := n.vals[k].(string); ok {
				return s, true
			}
		}
	}
	return "", false
}

// Child returns a subtree by key, case-insensitively.
func (n *Node) Child(key string) (*Node, bool) {
	if n == nil {
		return nil, false
	}
	for _, k := range n.Keys {
		if strings.EqualFold(k, key) {
			if c, ok := n.vals[k].(*Node); ok {
				return c, true
			}
		}
	}
	return nil, false
}

// Children iterates subtrees in insertion order, calling fn(key, node). Leaf
// (string) entries are skipped.
func (n *Node) Children(fn func(key string, node *Node)) {
	if n == nil {
		return
	}
	for _, k := range n.Keys {
		if c, ok := n.vals[k].(*Node); ok {
			fn(k, c)
		}
	}
}

// Parse reads a complete KeyValues document.
func Parse(data string) (*Node, error) {
	p := &parser{src: data}
	root := newNode()
	if err := p.parseInto(root, true); err != nil {
		return nil, err
	}
	return root, nil
}

type parser struct {
	src string
	pos int
}

// parseInto reads key/value pairs into n until EOF (top==true) or a closing
// brace (top==false).
func (p *parser) parseInto(n *Node, top bool) error {
	for {
		tok, kind, ok := p.next()
		if !ok {
			if top {
				return nil
			}
			return fmt.Errorf("vdf: unexpected EOF inside object")
		}
		switch kind {
		case tokClose:
			if top {
				return fmt.Errorf("vdf: unexpected '}' at top level")
			}
			return nil
		case tokOpen:
			return fmt.Errorf("vdf: unexpected '{' where a key was expected")
		}
		key := tok

		// Skip any trailing conditional on the key line (rare).
		vtok, vkind, ok := p.next()
		if !ok {
			return fmt.Errorf("vdf: key %q has no value", key)
		}
		switch vkind {
		case tokOpen:
			child := newNode()
			if err := p.parseInto(child, false); err != nil {
				return err
			}
			n.set(key, child)
		case tokString:
			n.set(key, vtok)
			// Swallow an optional platform conditional like [$WIN32].
			p.skipConditional()
		case tokClose:
			return fmt.Errorf("vdf: key %q followed by '}'", key)
		}
	}
}

type tokKind int

const (
	tokString tokKind = iota
	tokOpen
	tokClose
)

func (p *parser) skipConditional() {
	save := p.pos
	tok, kind, ok := p.next()
	if ok && kind == tokString && strings.HasPrefix(tok, "[") {
		return // consumed the conditional
	}
	p.pos = save // not a conditional; put it back
}

// next returns the next token. tokString covers both quoted and bare words.
func (p *parser) next() (string, tokKind, bool) {
	for p.pos < len(p.src) {
		c := p.src[p.pos]
		switch {
		case c == ' ' || c == '\t' || c == '\r' || c == '\n':
			p.pos++
		case c == '/' && p.pos+1 < len(p.src) && p.src[p.pos+1] == '/':
			for p.pos < len(p.src) && p.src[p.pos] != '\n' {
				p.pos++
			}
		case c == '{':
			p.pos++
			return "{", tokOpen, true
		case c == '}':
			p.pos++
			return "}", tokClose, true
		case c == '"':
			return p.quoted()
		default:
			return p.bareword()
		}
	}
	return "", tokString, false
}

func (p *parser) quoted() (string, tokKind, bool) {
	p.pos++ // opening quote
	var b strings.Builder
	for p.pos < len(p.src) {
		c := p.src[p.pos]
		if c == '\\' && p.pos+1 < len(p.src) {
			n := p.src[p.pos+1]
			switch n {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case '\\':
				b.WriteByte('\\')
			case '"':
				b.WriteByte('"')
			default:
				b.WriteByte(n)
			}
			p.pos += 2
			continue
		}
		if c == '"' {
			p.pos++
			return b.String(), tokString, true
		}
		b.WriteByte(c)
		p.pos++
	}
	return b.String(), tokString, true // unterminated; tolerate
}

func (p *parser) bareword() (string, tokKind, bool) {
	start := p.pos
	for p.pos < len(p.src) {
		c := p.src[p.pos]
		if c == ' ' || c == '\t' || c == '\r' || c == '\n' || c == '{' || c == '}' || c == '"' {
			break
		}
		p.pos++
	}
	return p.src[start:p.pos], tokString, true
}
