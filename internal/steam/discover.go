// Package steam handles local Steam installation discovery, identifying the
// active user, and reading owned/installed games from on-disk VDF files.
package steam

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// Root is a validated Steam installation directory.
type Root struct {
	Path string
}

// DiscoverRoot resolves the Steam root directory. If override is non-empty it is
// validated and used directly (erroring if invalid). Otherwise OS-specific
// candidate paths are checked in order; the first valid one wins.
func DiscoverRoot(override string) (*Root, error) {
	if override != "" {
		if valid(override) {
			return &Root{Path: override}, nil
		}
		return nil, fmt.Errorf("steam: --steam-path %q is not a valid Steam root (needs userdata/ and config/ or steamapps/)", override)
	}
	for _, c := range candidateRoots() {
		c = expand(c)
		if valid(c) {
			// Resolve symlinks (e.g. ~/.steam/steam) to a canonical path.
			if real, err := filepath.EvalSymlinks(c); err == nil {
				c = real
			}
			return &Root{Path: c}, nil
		}
	}
	return nil, fmt.Errorf("steam: no Steam installation found; pass --steam-path or set [steam].path in config")
}

// candidateRoots returns the OS-specific paths to probe, most-likely first.
func candidateRoots() []string {
	switch runtime.GOOS {
	case "linux":
		roots := []string{}
		if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
			roots = append(roots, filepath.Join(xdg, "Steam"))
		}
		roots = append(roots,
			"~/.local/share/Steam",
			"~/.steam/steam",
			"~/.steam/root",
			"~/.var/app/com.valvesoftware.Steam/data/Steam", // Flatpak
			"~/snap/steam/common/.local/share/Steam",        // Snap
		)
		return roots
	case "darwin":
		return []string{"~/Library/Application Support/Steam"}
	case "windows":
		// Registry first, then common install dirs.
		return append(registrySteamPaths(),
			`C:\Program Files (x86)\Steam`,
			`C:\Program Files\Steam`,
		)
	default:
		return nil
	}
}

// valid reports whether dir looks like a Steam root: it must contain userdata/
// and either config/ or steamapps/.
func valid(dir string) bool {
	if dir == "" {
		return false
	}
	if !isDir(filepath.Join(dir, "userdata")) {
		return false
	}
	return isDir(filepath.Join(dir, "config")) || isDir(filepath.Join(dir, "steamapps"))
}

func isDir(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

// expand resolves a leading ~ and environment variables in p.
func expand(p string) string {
	if len(p) > 0 && p[0] == '~' {
		if home, err := os.UserHomeDir(); err == nil {
			p = filepath.Join(home, p[1:])
		}
	}
	return os.ExpandEnv(p)
}

// ConfigDir is <root>/config.
func (r *Root) ConfigDir() string { return filepath.Join(r.Path, "config") }

// UserdataDir is <root>/userdata.
func (r *Root) UserdataDir() string { return filepath.Join(r.Path, "userdata") }

// LoginUsersPath is <root>/config/loginusers.vdf.
func (r *Root) LoginUsersPath() string { return filepath.Join(r.ConfigDir(), "loginusers.vdf") }
