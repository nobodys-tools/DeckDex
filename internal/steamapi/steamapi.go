// Package steamapi is a minimal client for the official Steam Web API. Only
// GetOwnedGames is implemented — it is the opt-in, fully-reliable owned-games
// source used when a free Web API key is configured.
package steamapi

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const ownedGamesEndpoint = "https://api.steampowered.com/IPlayerService/GetOwnedGames/v1/"

// maxResponseBytes caps the owned-games response to avoid unbounded memory use
// from an unexpectedly huge or hostile body.
const maxResponseBytes = 32 << 20 // 32 MiB

// client uses an explicit transport: HTTP/2 disabled (a half-dead connection
// can otherwise stall the request with no reliable timeout) plus fail-fast TLS
// and response-header timeouts.
var client = &http.Client{
	Timeout: 30 * time.Second,
	Transport: &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		ForceAttemptHTTP2:     false,
		TLSNextProto:          map[string]func(string, *tls.Conn) http.RoundTripper{},
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 20 * time.Second,
		IdleConnTimeout:       60 * time.Second,
	},
}

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
//
// The key is passed as a query parameter (Steam offers no alternative), so any
// error that might quote the request URL is scrubbed before it is returned.
func GetOwnedGames(ctx context.Context, apiKey string, steamID64 uint64) ([]Game, error) {
	q := url.Values{}
	q.Set("key", apiKey)
	q.Set("steamid", strconv.FormatUint(steamID64, 10))
	q.Set("include_appinfo", "1")
	q.Set("include_played_free_games", "1")
	q.Set("format", "json")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ownedGamesEndpoint+"?"+q.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("steamapi: build request: %s", redactKey(err, apiKey))
	}
	req.Header.Set("User-Agent", "DeckDex/1.0 (+https://github.com/nobodys-tools/DeckDex)")

	resp, err := client.Do(req)
	if err != nil {
		// err embeds the request URL (which contains the API key); scrub it.
		return nil, fmt.Errorf("steamapi: request failed: %s", redactKey(err, apiKey))
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, fmt.Errorf("steamapi: HTTP %d — bad API key or private profile (game details must be public)", resp.StatusCode)
	default:
		return nil, fmt.Errorf("steamapi: GetOwnedGames HTTP %d", resp.StatusCode)
	}

	var r ownedGamesResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("steamapi: decode response: %w", err)
	}
	return r.Response.Games, nil
}

// redactKey replaces the API key in an error string so it never reaches logs.
func redactKey(err error, key string) string {
	s := err.Error()
	if key != "" {
		s = strings.ReplaceAll(s, key, "<redacted>")
	}
	return s
}
