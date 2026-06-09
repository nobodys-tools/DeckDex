package steam

import "testing"

func TestSteamIDConversion(t *testing.T) {
	const accountID uint32 = 52079950
	const steamID64 uint64 = 76561198012345678

	if got := AccountIDToSteamID64(accountID); got != steamID64 {
		t.Errorf("AccountIDToSteamID64(%d) = %d, want %d", accountID, got, steamID64)
	}
	if got := SteamID64ToAccountID(steamID64); got != accountID {
		t.Errorf("SteamID64ToAccountID(%d) = %d, want %d", steamID64, got, accountID)
	}
	// Round-trip.
	if got := SteamID64ToAccountID(AccountIDToSteamID64(accountID)); got != accountID {
		t.Errorf("round-trip = %d, want %d", got, accountID)
	}
}
