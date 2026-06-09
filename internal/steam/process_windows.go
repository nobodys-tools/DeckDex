//go:build windows

package steam

import (
	"os/exec"
	"strings"
)

// IsSteamRunning checks the Windows task list for steam.exe.
func IsSteamRunning() bool {
	out, err := exec.Command("tasklist", "/FI", "IMAGENAME eq steam.exe", "/NH").Output()
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(string(out)), "steam.exe")
}

// KillSteam terminates steam.exe.
func KillSteam() error {
	return exec.Command("taskkill", "/IM", "steam.exe", "/F").Run()
}
