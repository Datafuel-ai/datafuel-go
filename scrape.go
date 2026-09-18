package datafuel

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

// Format is the shape of the scraped content.
type Format string

const (
	FormatHTML     Format = "html"     // raw page (API default)
	FormatMarkdown Format = "markdown" // cleaned text, best for LLMs
	FormatJSON     Format = "json"     // schema.org / JSON-LD / embedded JSON
	FormatPNG      Format = "png"      // full-page screenshot, needs JSRendering
	FormatJPEG     Format = "jpeg"     // full-page screenshot, needs JSRendering
)

// ProxyType is the proxy plan. Premium costs more and gets through more walls.
type ProxyType string

const (
	ProxyBasic   ProxyType = "Basic"
	ProxyPremium ProxyType = "Premium"
)

// Proxy selects the exit the request leaves from. The zero value is the
// account default.
type Proxy struct {
	Type    ProxyType `json:"proxy_type,omitempty"`
	Country string    `json:"proxy_country,omitempty"` // ISO 3166-1 alpha-2, e.g. "US"
	City    string    `json:"proxy_city,omitempty"`
	State   string    `json:"proxy_state,omitempty"`
	ASN     string    `json:"proxy_asn,omitempty"`
	// SessionID reuses the same exit across requests; TTL is its lifetime in
	// seconds. Scrape and Map only.
	SessionID string `json:"-"`
	TTL       int    `json:"-"`
}

// session adds the sticky-session keys to attributes, which is where the API
// reads them; the other proxy fields travel in the envelope.
func (p Proxy) session(attrs map[string]any) map[string]any {
	if p.SessionID != "" {
		attrs["proxy_session_id"] = p.SessionID
	}
	if p.TTL > 0 {
		attrs["proxy_ttl"] = p.TTL
	}
	return attrs
}

// ScrapeOptions are the page options shared by Scrape, jobs and crawls.
type ScrapeOptions struct {
	Format Format `json:"result_format,omitempty"`

	// JSRendering renders the page in a real browser: slower and five times
	// the credits on a Basic proxy. Leave it off unless the page is empty
	// without it.
	JSRendering              bool   `json:"js_rendering,omitempty"`
	WaitForSelector          string `json:"wait_for_selector,omitempty"`
	WaitForSelectorTimeoutMs int    `json:"wait_for_selector_timeout_ms,omitempty"`
	JSInstructions           any    `json:"js_instructions,omitempty"`
	BlockResource            string `json:"block_resource,omitempty"`

	// Markdown only. IncludeImages defaults to true on the API; point it at
	// false to drop images and save tokens.
	MainContentOnly bool  `json:"main_content_only,omitempty"`
	IncludeImages   *bool `json:"include_images,omitempty"`

	// Extract returns only the matching elements: field name → CSS selector,
	// append " @attr" to read an attribute, e.g. {"links": "a @href"}.
	// ExtractRegex does the same with RE2 patterns on the raw HTML. The
	// result is JSON: {name: [values...]}.
	Extract      map[string]string `json:"-"`
	ExtractRegex map[string]string `json:"-"`
	// Template is a comma-separated list of built-in extractors: images,
	// links, headings, phone_numbers, emails, meta_tags, tables, schema_org, all.
	Template string `json:"result_template,omitempty"`

	// AI post-processes the page with an LLM. Not available on crawls.
	AI *AIOptions `json:"-"`

	Method        string            `json:"method,omitempty"`
	Body          string            `json:"body,omitempty"`
	ContentType   string            `json:"content_type,omitempty"`
	Headers       map[string]string `json:"headers,omitempty"`
	HeaderOrder   []string          `json:"header_order,omitempty"`
	Cookies       string            `json:"cookie_string,omitempty"`
	UserAgentType string            `json:"user_agent_type,omitempty"` // chrome, firefox, safari, edge
	UserAgent     string            `json:"user_agent,omitempty"`
}

// AIOptions turns the page into structured data with your own LLM key.
type AIOptions struct {
	Prompt   string // what to extract
	Format   any    // example JSON object (struct or map) the output must follow
	Provider string // openai, anthropic, google
	Model    string
	APIKey   string
}

// attributes flattens the options into the API's attributes object and adds
// the endpoint-specific keys on top.
func (o ScrapeOptions) attributes(extra map[string]any) (map[string]any, error) {
	raw, err := json.Marshal(o)
	if err != nil {
		return nil, fmt.Errorf("datafuel: encode options: %w", err)
	}
	attrs := map[string]any{}
	if err := json.Unmarshal(raw, &attrs); err != nil {
		return nil, fmt.Errorf("datafuel: encode options: %w", err)
	}
	// The REST API takes multi-field extractors as a JSON object string.
	for key, fields := range map[string]map[string]string{"extract_selector": o.Extract, "extract_regex": o.ExtractRegex} {
		if len(fields) == 0 {
			continue
		}
		encoded, err := json.Marshal(fields)
		if err != nil {
			return nil, fmt.Errorf("datafuel: encode %s: %w", key, err)
		}
		attrs[key] = string(encoded)
	}
	if ai := o.AI; ai != nil {
		attrs["result_use_ai"] = true
		setIf(attrs, "result_ai_prompt", ai.Prompt)
		setIf(attrs, "ai_provider", ai.Provider)
		setIf(attrs, "ai_model", ai.Model)
		setIf(attrs, "ai_api_key", ai.APIKey)
		if ai.Format != nil {
			attrs["result_ai_format"] = ai.Format
		}
	}
	for k, v := range extra {
		attrs[k] = v
	}
	return attrs, nil
}

func setIf(m map[string]any, key, value string) {
	if value != "" {
		m[key] = value
	}
}

// envelope is the body every write shares.
type envelope struct {
	Type string `json:"type,omitempty"`
	Proxy
	Multithreaded *bool `json:"multithreaded,omitempty"`
	Attributes    any   `json:"attributes"`
}

// Status of a task, job or crawl.
type Status string

const (
	StatusCreated             Status = "created"
	StatusPending             Status = "pending"
	StatusProcessing          Status = "processing"
	StatusCompleted           Status = "completed"
	StatusCompletedWithErrors Status = "completed_with_errors"
	StatusFailed              Status = "failed"
	StatusCancelled           Status = "cancelled"
)

// Done reports whether the status is final.
func (s Status) Done() bool {
	switch s {
	case StatusCompleted, StatusCompletedWithErrors, StatusFailed, StatusCancelled:
		return true
	}
	return false
}

// Result is one scraped target with its metadata. Read the metadata before
// the content: a 200 can be an error page, Redirected means FinalURL is not
// what you asked for, and a 404 still completes and bills.
type Result struct {
	ID          string `json:"id,omitempty"`
	Status      Status `json:"status,omitempty"`
	StatusCode  int    `json:"status_code,omitempty"` // HTTP status the target answered
	FinalURL    string `json:"final_url,omitempty"`
	Redirected  bool   `json:"redirected,omitempty"`
	CreditsUsed int    `json:"credits_used"` // 0 when the task failed
	DurationMs  int    `json:"duration_ms,omitempty"`
	Blocked     bool   `json:"blocked,omitempty"`
	Protection  string `json:"protection,omitempty"` // anti-bot vendor when Blocked
	Error       string `json:"error,omitempty"`

	// Payload is the raw "result" object. Prefer Text, Decode and Image.
	Payload Payload `json:"result"`
}

// Payload is {"data": ...} on success, {"status", "error_detail",
// "status_code"} on failure and {"status"} while a job task is running.
type Payload struct {
	Data        json.RawMessage `json:"data,omitempty"`
	Status      Status          `json:"status,omitempty"`
	ErrorDetail string          `json:"error_detail,omitempty"`
	StatusCode  int             `json:"status_code,omitempty"`
}

func (r *Result) state() Status {
	if r.Status != "" {
		return r.Status
	}
	if r.Payload.Status != "" {
		return r.Payload.Status
	}
	if len(r.Payload.Data) > 0 {
		return StatusCompleted
	}
	return StatusPending
}

// Pending reports whether the task has not finished yet (job and crawl
// results list unfinished tasks as stubs).
func (r *Result) Pending() bool { return !r.state().Done() }

// Err returns a *TaskError when the task failed, nil otherwise.
func (r *Result) Err() error {
	if r.state() == StatusFailed {
		return &TaskError{Result: r}
	}
	return nil
}

// Text returns the content of an html or markdown result. For structured
// results it returns the raw JSON.
func (r *Result) Text() string {
	var s string
	if json.Unmarshal(r.Payload.Data, &s) == nil {
		return s
	}
	return string(r.Payload.Data)
}

// Decode unmarshals a structured result (FormatJSON, Extract, Template, AI)
// into v.
func (r *Result) Decode(v any) error {
	if len(r.Payload.Data) == 0 {
		return fmt.Errorf("datafuel: result has no data (status %s)", r.state())
	}
	return json.Unmarshal(r.Payload.Data, v)
}

// Image returns the bytes of a FormatPNG or FormatJPEG screenshot.
func (r *Result) Image() ([]byte, error) {
	return base64.StdEncoding.DecodeString(r.Text())
}

// ScrapeRequest is one page to fetch.
type ScrapeRequest struct {
	URL   string
	Proxy Proxy
	// IdempotencyKey is generated when empty.
	IdempotencyKey string
	ScrapeOptions
}

// Scrape fetches one URL and blocks until the result is ready. When the page
// could not be scraped it returns the Result together with a *TaskError, so
// `if err != nil` is enough and errors.Is(err, datafuel.ErrBlocked) tells
// you why.
func (c *Client) Scrape(ctx context.Context, req *ScrapeRequest) (*Result, error) {
	if req == nil {
		return nil, ErrNilRequest
	}
	attrs, err := req.attributes(map[string]any{"url": req.URL})
	if err != nil {
		return nil, err
	}
	return c.runTask(ctx, "/task", envelope{Type: "unlocker", Proxy: req.Proxy, Attributes: req.Proxy.session(attrs)}, req.IdempotencyKey)
}

// Markdown is the one-liner for the common case: the page as LLM-ready
// markdown, images dropped.
func (c *Client) Markdown(ctx context.Context, pageURL string) (string, error) {
	noImages := false
	res, err := c.Scrape(ctx, &ScrapeRequest{URL: pageURL, ScrapeOptions: ScrapeOptions{
		Format: FormatMarkdown, IncludeImages: &noImages,
	}})
	if err != nil {
		return "", err
	}
	return res.Text(), nil
}

// GetTask returns a task by ID, e.g. one created by a job or crawl. A task
// that is still running comes back with Pending() true and no error.
func (c *Client) GetTask(ctx context.Context, taskID string) (*Result, error) {
	var out struct {
		Result
		Code string `json:"code"` // set on the 202 "still processing" answer
	}
	if err := c.do(ctx, request{method: http.MethodGet, path: "/task/" + url.PathEscape(taskID)}, &out); err != nil {
		return nil, err
	}
	res := &out.Result
	if out.Code == "TASK_STILL_PROCESSING" {
		res.ID, res.Status = taskID, StatusProcessing
	}
	return res, res.Err()
}

func (c *Client) runTask(ctx context.Context, path string, body envelope, key string) (*Result, error) {
	var res Result
	err := c.do(ctx, request{method: http.MethodPost, path: path, body: body, idempotencyKey: keyOr(key)}, &res)
	if err != nil {
		return nil, err
	}
	return &res, res.Err()
}

// Engine is an AI assistant Ask can query. Engines can be switched off at
// runtime; Client.Capabilities says which ones accept work.
type Engine string

const (
	EngineOpenAI       Engine = "openai"
	EngineGemini       Engine = "gemini"
	EngineGoogleAIMode Engine = "google_ai_mode"
	EnginePerplexity   Engine = "perplexity"
	EngineCopilot      Engine = "copilot"
)

// AskRequest is a prompt for an AI engine (task type llm_scraping).
type AskRequest struct {
	Prompt         string
	Engine         Engine
	WebSearch      bool
	FollowUp       string
	Country        string
	Format         string
	IdempotencyKey string
}

func (r *AskRequest) attributes(target map[string]any) map[string]any {
	target["engine"] = r.Engine
	if r.WebSearch {
		target["websearch"] = true
	}
	setIf(target, "follow_up_prompt", r.FollowUp)
	setIf(target, "proxy_country", r.Country)
	setIf(target, "result_format", r.Format)
	return target
}

// Ask sends a prompt to an AI engine and returns its answer.
func (c *Client) Ask(ctx context.Context, req *AskRequest) (*Result, error) {
	if req == nil {
		return nil, ErrNilRequest
	}
	attrs := req.attributes(map[string]any{"prompt": req.Prompt})
	return c.runTask(ctx, "/task", envelope{Type: "llm_scraping", Attributes: attrs}, req.IdempotencyKey)
}
