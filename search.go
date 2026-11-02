package datafuel

import "context"

// Device is the device a search is made from.
type Device string

const (
	DeviceDesktop Device = "desktop"
	// DeviceMobile results carry no total_results.
	DeviceMobile Device = "mobile"
)

// AIOverview is Google's AI overview, the "ai_overview" key of a JSON search
// result when SearchRequest.AIOverview is set and Google showed one.
type AIOverview struct {
	Text       string                `json:"text"`
	Products   []AIOverviewProduct   `json:"products,omitempty"`
	References []AIOverviewReference `json:"references,omitempty"`
}

// AIOverviewProduct is a product card in an AI overview.
type AIOverviewProduct struct {
	Title   string  `json:"title"`
	Price   string  `json:"price,omitempty"`
	Source  string  `json:"source,omitempty"`
	Rating  float64 `json:"rating,omitempty"`
	Reviews int     `json:"reviews,omitempty"`
	Link    string  `json:"link,omitempty"`
}

// AIOverviewReference is a source cited by an AI overview.
type AIOverviewReference struct {
	Title    string `json:"title"`
	Source   string `json:"source,omitempty"`
	Snippet  string `json:"snippet,omitempty"`
	URL      string `json:"url,omitempty"`
	Redirect string `json:"redirect,omitempty"`
}

// SearchRequest is a Google search (task type serp). Location, UULE and
// Lat/Lon are mutually exclusive; Radius needs one of them. The result is
// JSON by default, read it with Result.Decode.
type SearchRequest struct {
	Query string `json:"query,omitempty"`

	Country      string `json:"country,omitempty"`  // gl, e.g. "us"
	Language     string `json:"language,omitempty"` // hl, e.g. "en"
	Location     string `json:"location,omitempty"` // e.g. "Austin, Texas, United States"
	GoogleDomain string `json:"google_domain,omitempty"`

	// Page is how many result pages to fetch, default 1. They come back
	// merged into one organic list with running positions; the other
	// sections come from page 1. The API caps it at 10 and rejects a
	// negative value. Each page costs 10 credits, pages Google did not serve
	// are refunded, and the result's "page" is the number returned.
	Page int `json:"page,omitempty"`

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

	// Device is DeviceDesktop (default) or DeviceMobile.
	Device Device `json:"device,omitempty"`

	// AIOverview asks for Google's AI overview too, returned as the
	// "ai_overview" key of the result; decode it into an [AIOverview].
	AIOverview bool `json:"ai_overview,omitempty"`

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
