package steam

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/nobodys-tools/DeckDex/internal/vdf"
)

// OwnedGame is a minimal owned/installed game record.
type OwnedGame struct {
	AppID uint32
	Name  string
}

// LocalOwnedGames returns the fullest owned set obtainable from local files,
// without any network call. It unions:
//   - installed games from steamapps/appmanifest_*.acf across every library
//     folder listed in libraryfolders.vdf, and
//   - owned-with-playtime AppIDs from userdata/<id>/config/localconfig.vdf.
//
// The result can still be incomplete versus the Steam Web API owned list
// (it misses owned-but-never-installed, never-played games).
func (r *Root) LocalOwnedGames(acc *Account) ([]OwnedGame, error) {
	byID := map[uint32]string{}

	for _, folder := range r.libraryFolders() {
		steamapps := filepath.Join(folder, "steamapps")
		matches, _ := filepath.Glob(filepath.Join(steamapps, "appmanifest_*.acf"))
		for _, m := range matches {
			if id, name, ok := parseAppManifest(m); ok {
				if _, seen := byID[id]; !seen || name != "" {
					byID[id] = name
				}
			}
		}
	}

	for _, id := range r.localConfigAppIDs(acc) {
		if _, ok := byID[id]; !ok {
			byID[id] = ""
		}
	}

	out := make([]OwnedGame, 0, len(byID))
	for id, name := range byID {
		out = append(out, OwnedGame{AppID: id, Name: name})
	}
	return out, nil
}

// libraryFolders returns every library folder path from
// steamapps/libraryfolders.vdf, always including the root's own steamapps.
func (r *Root) libraryFolders() []string {
	folders := []string{r.Path}
	path := filepath.Join(r.Path, "steamapps", "libraryfolders.vdf")
	data, err := os.ReadFile(path)
	if err != nil {
		return folders
	}
	root, err := vdf.Parse(string(data))
	if err != nil {
		return folders
	}
	lf, ok := root.Child("libraryfolders")
	if !ok {
		return folders
	}
	lf.Children(func(_ string, entry *vdf.Node) {
		if p, ok := entry.GetString("path"); ok && p != "" {
			folders = append(folders, p)
		}
	})
	return folders
}

// parseAppManifest extracts appid and name from an appmanifest_<id>.acf file.
func parseAppManifest(path string) (uint32, string, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, "", false
	}
	root, err := vdf.Parse(string(data))
	if err != nil {
		return 0, "", false
	}
	app, ok := root.Child("AppState")
	if !ok {
		return 0, "", false
	}
	idStr, ok := app.GetString("appid")
	if !ok {
		return 0, "", false
	}
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		return 0, "", false
	}
	name, _ := app.GetString("name")
	return uint32(id), name, true
}

// localConfigAppIDs reads AppIDs from localconfig.vdf's apps map
// (UserLocalConfigStore > Software > Valve > Steam > apps).
func (r *Root) localConfigAppIDs(acc *Account) []uint32 {
	path := filepath.Join(r.UserConfigDir(acc), "localconfig.vdf")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	root, err := vdf.Parse(string(data))
	if err != nil {
		return nil
	}
	node := root
	for _, key := range []string{"UserLocalConfigStore", "Software", "Valve", "Steam", "apps"} {
		c, ok := node.Child(key)
		if !ok {
			// Valve sometimes capitalises differently; Child is case-insensitive
			// already, so a miss here means the path genuinely differs.
			return nil
		}
		node = c
	}
	var ids []uint32
	node.Children(func(idStr string, _ *vdf.Node) {
		if id, err := strconv.ParseUint(strings.TrimSpace(idStr), 10, 32); err == nil {
			ids = append(ids, uint32(id))
		}
	})
	return ids
}
