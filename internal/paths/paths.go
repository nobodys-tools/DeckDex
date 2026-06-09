// Package paths resolves the per-OS directories DeckDex uses for its own
// config, cache and state, plus small filesystem helpers.
package paths

import (
	"os"
	"path/filepath"
)

const appName = "deckdex"

// ConfigDir is where config.toml is looked for by default
// (~/.config/deckdex, %AppData%\deckdex, ~/Library/Application Support/deckdex).
func ConfigDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, appName), nil
}

// CacheDir holds the tier/native cache ($XDG_CACHE_HOME, ~/Library/Caches,
// %LocalAppData%).
func CacheDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, appName), nil
}

// StateDir holds the managed-collections state file and backups. There is no
// stdlib UserStateDir, so reuse the config base (kept separate by subdir).
func StateDir() (string, error) {
	if x := os.Getenv("XDG_STATE_HOME"); x != "" {
		return filepath.Join(x, appName), nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, appName, "state"), nil
}

// EnsureDir creates dir (and parents) if missing.
func EnsureDir(dir string) error {
	return os.MkdirAll(dir, 0o755)
}

// DefaultConfigPath is ConfigDir()/config.toml.
func DefaultConfigPath() (string, error) {
	d, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "config.toml"), nil
}

// WriteFileAtomic writes data to path via a temp file in the same directory
// followed by a rename, so readers never observe a partial file.
func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once renamed
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
