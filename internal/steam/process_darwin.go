//go:build darwin

package steam

import "os/exec"

// IsSteamRunning uses pgrep to detect the macOS Steam client
// (process is "steam_osx" / "Steam").
func IsSteamRunning() bool {
	for _, name := range []string{"steam_osx", "Steam"} {
		if err := exec.Command("pgrep", "-x", name).Run(); err == nil {
			return true
		}
	}
	return false
}

// KillSteam asks the Steam client to quit.
func KillSteam() error {
	_ = exec.Command("pkill", "-x", "steam_osx").Run()
	_ = exec.Command("osascript", "-e", `quit app "Steam"`).Run()
	return nil
}
