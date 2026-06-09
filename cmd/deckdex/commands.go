package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/nobodys-tools/DeckDex/internal/cache"
	"github.com/nobodys-tools/DeckDex/internal/collections"
	"github.com/nobodys-tools/DeckDex/internal/config"
	"github.com/nobodys-tools/DeckDex/internal/engine"
	"github.com/nobodys-tools/DeckDex/internal/paths"
	"github.com/nobodys-tools/DeckDex/internal/protondb"
	"github.com/nobodys-tools/DeckDex/internal/state"
	"github.com/nobodys-tools/DeckDex/internal/steam"
)

// env bundles the resolved runtime context shared by most commands.
type env struct {
	g       globals
	cfg     config.Config
	root    *steam.Root
	account *steam.Account
	client  *protondb.Client
	now     time.Time
}

// parse builds a FlagSet with the common flags, parses args, and returns the
// globals plus any positional arguments.
func parse(name string, args []string) (globals, []string) {
	var g globals
	fs := flag.NewFlagSet(name, flag.ExitOnError)
	registerCommonFlags(fs, &g)
	_ = fs.Parse(args)
	return g, fs.Args()
}

// setup loads config and resolves the Steam root + account + ProtonDB client.
func setup(g globals) (*env, error) {
	cfgPath := g.configPath
	if cfgPath == "" {
		p, err := paths.DefaultConfigPath()
		if err != nil {
			return nil, err
		}
		cfgPath = p
	}
	cfg, _, err := config.Load(cfgPath)
	if err != nil {
		return nil, err
	}

	// CLI overrides win over config.
	if g.steamPath != "" {
		cfg.Steam.Path = g.steamPath
	}
	if g.accountID != "" {
		cfg.Steam.AccountID = g.accountID
	}
	if g.apiKey != "" {
		cfg.Steam.APIKey = g.apiKey
	} else if cfg.Steam.APIKey == "" {
		// Env var avoids exposing the key in argv (visible via `ps`) or shell history.
		if k := os.Getenv("DECKDEX_STEAM_API_KEY"); k != "" {
			cfg.Steam.APIKey = k
		}
	}
	if g.native {
		cfg.ProtonDB.DetectNative = true
	}
	if g.noNative { // explicit opt-out wins over config and auto-enable
		cfg.ProtonDB.DetectNative = false
	}
	if g.filterNonGames {
		cfg.ProtonDB.FilterNonGames = true
	}
	if g.maxRPS > 0 {
		cfg.ProtonDB.MaxRPS = g.maxRPS
	}
	if g.cacheTTL > 0 {
		cfg.ProtonDB.CacheTTLDays = g.cacheTTL
	}

	root, err := steam.DiscoverRoot(cfg.Steam.Path)
	if err != nil {
		return nil, err
	}
	acc, err := root.ResolveAccount(cfg.Steam.AccountID, cfg.Steam.SteamID64)
	if err != nil {
		return nil, err
	}
	client := protondb.New(protondb.Options{
		MaxRPS:         cfg.ProtonDB.MaxRPS,
		MaxConcurrency: cfg.ProtonDB.MaxConcurrency,
		Verbose:        g.verbose,
	})
	return &env{g: g, cfg: cfg, root: root, account: acc, client: client, now: time.Now()}, nil
}

// selectSpecs chooses the collection set: config collections take precedence;
// otherwise a --preset is used; otherwise an error.
func selectSpecs(e *env) ([]config.CollectionSpec, error) {
	if len(e.cfg.Collections) > 0 {
		if e.g.preset != "" {
			fmt.Println("note: --preset ignored because the config file defines [[collection]] blocks")
		}
		return e.cfg.Collections, nil
	}
	if e.g.preset != "" {
		return config.Preset(e.g.preset)
	}
	return nil, fmt.Errorf("no collections defined: add [[collection]] blocks to config or pass --preset <name>")
}

// specsUseNative reports whether any collection includes the 'native' category.
func specsUseNative(specs []config.CollectionSpec) bool {
	for _, s := range specs {
		for _, t := range s.Tiers {
			if t == "native" {
				return true
			}
		}
	}
	return false
}

// resolveLibrary fetches owned games and resolves their categories, returning
// the resolved set and saving the updated cache.
func (e *env) resolveLibrary(ctx context.Context) ([]engine.Resolved, engine.OwnedResult, error) {
	owned, err := engine.ResolveOwned(ctx, e.cfg, e.root, e.account)
	if err != nil {
		return nil, owned, err
	}
	c, err := cache.Open()
	if err != nil {
		return nil, owned, err
	}
	r := &engine.Resolver{
		Client:   e.client,
		Cache:    c,
		Cfg:      e.cfg,
		Now:      e.now,
		NoCache:  e.g.noCache,
		Progress: progressReporter(),
	}
	fmt.Printf("resolving tiers for %d games from %s ...\n", len(owned.Games), owned.Source)
	resolved, rerr := r.ResolveAll(ctx, owned.Games)
	if err := c.Save(); err != nil {
		return resolved, owned, err
	}

	// Report request volume + any throttling so a slow run is explainable.
	s := e.client.Stats()
	fmt.Printf("ProtonDB: %d requests, %d retries", s.Requests, s.Retries)
	if s.HTTP429 > 0 || s.HTTP5xx > 0 {
		fmt.Printf(" (%d×429, %d×5xx)", s.HTTP429, s.HTTP5xx)
	}
	fmt.Println()
	if s.HTTP429 > 0 {
		fmt.Printf("note: hit ProtonDB rate limits %d×; lower --max-rps (or [protondb].max_rps) for a smoother run — results are cached, so this only affects the first sweep. Use --verbose to see each backoff.\n", s.HTTP429)
	}
	return resolved, owned, rerr
}

// --- detect -------------------------------------------------------------

func cmdDetect(args []string) error {
	g, _ := parse("detect", args)
	e, err := setup(g)
	if err != nil {
		return err
	}
	cachePath := "(unavailable)"
	if c, err := cache.Open(); err == nil {
		cachePath = c.Path()
	}
	statePath := "(unavailable)"
	if s, err := state.Open(); err == nil {
		statePath = s.Path()
	}

	fmt.Printf("OS:               %s/%s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Printf("Steam root:       %s\n", e.root.Path)
	fmt.Printf("Account ID:       %d\n", e.account.AccountID)
	fmt.Printf("SteamID64:        %d\n", e.account.SteamID64)
	if e.account.PersonaName != "" {
		fmt.Printf("Persona:          %s\n", e.account.PersonaName)
	}
	fmt.Printf("Web API key:      %s\n", yesno(e.cfg.Steam.APIKey != ""))
	fmt.Printf("Collections file: %s\n", e.root.CollectionsPath(e.account))
	fmt.Printf("Cache:            %s\n", cachePath)
	fmt.Printf("State:            %s\n", statePath)
	fmt.Printf("Steam running:    %s\n", yesno(steam.IsSteamRunning()))
	return nil
}

// --- fetch --------------------------------------------------------------

func cmdFetch(args []string) error {
	g, _ := parse("fetch", args)
	e, err := setup(g)
	if err != nil {
		return err
	}
	owned, err := engine.ResolveOwned(context.Background(), e.cfg, e.root, e.account)
	if err != nil {
		return err
	}
	fmt.Printf("owned games: %d (source: %s)\n", len(owned.Games), owned.Source)
	if g.verbose {
		games := append([]engine.Game(nil), owned.Games...)
		sort.Slice(games, func(i, j int) bool { return games[i].AppID < games[j].AppID })
		for _, gm := range games {
			fmt.Printf("  %-8d %s\n", gm.AppID, gm.Name)
		}
	}
	return nil
}

// --- tiers --------------------------------------------------------------

func cmdTiers(args []string) error {
	g, _ := parse("tiers", args)
	e, err := setup(g)
	if err != nil {
		return err
	}
	resolved, _, err := e.resolveLibrary(context.Background())
	if err != nil {
		return err
	}
	fmt.Println("\ncategory counts:")
	for _, line := range engine.CategoryCounts(resolved) {
		fmt.Printf("  %s\n", line)
	}
	return nil
}

// --- plan ---------------------------------------------------------------

func cmdPlan(args []string) error {
	g, _ := parse("plan", args)
	g.dryRun = true
	return runSync(g) // plan is sync in dry-run mode
}

// --- sync ---------------------------------------------------------------

func cmdSync(args []string) error {
	g, _ := parse("sync", args)
	return runSync(g)
}

func runSync(g globals) error {
	e, err := setup(g)
	if err != nil {
		return err
	}
	specs, err := selectSpecs(e)
	if err != nil {
		return err
	}
	// If a collection references the 'native' category, native detection must
	// run or that collection would be silently empty — turn it on with a note
	// (unless the user explicitly passed --no-native).
	if !g.noNative && !e.cfg.ProtonDB.DetectNative && specsUseNative(specs) {
		e.cfg.ProtonDB.DetectNative = true
		fmt.Println("note: a collection uses 'native' — enabling native detection (batched Algolia queries). Pass --no-native to skip it (that collection will be empty).")
	}
	resolved, _, err := e.resolveLibrary(context.Background())
	if err != nil {
		return err
	}

	st, err := state.Open()
	if err != nil {
		return err
	}
	planned, names := engine.Plan(specs, resolved, e.cfg.ProtonDB.FilterNonGames)

	// Show the diff for every planned collection.
	fmt.Println("\nplanned collections:")
	for _, pc := range planned {
		var prev []uint32
		if m, ok := st.ByName(pc.Name); ok {
			prev = m.LastAppIDs
		}
		added, removed := engine.DiffAgainst(pc.AppIDs, prev)
		fmt.Printf("\n  %s  [%s]  → %d games (+%d / -%d)\n",
			pc.Name, strings.Join(pc.Tiers, ","), len(pc.AppIDs), len(added), len(removed))
		if g.verbose {
			for _, id := range added {
				fmt.Printf("    + %-8d %s\n", id, names[id])
			}
			for _, id := range removed {
				fmt.Printf("    - %-8d %s\n", id, names[id])
			}
		}
	}

	if g.dryRun {
		fmt.Println("\ndry-run: no changes written.")
		return nil
	}

	if !g.yes && !confirm("\nWrite these collections to Steam?") {
		fmt.Println("aborted.")
		return nil
	}

	return writeCollections(e, st, planned)
}

// writeCollections performs the guarded write: Steam-running check, backup,
// per-collection upsert, atomic write, and state update.
func writeCollections(e *env, st *state.State, planned []engine.PlannedCollection) error {
	if err := ensureSteamClosed(e.g); err != nil {
		return err
	}

	collPath := e.root.CollectionsPath(e.account)
	backupDir, err := paths.StateDir()
	if err != nil {
		return err
	}
	backupDir = filepath.Join(backupDir, "backups")
	if bp, err := collections.Backup(collPath, backupDir, 5, e.now); err != nil {
		return fmt.Errorf("backup failed: %w", err)
	} else if bp != "" {
		fmt.Printf("backed up to %s\n", bp)
	}

	ns, err := collections.LoadFile(collPath)
	if err != nil {
		return err
	}

	for _, pc := range planned {
		var id string
		if m, ok := st.ByName(pc.Name); ok {
			id = m.ID
		}
		newID, err := ns.Set(id, pc.Name, pc.AppIDs, e.now)
		if err != nil {
			return err
		}
		st.Upsert(&state.Managed{Name: pc.Name, ID: newID, LastAppIDs: pc.AppIDs})
	}

	if err := ns.WriteFile(collPath); err != nil {
		return fmt.Errorf("write collections: %w", err)
	}
	if err := st.Save(); err != nil {
		return fmt.Errorf("save state: %w", err)
	}
	fmt.Printf("wrote %d collections. Start Steam to pick them up.\n", len(planned))
	return nil
}

// --- list ---------------------------------------------------------------

func cmdList(args []string) error {
	g, _ := parse("list", args)
	e, err := setup(g)
	if err != nil {
		return err
	}
	st, err := state.Open()
	if err != nil {
		return err
	}
	managedIDs := map[string]bool{}
	for id := range st.Managed {
		managedIDs[id] = true
	}
	ns, err := collections.LoadFile(e.root.CollectionsPath(e.account))
	if err != nil {
		return err
	}
	infos := ns.List(managedIDs)
	sort.Slice(infos, func(i, j int) bool { return infos[i].Name < infos[j].Name })
	if len(infos) == 0 {
		fmt.Println("no collections found.")
		return nil
	}
	for _, in := range infos {
		tag := "user"
		if in.Managed {
			tag = "deckdex"
		}
		fmt.Printf("  [%-7s] %-30s %d games\n", tag, in.Name, in.Size)
	}
	return nil
}

// --- prune --------------------------------------------------------------

func cmdPrune(args []string) error {
	g, _ := parse("prune", args)
	e, err := setup(g)
	if err != nil {
		return err
	}
	st, err := state.Open()
	if err != nil {
		return err
	}
	if len(st.Managed) == 0 {
		fmt.Println("no managed collections to prune.")
		return nil
	}
	fmt.Println("managed collections to remove:")
	for _, m := range st.Managed {
		fmt.Printf("  %s\n", m.Name)
	}
	if g.dryRun {
		fmt.Println("\ndry-run: nothing removed.")
		return nil
	}
	if !g.yes && !confirm("\nRemove these managed collections?") {
		fmt.Println("aborted.")
		return nil
	}
	if err := ensureSteamClosed(g); err != nil {
		return err
	}

	collPath := e.root.CollectionsPath(e.account)
	backupDir, _ := paths.StateDir()
	backupDir = filepath.Join(backupDir, "backups")
	if _, err := collections.Backup(collPath, backupDir, 5, e.now); err != nil {
		return err
	}
	ns, err := collections.LoadFile(collPath)
	if err != nil {
		return err
	}
	ids := make([]string, 0, len(st.Managed))
	for id := range st.Managed {
		ids = append(ids, id)
	}
	for _, id := range ids {
		ns.Remove(id, e.now)
		st.Remove(id)
	}
	if err := ns.WriteFile(collPath); err != nil {
		return err
	}
	if err := st.Save(); err != nil {
		return err
	}
	fmt.Printf("removed %d managed collections. Start Steam to apply.\n", len(ids))
	return nil
}

// --- restore ------------------------------------------------------------

func cmdRestore(args []string) error {
	g, rest := parse("restore", args)
	e, err := setup(g)
	if err != nil {
		return err
	}
	backupDir, _ := paths.StateDir()
	backupDir = filepath.Join(backupDir, "backups")

	var backupPath string
	if len(rest) > 0 {
		backupPath = rest[0]
	} else {
		backupPath = collections.LatestBackup(backupDir)
	}
	if backupPath == "" {
		return fmt.Errorf("no backup found in %s", backupDir)
	}
	collPath := e.root.CollectionsPath(e.account)
	fmt.Printf("restore %s → %s\n", backupPath, collPath)
	if g.dryRun {
		fmt.Println("dry-run: nothing restored.")
		return nil
	}
	if !g.yes && !confirm("Overwrite the live collections file with this backup?") {
		fmt.Println("aborted.")
		return nil
	}
	if err := ensureSteamClosed(g); err != nil {
		return err
	}
	if err := collections.Restore(backupPath, collPath); err != nil {
		return err
	}
	fmt.Println("restored. Start Steam to apply.")
	return nil
}

// --- helpers ------------------------------------------------------------

// progressReporter returns a callback that renders an updating "resolved N/M"
// line on stderr when it is a terminal; off a TTY (piped/redirected) it returns
// nil so no carriage-return noise pollutes logs.
func progressReporter() func(done, total int) {
	fi, err := os.Stderr.Stat()
	if err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		return nil
	}
	return func(done, total int) {
		pct := 0
		if total > 0 {
			pct = done * 100 / total
		}
		fmt.Fprintf(os.Stderr, "\r  resolved %d/%d (%d%%)   ", done, total, pct)
		if done >= total {
			fmt.Fprintln(os.Stderr)
		}
	}
}

// ensureSteamClosed makes sure Steam is not running before a write. With
// --kill-steam it closes Steam directly (for unattended/cron use). Otherwise,
// when interactive it offers to close it; if declined or non-interactive, it
// returns an error. Steam caches collections client-side and overwrites the
// on-disk file on next sync, so writing while it runs is unsafe.
func ensureSteamClosed(g globals) error {
	if !steam.IsSteamRunning() {
		return nil
	}
	kill := g.killSteam
	if !kill && !g.yes {
		kill = confirm("Steam is running — close it now? (writes are overwritten otherwise)")
	}
	if !kill {
		return fmt.Errorf("Steam is running — close it and re-run, or pass --kill-steam (for cron: --kill-steam --yes)")
	}
	fmt.Println("closing Steam ...")
	_ = steam.KillSteam()
	time.Sleep(2 * time.Second)
	if steam.IsSteamRunning() {
		fmt.Println("note: Steam is still shutting down; continuing")
	}
	return nil
}

func yesno(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// confirm prompts on stdin for a yes/no answer (default no).
func confirm(prompt string) bool {
	fmt.Printf("%s [y/N] ", prompt)
	r := bufio.NewReader(os.Stdin)
	line, err := r.ReadString('\n')
	if err != nil {
		return false
	}
	line = strings.ToLower(strings.TrimSpace(line))
	return line == "y" || line == "yes"
}
