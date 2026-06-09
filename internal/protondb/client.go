// Package protondb is a read-only client for ProtonDB's undocumented public
// endpoints: the per-app tier summary, the Steam appdetails proxy, and the
// steamdb2 (Algolia) metadata index (for native/Steam-Deck status).
//
// All read endpoints are unauthenticated. The client self-throttles with a
// token bucket (max_rps), a concurrency semaphore, and jittered exponential
// backoff that honours Retry-After.
package protondb

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net"
	"net/http"
	neturl "net/url"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"golang.org/x/time/rate"
)

const (
	baseURL   = "https://www.protondb.com"
	userAgent = "DeckDex/1.0 (+https://github.com/nobodys-tools/DeckDex; ProtonDB tier sync)"

	// Algolia steamdb index — queried directly for native/Deck metadata to avoid
	// ProtonDB's rate-limited /proxy/steamdb2/query. The search key is public
	// (shipped in ProtonDB's JS bundle) and referer-restricted.
	algoliaApp      = "94HE6YATEI"
	algoliaKey      = "9ba0e69fb2974316cdaec8f5f257088f"
	algoliaQueryURL = "https://94HE6YATEI-dsn.algolia.net/1/indexes/steamdb/query"
	nativeBatchSize = 100 // objectIDs OR-ed per Algolia query

	// maxResponseBytes caps any single response body to avoid unbounded memory
	// use from an unexpectedly huge or hostile response.
	maxResponseBytes = 32 << 20 // 32 MiB
)

// Client talks to ProtonDB. Construct with New.
type Client struct {
	http    *http.Client
	limiter *rate.Limiter
	sem     chan struct{}
	maxTry  int
	verbose bool

	// Counters (atomic) for end-of-run diagnostics.
	reqs    int64 // HTTP requests actually sent (attempts)
	retries int64 // retries triggered by 429/5xx
	n429    int64 // responses with HTTP 429
	n5xx    int64 // responses with HTTP 5xx
}

// Options configures a Client.
type Options struct {
	MaxRPS         float64 // requests/sec token-bucket rate (default 50)
	MaxConcurrency int     // in-flight request cap (default 16)
	MaxRetries     int     // attempts per request on 429/5xx (default 6)
	Timeout        time.Duration
	Verbose        bool // log each rate-limit/backoff event to stderr
}

// New builds a Client, applying sensible defaults for zero-valued options.
func New(o Options) *Client {
	if o.MaxRPS <= 0 {
		o.MaxRPS = 50
	}
	if o.MaxConcurrency <= 0 {
		o.MaxConcurrency = 16
	}
	if o.MaxRetries <= 0 {
		o.MaxRetries = 6
	}
	if o.Timeout <= 0 {
		o.Timeout = 30 * time.Second
	}
	return &Client{
		http:    &http.Client{Timeout: o.Timeout, Transport: newTransport()},
		limiter: rate.NewLimiter(rate.Limit(o.MaxRPS), int(math.Max(1, o.MaxRPS))),
		sem:     make(chan struct{}, o.MaxConcurrency),
		maxTry:  o.MaxRetries,
		verbose: o.Verbose,
	}
}

// newTransport builds an HTTP transport with explicit, fail-fast timeouts.
// HTTP/2 is disabled deliberately: multiplexing every concurrent request over a
// single connection means one half-dead server connection can stall *all* of
// them in writeRequest with no reliable timeout. Plain HTTP/1.1 with a pooled
// connection per in-flight request (we cap concurrency separately) lets each
// request time out independently.
func newTransport() *http.Transport {
	return &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2: false,
		// A non-nil empty TLSNextProto disables HTTP/2 negotiation.
		TLSNextProto:          map[string]func(string, *tls.Conn) http.RoundTripper{},
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   32,
		IdleConnTimeout:       60 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 20 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
}

// Stats is a snapshot of request counters for end-of-run reporting.
type Stats struct {
	Requests, Retries, HTTP429, HTTP5xx int64
}

// Stats returns the current request counters.
func (c *Client) Stats() Stats {
	return Stats{
		Requests: atomic.LoadInt64(&c.reqs),
		Retries:  atomic.LoadInt64(&c.retries),
		HTTP429:  atomic.LoadInt64(&c.n429),
		HTTP5xx:  atomic.LoadInt64(&c.n5xx),
	}
}

// Throttled reports whether any rate-limiting (429) occurred.
func (c *Client) Throttled() bool { return atomic.LoadInt64(&c.n429) > 0 }

// Tier is a ProtonDB compatibility tier.
type Tier string

const (
	TierPlatinum Tier = "platinum"
	TierGold     Tier = "gold"
	TierSilver   Tier = "silver"
	TierBronze   Tier = "bronze"
	TierBorked   Tier = "borked"
	TierPending  Tier = "pending"
)

// Summary is the per-app tier summary (the authoritative published tier).
type Summary struct {
	Tier            Tier   `json:"tier"`
	TrendingTier    Tier   `json:"trendingTier"`
	BestReportedTier Tier  `json:"bestReportedTier"`
	Confidence      string `json:"confidence"`
	Score           float64 `json:"score"`
	Total           int    `json:"total"`
}

// Summary fetches the published tier for appID. HTTP 404 (no reports) maps to a
// pending summary rather than an error.
func (c *Client) Summary(ctx context.Context, appID uint32) (Summary, error) {
	url := fmt.Sprintf("%s/api/v1/reports/summaries/%d.json", baseURL, appID)
	body, status, err := c.get(ctx, url, nil)
	if err != nil {
		return Summary{}, err
	}
	if status == http.StatusNotFound {
		return Summary{Tier: TierPending, Total: 0}, nil
	}
	if status != http.StatusOK {
		return Summary{}, fmt.Errorf("protondb: summary %d: HTTP %d", appID, status)
	}
	var s Summary
	if err := json.Unmarshal(body, &s); err != nil {
		return Summary{}, fmt.Errorf("protondb: summary %d decode: %w", appID, err)
	}
	if s.Tier == "" {
		s.Tier = TierPending
	}
	return s, nil
}

// AppDetails is the slimmed Steam appdetails we care about (type + platforms +
// release date), fetched via ProtonDB's proxy to dodge Steam's IP rate limit.
type AppDetails struct {
	Type      string // "game", "dlc", "music", ...
	LinuxNative bool
	ReleaseDate string // raw "data.release_date.date" string
}

// appdetails proxy response shape (partial).
type appDetailsEnvelope map[string]struct {
	Success bool `json:"success"`
	Data    struct {
		Type      string `json:"type"`
		Platforms struct {
			Windows bool `json:"windows"`
			Mac     bool `json:"mac"`
			Linux   bool `json:"linux"`
		} `json:"platforms"`
		ReleaseDate struct {
			ComingSoon bool   `json:"coming_soon"`
			Date       string `json:"date"`
		} `json:"release_date"`
	} `json:"data"`
}

// AppDetails fetches type/platforms/release-date for appID through the ProtonDB
// Steam proxy. Returns (zero, false, nil) when the store reports success:false
// (delisted/region-locked) so callers can degrade gracefully.
func (c *Client) AppDetails(ctx context.Context, appID uint32) (AppDetails, bool, error) {
	url := fmt.Sprintf("%s/proxy/steam/api/appdetails/?appids=%d&filters=platforms,type,release_date", baseURL, appID)
	body, status, err := c.get(ctx, url, nil)
	if err != nil {
		return AppDetails{}, false, err
	}
	if status != http.StatusOK {
		return AppDetails{}, false, fmt.Errorf("protondb: appdetails %d: HTTP %d", appID, status)
	}
	var env appDetailsEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return AppDetails{}, false, fmt.Errorf("protondb: appdetails %d decode: %w", appID, err)
	}
	e, ok := env[strconv.FormatUint(uint64(appID), 10)]
	if !ok || !e.Success {
		return AppDetails{}, false, nil
	}
	return AppDetails{
		Type:        e.Data.Type,
		LinuxNative: e.Data.Platforms.Linux,
		ReleaseDate: e.Data.ReleaseDate.Date,
	}, true, nil
}

// steamdb2 (Algolia proxy) response (partial).
// algoliaResponse is the subset of an Algolia query response we read.
type algoliaResponse struct {
	Hits []struct {
		ObjectID string   `json:"objectID"`
		OSList   []string `json:"oslist"`
	} `json:"hits"`
}

// NativeBatch resolves native-Linux status for many appIDs in a few batched
// queries against the Algolia steamdb index DIRECTLY — not ProtonDB's
// /proxy/steamdb2/query, which is aggressively IP-rate-limited. The public,
// referer-restricted search key is fine for read-only queries. Returns a map
// appID->native; appIDs absent from the index are simply missing (treat as
// non-native). "Linux" present in a record's oslist ⇒ native.
func (c *Client) NativeBatch(ctx context.Context, appIDs []uint32) (map[uint32]bool, error) {
	out := make(map[uint32]bool, len(appIDs))
	hdr := http.Header{}
	hdr.Set("X-Algolia-API-Key", algoliaKey)
	hdr.Set("X-Algolia-Application-Id", algoliaApp)
	hdr.Set("Referer", "https://www.protondb.com/") // key is referer-restricted
	hdr.Set("Content-Type", "application/json")

	for start := 0; start < len(appIDs); start += nativeBatchSize {
		end := start + nativeBatchSize
		if end > len(appIDs) {
			end = len(appIDs)
		}
		var fb strings.Builder
		for i, id := range appIDs[start:end] {
			if i > 0 {
				fb.WriteString(" OR ")
			}
			fb.WriteString("objectID:")
			fb.WriteString(strconv.FormatUint(uint64(id), 10))
		}
		params := "query=&hitsPerPage=1000&filters=" + neturl.QueryEscape(fb.String()) +
			"&attributesToRetrieve=" + neturl.QueryEscape(`["objectID","oslist"]`)
		reqBody := `{"params":` + strconv.Quote(params) + `}`

		body, status, err := c.do(ctx, http.MethodPost, algoliaQueryURL, []byte(reqBody), hdr)
		if err != nil {
			return out, err
		}
		if status != http.StatusOK {
			return out, fmt.Errorf("protondb: algolia steamdb query: HTTP %d", status)
		}
		var r algoliaResponse
		if err := json.Unmarshal(body, &r); err != nil {
			return out, fmt.Errorf("protondb: algolia decode: %w", err)
		}
		for _, h := range r.Hits {
			if id, err := strconv.ParseUint(h.ObjectID, 10, 32); err == nil {
				out[uint32(id)] = hasLinux(h.OSList)
			}
		}
	}
	return out, nil
}

func hasLinux(oslist []string) bool {
	for _, o := range oslist {
		if strings.EqualFold(strings.TrimSpace(o), "Linux") {
			return true
		}
	}
	return false
}

// --- transport with throttle + retry -------------------------------------

func (c *Client) get(ctx context.Context, url string, hdr http.Header) ([]byte, int, error) {
	return c.do(ctx, http.MethodGet, url, nil, hdr)
}

func (c *Client) do(ctx context.Context, method, url string, body []byte, hdr http.Header) ([]byte, int, error) {
	// Concurrency cap.
	select {
	case c.sem <- struct{}{}:
		defer func() { <-c.sem }()
	case <-ctx.Done():
		return nil, 0, ctx.Err()
	}

	var lastErr error
	for attempt := 0; attempt < c.maxTry; attempt++ {
		if err := c.limiter.Wait(ctx); err != nil {
			return nil, 0, err
		}
		var rdr io.Reader
		if body != nil {
			rdr = strings.NewReader(string(body))
		}
		req, err := http.NewRequestWithContext(ctx, method, url, rdr)
		if err != nil {
			return nil, 0, err
		}
		req.Header.Set("User-Agent", userAgent)
		req.Header.Set("Accept", "application/json")
		for k, vs := range hdr {
			for _, v := range vs {
				req.Header.Set(k, v)
			}
		}

		atomic.AddInt64(&c.reqs, 1)
		resp, err := c.http.Do(req)
		if err != nil {
			lastErr = err
			atomic.AddInt64(&c.retries, 1)
			c.logBackoff(url, "network error: "+err.Error(), attempt, c.sleepBackoff(ctx, attempt, 0))
			continue
		}
		data, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
		resp.Body.Close()

		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			if resp.StatusCode == http.StatusTooManyRequests {
				atomic.AddInt64(&c.n429, 1)
			} else {
				atomic.AddInt64(&c.n5xx, 1)
			}
			atomic.AddInt64(&c.retries, 1)
			retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))
			lastErr = fmt.Errorf("HTTP %d", resp.StatusCode)
			wait := c.sleepBackoff(ctx, attempt, retryAfter)
			c.logBackoff(url, fmt.Sprintf("HTTP %d", resp.StatusCode), attempt, wait)
			continue
		}
		return data, resp.StatusCode, nil
	}
	return nil, 0, fmt.Errorf("protondb: %s %s failed after %d attempts: %w", method, url, c.maxTry, lastErr)
}

// sleepBackoff waits min(jittered exponential, capped) — or Retry-After when it
// is longer — before the next attempt, and returns the duration it waited.
func (c *Client) sleepBackoff(ctx context.Context, attempt int, retryAfter time.Duration) time.Duration {
	base := time.Duration(math.Min(float64(time.Duration(1<<attempt)*250*time.Millisecond), float64(30*time.Second)))
	jitter := time.Duration(rand.Int63n(int64(base/2 + 1)))
	wait := base + jitter
	if retryAfter > wait {
		wait = retryAfter
	}
	t := time.NewTimer(wait)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
	return wait
}

// logBackoff prints a one-line throttle notice to stderr when verbose.
func (c *Client) logBackoff(url, reason string, attempt int, wait time.Duration) {
	if !c.verbose {
		return
	}
	short := url
	if i := strings.Index(url, ".com/"); i >= 0 {
		short = url[i+4:]
	}
	fmt.Fprintf(os.Stderr, "\n  ⚠ %s on %s — retry %d after %s\n", reason, short, attempt+1, wait.Round(time.Millisecond))
}

func parseRetryAfter(v string) time.Duration {
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		// http.ParseTime needs a reference; honour as relative if in the future.
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}
