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
	// CreditBalance is the total spendable credits: PlanCreditBalance plus
	// PaygCreditBalance.
	CreditBalance int `json:"credit_balance"`
	// PlanCreditBalance is spent first and expires if the plan is not renewed.
	PlanCreditBalance int `json:"plan_credit_balance"`
	// PaygCreditBalance is spent after plan credits and never expires.
	PaygCreditBalance  int   `json:"payg_credit_balance"`
	MonthlyCreditLimit int64 `json:"monthly_credit_limit"`
}

// BalanceSplit is the remaining credits by pool. Plan credits are spent
// first, roll over when the plan renews and expire if it is not renewed.
// Pay-as-you-go credits come from credit packs, are spent after plan credits
// and never expire.
type BalanceSplit struct {
	// Balance is the total spendable credits: Plan plus Payg.
	Balance int `json:"balance"`
	Plan    int `json:"plan_balance"`
	Payg    int `json:"payg_balance"`
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
	if err := c.do(ctx, request{method: http.MethodGet, path: "/config/capabilities"}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Balance returns the remaining credits: plan and pay-as-you-go together.
func (c *Client) Balance(ctx context.Context) (int, error) {
	var out struct {
		Balance int `json:"balance"`
	}
	err := c.do(ctx, request{method: http.MethodGet, path: "/users/@me/balance"}, &out)
	return out.Balance, err
}

// BalanceSplit returns the remaining credits by pool: plan credits, spent
// first, and pay-as-you-go credits.
func (c *Client) BalanceSplit(ctx context.Context) (*BalanceSplit, error) {
	var out BalanceSplit
	if err := c.do(ctx, request{method: http.MethodGet, path: "/users/@me/balance"}, &out); err != nil {
		return nil, err
	}
	return &out, nil
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
