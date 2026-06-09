//go:build !windows

package steam

// registrySteamPaths is a no-op off Windows; this keeps the discovery code in
// discover.go platform-agnostic while the registry dependency stays behind a
// build tag so non-Windows builds compile without golang.org/x/sys/windows.
func registrySteamPaths() []string { return nil }
