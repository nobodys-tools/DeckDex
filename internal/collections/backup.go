package collections

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/nobodys-tools/DeckDex/internal/paths"
)

// LoadFile reads and parses the collections file at path. A missing file is
// treated as an empty namespace (not an error).
func LoadFile(path string) (*Namespace, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return Parse(nil)
	}
	if err != nil {
		return nil, err
	}
	return Parse(data)
}

// Backup copies path into backupDir with a timestamped name, then prunes to the
// newest keepN backups. now provides the timestamp (no Date.now in callers).
// Returns the backup path written ("" if the source file does not exist yet).
func Backup(path, backupDir string, keepN int, now time.Time) (string, error) {
	src, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", nil // nothing to back up on first write
	}
	if err != nil {
		return "", err
	}
	if err := paths.EnsureDir(backupDir); err != nil {
		return "", err
	}
	name := fmt.Sprintf("cloud-storage-namespace-1.%s.json", now.UTC().Format("20060102T150405Z"))
	dst := filepath.Join(backupDir, name)
	if err := paths.WriteFileAtomic(dst, src, 0o644); err != nil {
		return "", err
	}
	pruneBackups(backupDir, keepN)
	return dst, nil
}

// pruneBackups keeps only the newest keepN backup files.
func pruneBackups(dir string, keepN int) {
	if keepN <= 0 {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var names []string
	for _, e := range entries {
		n := e.Name()
		if strings.HasPrefix(n, "cloud-storage-namespace-1.") && strings.HasSuffix(n, ".json") {
			names = append(names, n)
		}
	}
	if len(names) <= keepN {
		return
	}
	sort.Strings(names) // timestamp names sort chronologically
	for _, n := range names[:len(names)-keepN] {
		_ = os.Remove(filepath.Join(dir, n))
	}
}

// LatestBackup returns the newest backup path in backupDir, or "" if none.
func LatestBackup(backupDir string) string {
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		return ""
	}
	var names []string
	for _, e := range entries {
		n := e.Name()
		if strings.HasPrefix(n, "cloud-storage-namespace-1.") && strings.HasSuffix(n, ".json") {
			names = append(names, n)
		}
	}
	if len(names) == 0 {
		return ""
	}
	sort.Strings(names)
	return filepath.Join(backupDir, names[len(names)-1])
}

// WriteFile writes the namespace to path atomically.
func (ns *Namespace) WriteFile(path string) error {
	data, err := ns.Bytes()
	if err != nil {
		return err
	}
	if err := paths.EnsureDir(filepath.Dir(path)); err != nil {
		return err
	}
	return paths.WriteFileAtomic(path, data, 0o644)
}

// Restore copies backupPath over targetPath atomically.
func Restore(backupPath, targetPath string) error {
	data, err := os.ReadFile(backupPath)
	if err != nil {
		return err
	}
	return paths.WriteFileAtomic(targetPath, data, 0o644)
}
