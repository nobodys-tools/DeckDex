//go:build linux

package steam

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// steamRunning scans /proc for a process named exactly "steam" and returns the
// matching PIDs.
func steamPIDs() []int {
	var pids []int
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		comm, err := os.ReadFile(filepath.Join("/proc", e.Name(), "comm"))
		if err != nil {
			continue
		}
		if strings.TrimSpace(string(comm)) == "steam" {
			pids = append(pids, pid)
		}
	}
	return pids
}

// IsSteamRunning reports whether a Steam client process is active.
func IsSteamRunning() bool { return len(steamPIDs()) > 0 }

// KillSteam sends SIGTERM to running Steam processes.
func KillSteam() error {
	for _, pid := range steamPIDs() {
		_ = syscall.Kill(pid, syscall.SIGTERM)
	}
	return nil
}
