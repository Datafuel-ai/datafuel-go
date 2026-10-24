package datafuel

import "context"

// DuckDuckGoRequest is a prompt for duck.ai's chat (task type duckduckgo).
// Flat price per prompt, whatever the model. The result is JSON
// {"text", "citations"}, read it with Result.Decode; citation markers in
// text read [n].
type DuckDuckGoRequest struct {
	Prompt string `json:"prompt,omitempty"`
	// Model is gpt-6-luna (default), gpt-5.4-mini, claude-haiku-4-5,
	// mistral-small-2603, tinfoil/gpt-oss-120b or tinfoil/gemma4-31b.
	Model string `json:"model,omitempty"`
	// Country is the exit country; duck.ai answers location questions for it.
	Country string `json:"proxy_country,omitempty"`

	IdempotencyKey string `json:"-"`
}

// DuckDuckGoJobRequest sends a batch of at least two prompts.
type DuckDuckGoJobRequest struct {
	Prompts           []string
	Sequential        bool
	DuckDuckGoRequest // Prompt is ignored
}

// DuckDuckGo sends a prompt to duck.ai and returns its answer.
func (c *Client) DuckDuckGo(ctx context.Context, req *DuckDuckGoRequest) (*Result, error) {
	if req == nil {
		return nil, ErrNilRequest
	}
	attrs, err := toAttrs(req)
	if err != nil {
		return nil, err
	}
	return c.runTask(ctx, "/task", envelope{Type: "duckduckgo", Attributes: attrs}, req.IdempotencyKey)
}

// CreateDuckDuckGoJob queues a batch of duck.ai prompts and returns the job ID.
func (c *Client) CreateDuckDuckGoJob(ctx context.Context, req *DuckDuckGoJobRequest) (string, error) {
	if req == nil {
		return "", ErrNilRequest
	}
	attrs, err := toAttrs(&req.DuckDuckGoRequest)
	if err != nil {
		return "", err
	}
	delete(attrs, "prompt")
	attrs["prompts"] = req.Prompts
	return c.createJob(ctx, envelope{Type: "duckduckgo", Attributes: attrs}, req.Sequential, req.IdempotencyKey)
}

// RunDuckDuckGoJob creates a duck.ai job, waits for it and returns its
// results, like RunJob.
func (c *Client) RunDuckDuckGoJob(ctx context.Context, req *DuckDuckGoJobRequest) (*JobResults, error) {
	id, err := c.CreateDuckDuckGoJob(ctx, req)
	return c.finishJob(ctx, id, err)
}
