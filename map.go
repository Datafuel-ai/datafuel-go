package datafuel

import (
	"context"
	"encoding/json"
	"fmt"
)

// MapRequest lists the URLs of a site without scraping them. One credit per
// call on a Basic proxy, however many links come back.
type MapRequest struct {
	URL   string `json:"url"`
	Proxy Proxy  `json:"-"`

	Search            string `json:"search,omitempty"`  // keep links whose URL or title contains this
	Sitemap           string `json:"sitemap,omitempty"` // map only this sitemap, from a previous SiteMap.Sitemaps
	Limit             int    `json:"limit,omitempty"`   // default 5000, max 10000
	IncludeSubdomains bool   `json:"include_subdomains,omitempty"`
	IgnoreSitemap     bool   `json:"ignore_sitemap,omitempty"`
	SitemapOnly       bool   `json:"sitemap_only,omitempty"`

	IdempotencyKey string `json:"-"`
}

// Link is one discovered URL.
type Link struct {
	URL     string `json:"url"`
	Source  string `json:"source"`            // "sitemap" or "page"
	Title   string `json:"title,omitempty"`   // page links only
	LastMod string `json:"lastmod,omitempty"` // sitemap links only
}

// SiteMap is the answer of Map.
type SiteMap struct {
	URL            string   `json:"url"`
	Links          []Link   `json:"links"`
	Total          int      `json:"total"`
	Truncated      bool     `json:"truncated"` // Limit or the time budget cut the list
	Sitemaps       []string `json:"sitemaps"`
	PageStatusCode int      `json:"page_status_code"`
	// Reason says why Links is empty: page_blocked, page_error,
	// page_unreachable, no_links_on_page (try Scrape with JSRendering),
	// no_sitemap, sitemap_no_entries, search_no_match.
	Reason  string `json:"reason,omitempty"`
	Credits int    `json:"credits"`

	// Task is the envelope of the underlying task.
	Task *Result `json:"-"`
}

// Map discovers the URLs of a site from its sitemaps and the links on the
// page. Use it before a crawl to see how big a section is.
func (c *Client) Map(ctx context.Context, req *MapRequest) (*SiteMap, error) {
	if req == nil {
		return nil, ErrNilRequest
	}
	raw, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("datafuel: encode request: %w", err)
	}
	attrs := map[string]any{}
	if err := json.Unmarshal(raw, &attrs); err != nil {
		return nil, fmt.Errorf("datafuel: encode request: %w", err)
	}
	res, err := c.runTask(ctx, "/map", envelope{Type: "map", Proxy: req.Proxy, Attributes: req.Proxy.session(attrs)}, req.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	site := &SiteMap{Task: res}
	if err := res.Decode(site); err != nil {
		return nil, err
	}
	return site, nil
}
