package vdf

import "testing"

func TestParseLoginUsers(t *testing.T) {
	src := `
"users"
{
	"76561198012345678"
	{
		"AccountName"		"tester"
		"PersonaName"		"Tester McTest"
		"MostRecent"		"1"
	}
}
`
	root, err := Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	users, ok := root.Child("users")
	if !ok {
		t.Fatal("missing users block")
	}
	u, ok := users.Child("76561198012345678")
	if !ok {
		t.Fatal("missing user")
	}
	if name, _ := u.GetString("PersonaName"); name != "Tester McTest" {
		t.Errorf("PersonaName = %q", name)
	}
	if mr, _ := u.GetString("mostrecent"); mr != "1" { // case-insensitive
		t.Errorf("MostRecent = %q", mr)
	}
}

func TestParseCommentsAndConditionals(t *testing.T) {
	src := `
"AppState" // a comment
{
	"appid"   "413150"
	"name"    "Stardew Valley" [$WIN32]
}
`
	root, err := Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	app, _ := root.Child("AppState")
	if id, _ := app.GetString("appid"); id != "413150" {
		t.Errorf("appid = %q", id)
	}
	if name, _ := app.GetString("name"); name != "Stardew Valley" {
		t.Errorf("name = %q (conditional not skipped?)", name)
	}
}

func TestChildrenOrder(t *testing.T) {
	src := `"root" { "a" "1" "b" { "x" "y" } "c" "3" }`
	root, _ := Parse(src)
	r, _ := root.Child("root")
	var keys []string
	r.Children(func(k string, _ *Node) { keys = append(keys, k) })
	if len(keys) != 1 || keys[0] != "b" {
		t.Errorf("Children should yield only subtrees in order, got %v", keys)
	}
}
