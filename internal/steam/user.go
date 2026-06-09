package steam

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/nobodys-tools/DeckDex/internal/vdf"
)

// steamID64Base is the constant offset between a 32-bit account ID and the
// 64-bit community ID: SteamID64 = AccountID + 76561197960265728.
const steamID64Base uint64 = 76561197960265728

// Account identifies a Steam user. AccountID is the userdata/<id> folder name;
// SteamID64 is the community/Web-API id.
type Account struct {
	AccountID   uint32 // 32-bit, == userdata folder name
	SteamID64   uint64 // 64-bit community id
	AccountName string // login name (may be empty)
	PersonaName string // display name (may be empty)
}

// AccountID64 derives the SteamID64 from a 32-bit account id.
func AccountIDToSteamID64(accountID uint32) uint64 {
	return uint64(accountID) + steamID64Base
}

// SteamID64ToAccountID derives the 32-bit account id from a SteamID64.
func SteamID64ToAccountID(steamID64 uint64) uint32 {
	return uint32(steamID64 - steamID64Base)
}

// ResolveAccount determines the active Steam account using, in order:
//  1. explicit configAccountID (folder name) or configSteamID64,
//  2. the MostRecent entry in loginusers.vdf,
//  3. the sole numeric directory under userdata/ (error if ambiguous).
func (r *Root) ResolveAccount(configAccountID, configSteamID64 string) (*Account, error) {
	if configAccountID != "" {
		id, err := strconv.ParseUint(configAccountID, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("steam: invalid account_id %q: %w", configAccountID, err)
		}
		return &Account{AccountID: uint32(id), SteamID64: AccountIDToSteamID64(uint32(id))}, nil
	}
	if configSteamID64 != "" {
		id, err := strconv.ParseUint(configSteamID64, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("steam: invalid steam_id64 %q: %w", configSteamID64, err)
		}
		return &Account{AccountID: SteamID64ToAccountID(id), SteamID64: id}, nil
	}
	if acc, err := r.accountFromLoginUsers(); err == nil && acc != nil {
		return acc, nil
	}
	return r.accountFromUserdata()
}

// accountFromLoginUsers reads config/loginusers.vdf and returns the MostRecent
// account, or the only account if none is flagged.
func (r *Root) accountFromLoginUsers() (*Account, error) {
	data, err := os.ReadFile(r.LoginUsersPath())
	if err != nil {
		return nil, err
	}
	root, err := vdf.Parse(string(data))
	if err != nil {
		return nil, err
	}
	users, ok := root.Child("users")
	if !ok {
		return nil, fmt.Errorf("steam: loginusers.vdf has no 'users' block")
	}

	var fallback *Account
	var chosen *Account
	users.Children(func(idStr string, u *vdf.Node) {
		id64, err := strconv.ParseUint(idStr, 10, 64)
		if err != nil {
			return
		}
		acc := &Account{
			SteamID64: id64,
			AccountID: SteamID64ToAccountID(id64),
		}
		acc.AccountName, _ = u.GetString("AccountName")
		acc.PersonaName, _ = u.GetString("PersonaName")
		if fallback == nil {
			fallback = acc
		}
		if v, _ := u.GetString("MostRecent"); v == "1" {
			chosen = acc
		}
	})
	if chosen != nil {
		return chosen, nil
	}
	if fallback != nil {
		return fallback, nil
	}
	return nil, fmt.Errorf("steam: no users in loginusers.vdf")
}

// accountFromUserdata enumerates numeric userdata/<id> directories.
func (r *Root) accountFromUserdata() (*Account, error) {
	entries, err := os.ReadDir(r.UserdataDir())
	if err != nil {
		return nil, fmt.Errorf("steam: cannot read userdata dir: %w", err)
	}
	var ids []uint32
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		id, err := strconv.ParseUint(e.Name(), 10, 32)
		if err != nil || id == 0 {
			continue // skip "ac", "0", "anonymous", etc.
		}
		ids = append(ids, uint32(id))
	}
	switch len(ids) {
	case 0:
		return nil, fmt.Errorf("steam: no user profiles found under %s", r.UserdataDir())
	case 1:
		return &Account{AccountID: ids[0], SteamID64: AccountIDToSteamID64(ids[0])}, nil
	default:
		return nil, fmt.Errorf("steam: multiple users under userdata/ (%v); set [steam].account_id or --account-id", ids)
	}
}

// UserConfigDir is <root>/userdata/<accountID>/config.
func (r *Root) UserConfigDir(acc *Account) string {
	return filepath.Join(r.UserdataDir(), strconv.FormatUint(uint64(acc.AccountID), 10), "config")
}

// CollectionsPath is the cloud-storage namespace file holding collections.
func (r *Root) CollectionsPath(acc *Account) string {
	return filepath.Join(r.UserConfigDir(acc), "cloudstorage", "cloud-storage-namespace-1.json")
}
