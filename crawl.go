package datafuel

import (
	"context"
	"iter"
	"net/http"
	"net/url"
	"strconv"
)

// CrawlRequest follows links from a start URL and scrapes every page. Pages
// are charged like single scrapes when they are queued; failed and blocked
// pages are refunded.
type CrawlRequest struct {
	URL   string
	Proxy Proxy

	MaxPages     int      // default 100, max 10000; the hard budget of the crawl
	MaxDepth     int      // default 3, max 10; the start URL is depth 0
	IncludePaths []string // RE2 on path?query; when set only matches are followed
	ExcludePaths []string // RE2 on path?query; exclude wins
	// By default only same-host URLs under the start URL's path are followed.
	IncludeSubdomains  bool
	AllowBackwardLinks bool
	Concurrency        int // pages in flight, default 5

	IdempotencyKey string
	ScrapeOptions
}

// CrawlStatus is the progress of a crawl.
type CrawlStatus struct {
	Status Status `json:"status"`
	// StopReason is empty while the crawl still grows: max_pages,
	// max_depth_exhausted, insufficient_credits, cancelled.
	StopReason string `json:"stop_reason"`
	Pages      struct {
		Discovered int `json:"discovered"`
		Enqueued   int `json:"enqueued"`
		Done       int `json:"done"`
		Failed     int `json:"failed"`
		Skipped    int `json:"skipped"`
	} `json:"pages"`
	DepthReached int `json:"depth_reached"`
	// TotalCost is what was charged at queue time; refunds of failed pages
	// are not subtracted. Sum CrawlPage.CreditsUsed for the net figure.
	TotalCost int `json:"total_cost"`
}

// CrawlPage is one crawled page: where it was found plus the scrape Result.
type CrawlPage struct {
	URL    string `json:"url"`
	Depth  int    `json:"depth"`
	TaskID string `json:"task_id"`
	Result
}

// CrawlResultsPage is one page of crawl results.
type CrawlResultsPage struct {
	Pages      []*CrawlPage `json:"pages"`
	NextCursor string       `json:"next_cursor"` // empty on the last page
}

// StartCrawl queues the crawl and returns its ID immediately.
func (c *Client) StartCrawl(ctx context.Context, req *CrawlRequest) (string, error) {
	if req == nil {
		return "", ErrNilRequest
	}
	extra := map[string]any{"url": req.URL}
	if req.MaxPages > 0 {
		extra["max_pages"] = req.MaxPages
	}
	if req.MaxDepth > 0 {
		extra["max_depth"] = req.MaxDepth
	}
	if req.Concurrency > 0 {
		extra["concurrency"] = req.Concurrency
	}
	if len(req.IncludePaths) > 0 {
		extra["include_paths"] = req.IncludePaths
	}
	if len(req.ExcludePaths) > 0 {
		extra["exclude_paths"] = req.ExcludePaths
	}
	if req.IncludeSubdomains {
		extra["include_subdomains"] = true
	}
	if req.AllowBackwardLinks {
		extra["allow_backward_links"] = true
	}
	attrs, err := req.attributes(extra)
	if err != nil {
		return "", err
	}
	var out struct {
		JobID string `json:"job_id"`
	}
	err = c.do(ctx, request{
		method: http.MethodPost, path: "/crawl",
		body:           envelope{Type: "crawl", Proxy: req.Proxy, Attributes: attrs},
		idempotencyKey: keyOr(req.IdempotencyKey),
	}, &out)
	return out.JobID, err
}

// GetCrawl returns the progress of a crawl.
func (c *Client) GetCrawl(ctx context.Context, crawlID string) (*CrawlStatus, error) {
	var out CrawlStatus
	if err := c.do(ctx, request{method: http.MethodGet, path: "/crawl/" + url.PathEscape(crawlID)}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CrawlResults returns one page of results in discovery order. limit 0 uses
// the API default (100, max 500). Results can be read while the crawl runs;
// unfinished pages report Pending.
func (c *Client) CrawlResults(ctx context.Context, crawlID, cursor string, limit int) (*CrawlResultsPage, error) {
	query := url.Values{}
	if cursor != "" {
		query.Set("cursor", cursor)
	}
	if limit > 0 {
		query.Set("limit", strconv.Itoa(limit))
	}
	var out CrawlResultsPage
	err := c.do(ctx, request{method: http.MethodGet, path: "/crawl/" + url.PathEscape(crawlID) + "/results", query: query}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// CrawlPages iterates over every page of a crawl, fetching result pages as
// needed:
//
//	for page, err := range client.CrawlPages(ctx, id) {
//		if err != nil { return err }
//		fmt.Println(page.URL, len(page.Text()))
//	}
func (c *Client) CrawlPages(ctx context.Context, crawlID string) iter.Seq2[*CrawlPage, error] {
	return func(yield func(*CrawlPage, error) bool) {
		cursor := ""
		for {
			batch, err := c.CrawlResults(ctx, crawlID, cursor, 0)
			if err != nil {
				yield(nil, err)
				return
			}
			for _, page := range batch.Pages {
				if !yield(page, nil) {
					return
				}
			}
			if cursor = batch.NextCursor; cursor == "" {
				return
			}
		}
	}
}

// WaitCrawl polls until the crawl is done or ctx ends. On error it still returns
// the last status it saw, which may be nil.
func (c *Client) WaitCrawl(ctx context.Context, crawlID string) (*CrawlStatus, error) {
	var last *CrawlStatus
	for {
		status, err := c.GetCrawl(ctx, crawlID)
		if err != nil {
			return last, err
		}
		if last = status; status.Status.Done() {
			return status, nil
		}
		if err := sleep(ctx, c.pollInterval); err != nil {
			return last, err
		}
	}
}

// CrawlResult is a finished crawl: why it stopped and what it found.
type CrawlResult struct {
	ID string
	*CrawlStatus
	Pages []*CrawlPage
}

// Crawl starts the crawl, waits for it and returns every page. Check
// StopReason: "insufficient_credits" means the crawl ended early. For large
// crawls prefer StartCrawl + WaitCrawl + CrawlPages to stream instead of
// holding all pages in memory.
//
// When ctx ends first, the returned CrawlResult still carries the ID, so the
// crawl (which keeps running and billing) can be picked up again.
func (c *Client) Crawl(ctx context.Context, req *CrawlRequest) (*CrawlResult, error) {
	id, err := c.StartCrawl(ctx, req)
	if err != nil {
		return nil, err
	}
	out := &CrawlResult{ID: id, CrawlStatus: &CrawlStatus{}}
	status, err := c.WaitCrawl(ctx, id)
	if status != nil {
		out.CrawlStatus = status
	}
	if err != nil {
		return out, err
	}
	for page, err := range c.CrawlPages(ctx, id) {
		if err != nil {
			return out, err
		}
		out.Pages = append(out.Pages, page)
	}
	return out, nil
}
