// Package steamapi is a minimal client for the official Steam Web API. Only
// GetOwnedGames is implemented — it is the opt-in, fully-reliable owned-games
// source used when a free Web API key is configured.
package steamapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const ownedGamesEndpoint = "https://api.steampowered.com/IPlayerService/GetOwnedGames/v1/"

// Game is one owned game from GetOwnedGames.
type Game struct {
	AppID           uint32 `json:"appid"`
	Name            string `json:"name"`
	PlaytimeForever int    `json:"playtime_forever"`
}

type ownedGamesResponse struct {
	Response struct {
		GameCount int    `json:"game_count"`
		Games     []Game `json:"games"`
	} `json:"response"`
}

// GetOwnedGames returns the owned games for steamID64. Requires a free key from
// https://steamcommunity.com/dev/apikey and a public game-details profile.
func GetOwnedGames(ctx context.Context, apiKey string, steamID64 uint64) ([]Game, error) {
	q := url.Values{}
	q.Set("key", apiKey)
	q.Set("steamid", strconv.FormatUint(steamID64, 10))
	q.Set("include_appinfo", "1")
	q.Set("include_played_free_games", "1")
	q.Set("format", "json")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ownedGamesEndpoint+"?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "DeckDex/1.0 (+https://github.com/nobodys-tools/DeckDex)")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, fmt.Errorf("steamapi: HTTP %d — bad API key or private profile (game details must be public)", resp.StatusCode)
	default:
		return nil, fmt.Errorf("steamapi: GetOwnedGames HTTP %d", resp.StatusCode)
	}

	var r ownedGamesResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("steamapi: decode: %w", err)
	}
	return r.Response.Games, nil
}
