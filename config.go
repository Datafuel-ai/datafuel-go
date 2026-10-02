package datafuel

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
)

// JSInstructionArg is one argument of a browser action.
type JSInstructionArg struct {
	Name     string   `json:"name"`
	Type     string   `json:"type"`
	Values   []string `json:"values,omitempty"`
	Required bool     `json:"required"`
}

// JSInstruction is one browser action ScrapeOptions.JSInstructions accepts.
type JSInstruction struct {
	Action      string `json:"action"`
	Description string `json:"description"`
	// Value is the JSON shape under the action key: scalar, array or object.
	Value string             `json:"value"`
	Args  []JSInstructionArg `json:"args"`
	// IFrame reports whether the action also exists as iframe_<action>.
	IFrame  bool            `json:"iframe"`
	Example json.RawMessage `json:"example"`
}

// JSInstructions lists the browser actions ScrapeOptions.JSInstructions
// accepts, with their arguments and an example each.
func (c *Client) JSInstructions(ctx context.Context) ([]JSInstruction, error) {
	var out struct {
		Instructions []JSInstruction `json:"instructions"`
	}
	if err := c.do(ctx, request{method: http.MethodGet, path: "/config/js-instructions"}, &out); err != nil {
		return nil, err
	}
	return out.Instructions, nil
}

// AIProvider is an LLM provider AIOptions.Provider accepts, with the models
// AIOptions.Model accepts for it.
type AIProvider struct {
	Name   string   `json:"name"`
	Models []string `json:"models"`
}

// AIProviders lists the providers and models AIOptions accepts. Any other
// value fails with ErrInvalidAttributes.
func (c *Client) AIProviders(ctx context.Context) ([]AIProvider, error) {
	var out struct {
		Providers []AIProvider `json:"providers"`
	}
	if err := c.do(ctx, request{method: http.MethodGet, path: "/config/ai-providers"}, &out); err != nil {
		return nil, err
	}
	return out.Providers, nil
}

// ProxyLocation is a named proxy location: a city, or an ASN.
type ProxyLocation struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

// ProxyRegion is a region of a country with its cities.
type ProxyRegion struct {
	Code   string          `json:"code"`
	Name   string          `json:"name"`
	Cities []ProxyLocation `json:"cities"`
}

// ProxyCountry is a country a proxy plan can exit from. Its codes are the
// values of Proxy.Country, Proxy.State and Proxy.City.
type ProxyCountry struct {
	Code    string        `json:"code"`
	Name    string        `json:"name"`
	Regions []ProxyRegion `json:"regions"`
}

func proxyQuery(proxyType ProxyType, country string) url.Values {
	q := url.Values{}
	setIfValue(q, "proxy_type", string(proxyType))
	setIfValue(q, "country", country)
	return q
}

// ProxyLocations lists the countries, regions and cities a proxy plan can
// exit from. An empty proxyType is the account default.
func (c *Client) ProxyLocations(ctx context.Context, proxyType ProxyType) ([]ProxyCountry, error) {
	var out []ProxyCountry
	err := c.do(ctx, request{method: http.MethodGet, path: "/config/proxy/locations", query: proxyQuery(proxyType, "")}, &out)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ProxyASNs lists the ASNs a proxy plan can exit from in one country (ISO
// 3166-1 alpha-2). Their codes are the values of Proxy.ASN. An empty
// proxyType is the account default.
func (c *Client) ProxyASNs(ctx context.Context, country string, proxyType ProxyType) ([]ProxyLocation, error) {
	var out []ProxyLocation
	err := c.do(ctx, request{method: http.MethodGet, path: "/config/proxy/asn", query: proxyQuery(proxyType, country)}, &out)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ProtectionCheck is the anti-bot protection found in front of one host and
// path: cloudflare, cloudflare_5sec, akamai, imperva, perimeterx or
// unprotected.
type ProtectionCheck struct {
	Host           string `json:"host"`
	Path           string `json:"path"`
	ProtectionType string `json:"protection_type"`
}

// CheckProtection probes each URL and reports the anti-bot protection in
// front of it. Nothing is scraped and nothing is charged. Invalid URLs are
// skipped.
func (c *Client) CheckProtection(ctx context.Context, urls []string) ([]ProtectionCheck, error) {
	var out []ProtectionCheck
	if urls == nil {
		urls = []string{}
	}
	err := c.do(ctx, request{method: http.MethodPost, path: "/filter/check", body: urls}, &out)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// HealthCheck is the outcome of pinging one dependency: ok or fail.
type HealthCheck struct {
	Status    string `json:"status"`
	LatencyMs int64  `json:"latency_ms"`
}

// Health is the state of the API: ok or degraded. Checks is filled by a deep
// check only, keyed by dependency.
type Health struct {
	Status string                 `json:"status"`
	Checks map[string]HealthCheck `json:"checks,omitempty"`
}

// OK reports whether the API can serve requests.
func (h *Health) OK() bool { return h.Status == "ok" }

// Health reports whether the API is up. With deep it also checks the
// dependencies the API needs to serve scrapes; a degraded report is returned
// without an error, so read Health.OK.
func (c *Client) Health(ctx context.Context, deep bool) (*Health, error) {
	q := url.Values{}
	if deep {
		q.Set("deep", "1")
	}
	var out Health
	if err := c.do(ctx, request{method: http.MethodGet, path: "/healthz", query: q, degradedOK: true}, &out); err != nil {
		return nil, err
	}
	if out.Status == "" {
		return nil, &APIError{StatusCode: http.StatusServiceUnavailable, Message: http.StatusText(http.StatusServiceUnavailable)}
	}
	return &out, nil
}
