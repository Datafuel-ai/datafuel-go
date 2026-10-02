package datafuel

import "context"

// SearchRequest is a Google search (task type serp). Location, UULE and
// Lat/Lon are mutually exclusive; Radius needs one of them. The result is
// JSON by default, read it with Result.Decode.
type SearchRequest struct {
	Query string `json:"query,omitempty"`

	Country      string `json:"country,omitempty"`  // gl, e.g. "us"
	Language     string `json:"language,omitempty"` // hl, e.g. "en"
	Location     string `json:"location,omitempty"` // e.g. "Austin, Texas, United States"
	Page         int    `json:"page,omitempty"`     // 1-based, default 1
	GoogleDomain string `json:"google_domain,omitempty"`

	UULE   string   `json:"uule,omitempty"`
	Lat    *float64 `json:"lat,omitempty"`
	Lon    *float64 `json:"lon,omitempty"`
	Radius int      `json:"radius,omitempty"` // max 1000

	CR      string `json:"cr,omitempty"`
	LR      string `json:"lr,omitempty"`
	TBS     string `json:"tbs,omitempty"`
	Safe    string `json:"safe,omitempty"` // active or off
	NFPR    *bool  `json:"nfpr,omitempty"`
	Filter  *bool  `json:"filter,omitempty"`
	UDS     string `json:"uds,omitempty"`
	KGMID   string `json:"kgmid,omitempty"`
	SI      string `json:"si,omitempty"`
	LudoCID string `json:"ludocid,omitempty"`
	LSig    string `json:"lsig,omitempty"`
	IBP     string `json:"ibp,omitempty"`

	// ProxyCountry is accepted but not used yet: searches leave through
	// DataFuel's own pool. Target a market with Country and Language.
	ProxyCountry string `json:"proxy_country,omitempty"`
	// Format is FormatJSON (default), FormatHTML or FormatMarkdown.
	Format Format `json:"result_format,omitempty"`

	IdempotencyKey string `json:"-"`
}

// SearchJobRequest sends a batch of at least two queries.
type SearchJobRequest struct {
	Queries       []string
	Sequential    bool
	SearchRequest // Query is ignored
}

// Search runs one Google search and returns the results page.
func (c *Client) Search(ctx context.Context, req *SearchRequest) (*Result, error) {
	if req == nil {
		return nil, ErrNilRequest
	}
	attrs, err := toAttrs(req)
	if err != nil {
		return nil, err
	}
	return c.runTask(ctx, "/task", envelope{Type: "serp", Attributes: attrs}, req.IdempotencyKey)
}

// CreateSearchJob queues a batch of searches and returns the job ID.
func (c *Client) CreateSearchJob(ctx context.Context, req *SearchJobRequest) (string, error) {
	if req == nil {
		return "", ErrNilRequest
	}
	attrs, err := toAttrs(&req.SearchRequest)
	if err != nil {
		return "", err
	}
	delete(attrs, "query")
	attrs["queries"] = req.Queries
	return c.createJob(ctx, envelope{Type: "serp", Attributes: attrs}, req.Sequential, req.IdempotencyKey)
}
