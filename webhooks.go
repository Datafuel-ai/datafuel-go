package datafuel

import (
	"context"
	"net/http"
	"net/url"
	"time"
)

// Webhook asks for one signed POST when a job or crawl finishes. Metadata is
// an optional JSON object of at most 4096 bytes, echoed in the event. Verify
// the signature with any Standard Webhooks library and the secret from
// Client.WebhookSecret.
type Webhook struct {
	URL      string `json:"url"`
	Metadata any    `json:"metadata,omitempty"`
}

// WebhookSecret signs webhooks. After a rotation the previous secret keeps
// signing until PreviousExpiresAt.
type WebhookSecret struct {
	Secret            string     `json:"secret"`
	CreatedAt         time.Time  `json:"created_at"`
	RotatedAt         *time.Time `json:"rotated_at,omitempty"`
	PreviousExpiresAt *time.Time `json:"previous_expires_at,omitempty"`
}

type WebhookEndpoint struct {
	ID          string `json:"id"`
	URL         string `json:"url"`
	Description string `json:"description"`
	// EventTypes are names such as job.completed, or categories: job.*, crawl.*.
	EventTypes     []string   `json:"event_types"`
	Enabled        bool       `json:"enabled"`
	DisabledReason string     `json:"disabled_reason,omitempty"`
	LastSuccessAt  *time.Time `json:"last_success_at,omitempty"`
	FailingSince   *time.Time `json:"failing_since,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

// CreatedWebhookEndpoint carries the endpoint's own secret. Store it: the API
// returns it only on create, and a new one from RotateWebhookEndpointSecret.
type CreatedWebhookEndpoint struct {
	WebhookEndpoint
	Secret string `json:"secret"`
}

// CreateWebhookEndpointRequest needs URL and 1 to 20 EventTypes. Enabled nil
// means true.
type CreateWebhookEndpointRequest struct {
	URL         string   `json:"url"`
	EventTypes  []string `json:"event_types"`
	Description string   `json:"description,omitempty"`
	Enabled     *bool    `json:"enabled,omitempty"`
}

// UpdateWebhookEndpointRequest changes the fields that are set; at least one
// must be. Setting Enabled to true re-enables a disabled endpoint.
type UpdateWebhookEndpointRequest struct {
	URL         *string   `json:"url,omitempty"`
	EventTypes  *[]string `json:"event_types,omitempty"`
	Description *string   `json:"description,omitempty"`
	Enabled     *bool     `json:"enabled,omitempty"`
}

type WebhookDelivery struct {
	ID         string `json:"id"`
	EventID    string `json:"event_id"`
	EventType  string `json:"event_type"`
	JobID      string `json:"job_id,omitempty"`
	EndpointID string `json:"endpoint_id,omitempty"`
	URL        string `json:"url"`
	// Status is pending, delivering, retrying, delivered or failed.
	Status         string     `json:"status"`
	Attempts       int        `json:"attempts"`
	NextAttemptAt  *time.Time `json:"next_attempt_at,omitempty"`
	LastStatusCode int        `json:"last_status_code,omitempty"`
	LastError      string     `json:"last_error,omitempty"`
	DeliveredAt    *time.Time `json:"delivered_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

type WebhookDeliveriesOptions struct {
	EndpointID string
	JobID      string
	Status     string
	// Limit is items per page: API default 50, max 200.
	Limit  int
	Cursor string
}

type WebhookDeliveriesPage struct {
	Deliveries []*WebhookDelivery `json:"deliveries"`
	NextCursor string             `json:"next_cursor"` // empty on the last page
}

func (c *Client) webhookCall(ctx context.Context, method, path string, body, out any) error {
	return c.do(ctx, request{method: method, path: "/webhooks" + path, body: body}, out)
}

func webhookSecretCall(ctx context.Context, c *Client, method, path string) (*WebhookSecret, error) {
	var out WebhookSecret
	if err := c.webhookCall(ctx, method, path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func webhookDeliveryCall(ctx context.Context, c *Client, path string) (*WebhookDelivery, error) {
	var out WebhookDelivery
	if err := c.webhookCall(ctx, http.MethodPost, path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// WebhookSecret returns the account's signing secret, creating it on first
// use.
func (c *Client) WebhookSecret(ctx context.Context) (*WebhookSecret, error) {
	return webhookSecretCall(ctx, c, http.MethodGet, "/secret")
}

// RotateWebhookSecret replaces the signing secret. The old one keeps signing
// for 24 hours.
func (c *Client) RotateWebhookSecret(ctx context.Context) (*WebhookSecret, error) {
	return webhookSecretCall(ctx, c, http.MethodPost, "/secret/rotate")
}

func (c *Client) CreateWebhookEndpoint(ctx context.Context, req *CreateWebhookEndpointRequest) (*CreatedWebhookEndpoint, error) {
	if req == nil {
		return nil, ErrNilRequest
	}
	var out CreatedWebhookEndpoint
	if err := c.webhookCall(ctx, http.MethodPost, "/endpoints", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) ListWebhookEndpoints(ctx context.Context) ([]*WebhookEndpoint, error) {
	var out struct {
		Endpoints []*WebhookEndpoint `json:"endpoints"`
	}
	if err := c.webhookCall(ctx, http.MethodGet, "/endpoints", nil, &out); err != nil {
		return nil, err
	}
	return out.Endpoints, nil
}

func (c *Client) GetWebhookEndpoint(ctx context.Context, endpointID string) (*WebhookEndpoint, error) {
	var out WebhookEndpoint
	if err := c.webhookCall(ctx, http.MethodGet, "/endpoints/"+url.PathEscape(endpointID), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) UpdateWebhookEndpoint(ctx context.Context, endpointID string, req *UpdateWebhookEndpointRequest) (*WebhookEndpoint, error) {
	if req == nil {
		return nil, ErrNilRequest
	}
	var out WebhookEndpoint
	if err := c.webhookCall(ctx, http.MethodPatch, "/endpoints/"+url.PathEscape(endpointID), req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DeleteWebhookEndpoint(ctx context.Context, endpointID string) error {
	return c.webhookCall(ctx, http.MethodDelete, "/endpoints/"+url.PathEscape(endpointID), nil, nil)
}

// RotateWebhookEndpointSecret replaces an endpoint's secret. The old one keeps
// signing for 24 hours.
func (c *Client) RotateWebhookEndpointSecret(ctx context.Context, endpointID string) (*WebhookSecret, error) {
	return webhookSecretCall(ctx, c, http.MethodPost, "/endpoints/"+url.PathEscape(endpointID)+"/secret/rotate")
}

// TestWebhookEndpoint queues a webhook.test event for the endpoint.
func (c *Client) TestWebhookEndpoint(ctx context.Context, endpointID string) (*WebhookDelivery, error) {
	return webhookDeliveryCall(ctx, c, "/endpoints/"+url.PathEscape(endpointID)+"/test")
}

// ListWebhookDeliveries returns deliveries newest first.
func (c *Client) ListWebhookDeliveries(ctx context.Context, opts *WebhookDeliveriesOptions) (*WebhookDeliveriesPage, error) {
	q := url.Values{}
	if opts != nil {
		setIfValue(q, "endpoint_id", opts.EndpointID)
		setIfValue(q, "job_id", opts.JobID)
		setIfValue(q, "status", opts.Status)
		setCount(q, "limit", opts.Limit)
		setIfValue(q, "cursor", opts.Cursor)
	}
	var out WebhookDeliveriesPage
	if err := c.do(ctx, request{method: http.MethodGet, path: "/webhooks/deliveries", query: q}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) RetryWebhookDelivery(ctx context.Context, deliveryID string) (*WebhookDelivery, error) {
	return webhookDeliveryCall(ctx, c, "/deliveries/"+url.PathEscape(deliveryID)+"/retry")
}
