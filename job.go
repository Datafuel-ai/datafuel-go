package datafuel

import (
	"context"
	"net/http"
	"net/url"
)

// JobRequest scrapes a list of known URLs asynchronously. Cheaper and more
// predictable than a crawl when you already have the URLs. A job needs at
// least two URLs, fewer fail with ErrJobRequiresMultipleTargets.
type JobRequest struct {
	URLs  []string
	Proxy Proxy
	// Sequential processes the URLs one after the other. The default runs
	// them concurrently, bounded by your account's concurrency limit.
	Sequential     bool
	IdempotencyKey string
	ScrapeOptions
}

// AskJobRequest sends a batch of prompts to an AI engine.
type AskJobRequest struct {
	Prompts    []string
	Sequential bool
	AskRequest // Prompt is ignored
}

// JobStatus is the progress of a job.
type JobStatus struct {
	Status         Status `json:"status"`
	TasksCount     int    `json:"tasks_count"`
	TasksDone      int    `json:"tasks_done"`
	TasksRemaining int    `json:"tasks_remaining"`
	TotalCost      int    `json:"total_cost"`
}

// CancelResult is the state of a job or crawl after a cancel. Pending tasks
// fail and are refunded; tasks already in flight finish and bill normally.
type CancelResult struct {
	JobStatus
	RefundedTasks   int `json:"refunded_tasks"`
	RefundedCredits int `json:"refunded_credits"`
}

// JobResults holds every task of a job. Check Result.Err per task: a job
// completes even when some of its tasks failed.
type JobResults struct {
	ID            string    `json:"-"` // set by RunJob
	TasksCount    int       `json:"tasks_count"`
	TasksFailed   int       `json:"tasks_failed"`
	TasksComplete int       `json:"tasks_complete"`
	Tasks         []*Result `json:"tasks_result"`
}

// CreateJob queues the batch and returns the job ID immediately.
func (c *Client) CreateJob(ctx context.Context, req *JobRequest) (string, error) {
	if req == nil {
		return "", ErrNilRequest
	}
	attrs, err := req.attributes(map[string]any{"urls": req.URLs})
	if err != nil {
		return "", err
	}
	return c.createJob(ctx, envelope{Type: "unlocker", Proxy: req.Proxy, Attributes: req.Proxy.session(attrs)}, req.Sequential, req.IdempotencyKey)
}

// CreateAskJob queues a batch of prompts and returns the job ID.
func (c *Client) CreateAskJob(ctx context.Context, req *AskJobRequest) (string, error) {
	if req == nil {
		return "", ErrNilRequest
	}
	attrs := req.AskRequest.attributes(map[string]any{"prompts": req.Prompts})
	return c.createJob(ctx, envelope{Type: "llm_scraping", Attributes: attrs}, req.Sequential, req.IdempotencyKey)
}

func (c *Client) createJob(ctx context.Context, body envelope, sequential bool, key string) (string, error) {
	multithreaded := !sequential
	body.Multithreaded = &multithreaded
	var out struct {
		ID string `json:"id"`
	}
	err := c.do(ctx, request{method: http.MethodPost, path: "/job", body: body, idempotencyKey: keyOr(key)}, &out)
	return out.ID, err
}

// GetJob returns the progress of a job.
func (c *Client) GetJob(ctx context.Context, jobID string) (*JobStatus, error) {
	var out JobStatus
	if err := c.do(ctx, request{method: http.MethodGet, path: "/job/" + url.PathEscape(jobID)}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// JobResults returns the per-task results of a job.
func (c *Client) JobResults(ctx context.Context, jobID string) (*JobResults, error) {
	var out JobResults
	if err := c.do(ctx, request{method: http.MethodGet, path: "/job/" + url.PathEscape(jobID) + "/results"}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CancelJob stops a job. Cancelling a cancelled job is a no-op; a finished
// job fails with ErrJobNotCancellable.
func (c *Client) CancelJob(ctx context.Context, jobID string) (*CancelResult, error) {
	return c.cancel(ctx, "/job/"+url.PathEscape(jobID)+"/cancel")
}

func (c *Client) cancel(ctx context.Context, path string) (*CancelResult, error) {
	var out CancelResult
	if err := c.do(ctx, request{method: http.MethodPost, path: path, idempotent: true}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// WaitJob polls until the job is done or ctx ends. On error it still returns
// the last status it saw, which may be nil.
func (c *Client) WaitJob(ctx context.Context, jobID string) (*JobStatus, error) {
	var last *JobStatus
	for {
		status, err := c.GetJob(ctx, jobID)
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

// RunJob creates the job, waits for it and returns its results. When ctx ends
// first, the returned JobResults still carries the ID, so the job (which
// keeps running and billing) can be picked up again with JobResults.
func (c *Client) RunJob(ctx context.Context, req *JobRequest) (*JobResults, error) {
	id, err := c.CreateJob(ctx, req)
	return c.finishJob(ctx, id, err)
}

// RunAskJob creates a prompt job, waits for it and returns its results, like
// RunJob.
func (c *Client) RunAskJob(ctx context.Context, req *AskJobRequest) (*JobResults, error) {
	id, err := c.CreateAskJob(ctx, req)
	return c.finishJob(ctx, id, err)
}

// RunSearchJob creates a search job, waits for it and returns its results,
// like RunJob.
func (c *Client) RunSearchJob(ctx context.Context, req *SearchJobRequest) (*JobResults, error) {
	id, err := c.CreateSearchJob(ctx, req)
	return c.finishJob(ctx, id, err)
}

func (c *Client) finishJob(ctx context.Context, id string, err error) (*JobResults, error) {
	if err != nil {
		return nil, err
	}
	if _, err := c.WaitJob(ctx, id); err != nil {
		return &JobResults{ID: id}, err
	}
	results, err := c.JobResults(ctx, id)
	if err != nil {
		return &JobResults{ID: id}, err
	}
	results.ID = id
	return results, nil
}
