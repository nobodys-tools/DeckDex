//go:build windows

package steam

import (
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// registrySteamPaths reads the Steam install path from the Windows registry,
// preferring HKCU\Software\Valve\Steam\SteamPath then
// HKLM\SOFTWARE\WOW6432Node\Valve\Steam\InstallPath. Slashes are normalised.
func registrySteamPaths() []string {
	var out []string
	add := func(p string) {
		p = strings.TrimSpace(p)
		if p == "" {
			return
		}
		out = append(out, filepath.FromSlash(strings.ReplaceAll(p, `/`, `\`)))
	}

	if k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Valve\Steam`, registry.QUERY_VALUE); err == nil {
		if v, _, err := k.GetStringValue("SteamPath"); err == nil {
			add(v)
		}
		k.Close()
	}
	if k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\WOW6432Node\Valve\Steam`, registry.QUERY_VALUE); err == nil {
		if v, _, err := k.GetStringValue("InstallPath"); err == nil {
			add(v)
		}
		k.Close()
	}
	return out
}
