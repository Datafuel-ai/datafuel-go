package datafuel

import (
	"context"
	"net/http"
)

// Profile is the account behind the API key.
type Profile struct {
	Email              string `json:"email"`
	Username           string `json:"username"`
	CurrentConcurrency int    `json:"current_concurrency"`
	ConcurrencyLimit   int    `json:"concurrency_limit"`
	CreditBalance      int    `json:"credit_balance"`
	MonthlyCreditLimit int64  `json:"monthly_credit_limit"`
}

// Capability is one task type or LLM engine and whether it accepts new work.
type Capability struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	Reason  string `json:"reason,omitempty"` // operator's note when switched off
}

// Capabilities lists what the API accepts right now.
type Capabilities struct {
	Modules []Capability `json:"modules"`
	Engines []Capability `json:"engines"`
}

// EngineEnabled reports whether an LLM engine accepts work. Unknown engines
// report false.
func (c *Capabilities) EngineEnabled(e Engine) bool {
	for _, engine := range c.Engines {
		if engine.Name == string(e) {
			return engine.Enabled
		}
	}
	return false
}

// Capabilities returns which task types and LLM engines are switched on.
// Operators can switch them off at runtime, e.g. during a provider outage;
// calls to a switched-off one fail with ErrModuleUnavailable or
// ErrEngineUnavailable.
func (c *Client) Capabilities(ctx context.Context) (*Capabilities, error) {
	var out Capabilities
	if err := c.do(ctx, request{method: http.MethodGet, path: "/capabilities"}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Balance returns the remaining credits.
func (c *Client) Balance(ctx context.Context) (int, error) {
	var out struct {
		Balance int `json:"balance"`
	}
	err := c.do(ctx, request{method: http.MethodGet, path: "/users/@me/balance"}, &out)
	return out.Balance, err
}

// Me returns the account profile.
func (c *Client) Me(ctx context.Context) (*Profile, error) {
	var out Profile
	if err := c.do(ctx, request{method: http.MethodGet, path: "/users/@me"}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ResetAPIKey revokes the current key and returns the new one. This is the
// only time the new key is shown. The client still holds the revoked key:
// store the new one and build a new client with it.
func (c *Client) ResetAPIKey(ctx context.Context) (string, error) {
	var out struct {
		APIKey string `json:"api_key"`
	}
	err := c.do(ctx, request{method: http.MethodPost, path: "/users/@me/api-key/reset"}, &out)
	return out.APIKey, err
}
