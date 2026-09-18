// Package datafuel is the Go client for the DataFuel scraping API.
//
//	client := datafuel.New(os.Getenv("DATAFUEL_API_KEY"))
//	md, err := client.Markdown(ctx, "https://example.com")
//
// Pick the call by the shape of the work:
//
//   - one URL                      → [Client.Scrape]
//   - the URL list of a site       → [Client.Map]
//   - many pages from a start URL  → [Client.Crawl]
//   - a list of known URLs         → [Client.RunJob]
//   - a question for an AI engine  → [Client.Ask]
//
// Every write carries an Idempotency-Key (generated when you do not set one),
// so the client can retry dropped connections and 429/5xx answers without
// charging twice.
package datafuel

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	// DefaultBaseURL is the production API.
	DefaultBaseURL = "https://scraping-api.datafuel.ai/api/v1"
	// Version of this SDK, sent in the User-Agent.
	Version = "0.1.0"
)

// Client talks to the DataFuel API. It is safe for concurrent use.
type Client struct {
	apiKey       string
	baseURL      string
	httpClient   *http.Client
	userAgent    string
	maxRetries   int
	pollInterval time.Duration
}

// Option configures a Client.
type Option func(*Client)

// WithBaseURL points the client at another environment, e.g. staging.
func WithBaseURL(baseURL string) Option {
	return func(c *Client) { c.baseURL = strings.TrimRight(baseURL, "/") }
}

// WithHTTPClient replaces the HTTP client. Do not set a short Timeout on it:
// Scrape blocks until the page is ready, which can take minutes with
// JSRendering. Bound calls with the context instead.
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) {
		if h != nil {
			c.httpClient = h
		}
	}
}

// WithMaxRetries sets how often a request is retried after a network error
// or a 429/502/503/504 answer. Default 2, 0 disables retries.
func WithMaxRetries(n int) Option {
	return func(c *Client) { c.maxRetries = max(n, 0) }
}

// WithPollInterval sets how often WaitJob and WaitCrawl poll. Default 2s.
func WithPollInterval(d time.Duration) Option {
	return func(c *Client) {
		if d > 0 {
			c.pollInterval = d
		}
	}
}

// WithUserAgent prefixes the SDK User-Agent with your application's.
func WithUserAgent(ua string) Option {
	return func(c *Client) { c.userAgent = ua + " " + c.userAgent }
}

// New returns a client for apiKey. An empty key falls back to the
// DATAFUEL_API_KEY environment variable.
func New(apiKey string, opts ...Option) *Client {
	if apiKey == "" {
		apiKey = os.Getenv("DATAFUEL_API_KEY")
	}
	c := &Client{
		apiKey:       apiKey,
		baseURL:      DefaultBaseURL,
		httpClient:   &http.Client{},
		userAgent:    "datafuel-go/" + Version,
		maxRetries:   2,
		pollInterval: 2 * time.Second,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

type request struct {
	method string
	path   string
	query  url.Values
	body   any
	// idempotencyKey makes a POST safe to retry; GETs always are.
	idempotencyKey string
}

func (c *Client) do(ctx context.Context, r request, out any) error {
	if c.apiKey == "" {
		return ErrNoAPIKey
	}
	var payload []byte
	if r.body != nil {
		var err error
		if payload, err = json.Marshal(r.body); err != nil {
			return fmt.Errorf("datafuel: encode request: %w", err)
		}
	}
	target := c.baseURL + r.path
	if len(r.query) > 0 {
		target += "?" + r.query.Encode()
	}
	retryable := r.method == http.MethodGet || r.idempotencyKey != ""

	var lastErr error
	for attempt := 0; ; attempt++ {
		retryAfter, err := c.once(ctx, r, target, payload, out)
		if err == nil {
			return nil
		}
		lastErr = err
		if !retryable || attempt >= c.maxRetries || !shouldRetry(ctx, err) {
			return lastErr
		}
		if err := sleep(ctx, backoff(attempt, retryAfter)); err != nil {
			return lastErr
		}
	}
}

func (c *Client) once(ctx context.Context, r request, target string, payload []byte, out any) (time.Duration, error) {
	var body io.Reader
	if payload != nil {
		body = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, r.method, target, body)
	if err != nil {
		return 0, fmt.Errorf("datafuel: build request: %w", err)
	}
	req.Header.Set("X-API-Key", c.apiKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if r.idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", r.idempotencyKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, &transportError{err}
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, &transportError{err}
	}
	if resp.StatusCode >= 400 {
		retryAfter, _ := strconv.Atoi(resp.Header.Get("Retry-After"))
		return time.Duration(retryAfter) * time.Second, newAPIError(resp.StatusCode, data)
	}
	if out == nil || len(data) == 0 {
		return 0, nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return 0, fmt.Errorf("datafuel: decode response: %w", err)
	}
	return 0, nil
}

func backoff(attempt int, retryAfter time.Duration) time.Duration {
	if retryAfter > 0 {
		return min(retryAfter, 30*time.Second)
	}
	// 500ms, 1s, 2s, 4s, then 8s flat. The clamp keeps the shift from
	// overflowing into a negative duration on high attempt counts.
	base := 500 * time.Millisecond << min(attempt, 4)
	jitter, err := rand.Int(rand.Reader, big.NewInt(int64(base/2)+1))
	if err != nil {
		return base
	}
	return base/2 + time.Duration(jitter.Int64())
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// newIdempotencyKey returns a random UUIDv4.
func newIdempotencyKey() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func keyOr(key string) string {
	if key != "" {
		return key
	}
	return newIdempotencyKey()
}
