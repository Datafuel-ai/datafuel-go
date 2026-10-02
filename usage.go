package datafuel

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const dateLayout = "2006-01-02"

// ListOptions filters ListJobs and ListTasks. Zero values are not sent.
type ListOptions struct {
	Status Status
	// Type is a task type: unlocker, llm_scraping, serp, map, or crawl.
	Type string
	// Start and End bound the creation day in UTC; End is inclusive.
	Start time.Time
	End   time.Time
	// Limit is items per page: API default 50, max 200.
	Limit int
	// Cursor is the NextCursor of the previous page.
	Cursor string
}

func (o *ListOptions) query() url.Values {
	q := url.Values{}
	if o == nil {
		return q
	}
	setIfValue(q, "status", string(o.Status))
	setIfValue(q, "type", o.Type)
	setDate(q, "start_date", o.Start)
	setDate(q, "end_date", o.End)
	setCount(q, "limit", o.Limit)
	setIfValue(q, "cursor", o.Cursor)
	return q
}

func setIfValue(q url.Values, key, value string) {
	if value != "" {
		q.Set(key, value)
	}
}

func setDate(q url.Values, key string, t time.Time) {
	if !t.IsZero() {
		q.Set(key, t.UTC().Format(dateLayout))
	}
}

func setCount(q url.Values, key string, n int) {
	if n > 0 {
		q.Set(key, strconv.Itoa(n))
	}
}

// JobSummary is a job or crawl in a list.
type JobSummary struct {
	ID string `json:"id"`
	// Type is crawl for a crawl, otherwise the task type of the batch.
	Type string `json:"type"`
	JobStatus
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// JobsPage is one page of jobs, newest first.
type JobsPage struct {
	Jobs       []*JobSummary `json:"jobs"`
	NextCursor string        `json:"next_cursor"` // empty on the last page
}

// ListJobs returns one page of your jobs and crawls, newest first. Pass
// NextCursor back as Cursor until it is empty.
func (c *Client) ListJobs(ctx context.Context, opts *ListOptions) (*JobsPage, error) {
	var out JobsPage
	if err := c.do(ctx, request{method: http.MethodGet, path: "/job", query: opts.query()}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListTasksOptions filters ListTasks.
type ListTasksOptions struct {
	ListOptions
	// JobID keeps only the tasks of one job or crawl.
	JobID string
}

// TaskSummary is a task in a list. It carries no result: fetch that with
// GetTask.
type TaskSummary struct {
	ID string `json:"id"`
	// JobID is empty for a task created on its own.
	JobID  string `json:"job_id"`
	Type   string `json:"type"`
	Status Status `json:"status"`
	// URL is empty for llm_scraping and serp.
	URL string `json:"url"`
	// CreditCost is charged when the task is queued; a failed task is refunded.
	CreditCost  int        `json:"credit_cost"`
	CreatedAt   time.Time  `json:"created_at"`
	ProcessedAt *time.Time `json:"processed_at"`
	FailedAt    *time.Time `json:"failed_at"`
}

// TasksPage is one page of tasks, newest first.
type TasksPage struct {
	Tasks      []*TaskSummary `json:"tasks"`
	NextCursor string         `json:"next_cursor"` // empty on the last page
}

// ListTasks returns one page of your tasks, newest first, including those of
// jobs and crawls. Pass NextCursor back as Cursor until it is empty.
func (c *Client) ListTasks(ctx context.Context, opts *ListTasksOptions) (*TasksPage, error) {
	q := url.Values{}
	if opts != nil {
		q = opts.ListOptions.query()
		setIfValue(q, "job_id", opts.JobID)
	}
	var out TasksPage
	if err := c.do(ctx, request{method: http.MethodGet, path: "/task", query: q}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// TransactionsOptions filters and pages Transactions. Zero values are not
// sent.
type TransactionsOptions struct {
	// Operation is one of plan_assignment, purchase, usage, refund, topup,
	// expiry, adjustment.
	Operation string
	Start     time.Time
	End       time.Time
	// Page starts at 1.
	Page int
	// Limit is items per page: API default 10, max 200.
	Limit int
}

// Transaction is one credit movement. Amount is negative for usage and
// expiry.
type Transaction struct {
	ID     int64 `json:"id"`
	Amount int   `json:"amount"`
	// PlanAmount is the part of Amount that moved plan credits, same sign;
	// the rest moved pay-as-you-go credits.
	PlanAmount int    `json:"plan_amount"`
	Operation  string `json:"operation"`
	// ReferenceType says what ReferenceID points to, e.g. task_id or job_id.
	ReferenceType string `json:"reference_type"`
	ReferenceID   string `json:"reference_id"`
	// BalanceAfter is the balance right after this movement.
	BalanceAfter *int64    `json:"balance_after"`
	CreatedAt    time.Time `json:"created_at"`
}

// TransactionSum totals one operation over the whole filtered range.
type TransactionSum struct {
	Operation string `json:"operation"`
	Total     int64  `json:"total"`
	Count     int64  `json:"count"`
}

// TransactionsPage is one page of credit movements, newest first.
type TransactionsPage struct {
	Transactions []*Transaction   `json:"transactions"`
	TotalCount   int64            `json:"total_count"` // across all pages
	Sums         []TransactionSum `json:"sums"`
}

// Transactions returns one page of credit movements: plan assignments,
// credit pack purchases, usage, refunds, expiry. Sums covers the whole range, not just the page.
func (c *Client) Transactions(ctx context.Context, opts *TransactionsOptions) (*TransactionsPage, error) {
	q := url.Values{}
	if opts != nil {
		setIfValue(q, "operation", opts.Operation)
		setDate(q, "start_date", opts.Start)
		setDate(q, "end_date", opts.End)
		setCount(q, "page", opts.Page)
		setCount(q, "limit", opts.Limit)
	}
	var out TransactionsPage
	if err := c.do(ctx, request{method: http.MethodGet, path: "/users/@me/transactions", query: q}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// AnalyticsOptions picks the range and grouping of Analytics. Zero values
// use the API defaults: the last 30 days, daily, every task type.
type AnalyticsOptions struct {
	// Start and End are UTC days, End inclusive, at most 365 days apart.
	Start time.Time
	End   time.Time
	// Interval is hourly, daily, weekly, or monthly.
	Interval string
	// Module restricts the figures to one task type.
	Module string
}

// AnalyticsCounts are the task counts and net credits of one slice. Failed
// tasks are refunded and count 0 credits.
type AnalyticsCounts struct {
	Total       int64 `json:"total"`
	Completed   int64 `json:"completed"`
	Failed      int64 `json:"failed"`
	CreditsUsed int64 `json:"credits_used"`
}

// StatusCodeBreakdown counts tasks by the HTTP status the target answered.
// StatusCode 0 means no answer at all (timeout, DNS failure).
type StatusCodeBreakdown struct {
	StatusCode           int     `json:"status_code"`
	Count                int64   `json:"count"`
	Completed            int64   `json:"completed"`
	Failed               int64   `json:"failed"`
	CreditsUsed          int64   `json:"credits_used"`
	AvgCreditsPerRequest float64 `json:"avg_credits_per_request"`
}

// PreviousPeriod holds the figures of the equally long period before the
// range, and the change in percent.
type PreviousPeriod struct {
	CreditsUsed             int64   `json:"credits_used"`
	FulfilledRequests       int64   `json:"fulfilled_requests"`
	FailedRequests          int64   `json:"failed_requests"`
	FailedPercentage        float64 `json:"failed_percentage"`
	EfficiencyScore         float64 `json:"efficiency_score"`
	CreditsUsedChange       float64 `json:"credits_used_change"`
	FulfilledRequestsChange float64 `json:"fulfilled_requests_change"`
	FailedRequestsChange    float64 `json:"failed_requests_change"`
	EfficiencyScoreChange   float64 `json:"efficiency_score_change"`
}

// AnalyticsSummary holds the totals of the range.
type AnalyticsSummary struct {
	TotalTasks        int64   `json:"total_tasks"`
	CreditsUsed       int64   `json:"credits_used"`
	FulfilledRequests int64   `json:"fulfilled_requests"`
	FailedRequests    int64   `json:"failed_requests"`
	FailedPercentage  float64 `json:"failed_percentage"`
	// SuccessRate is completed tasks as a percentage of all tasks.
	SuccessRate float64 `json:"success_rate"`
	// EfficiencyScore is credits spent on completed tasks as a percentage of
	// all credits spent.
	EfficiencyScore      float64         `json:"efficiency_score"`
	AvgCreditsPerRequest float64         `json:"avg_credits_per_request"`
	AvgDurationMs        float64         `json:"avg_duration_ms"`
	PreviousPeriod       *PreviousPeriod `json:"previous_period"`
}

// TimeseriesEntry is one bucket of the time series.
type TimeseriesEntry struct {
	Period string `json:"period"`
	AnalyticsCounts
}

// ModuleBreakdown is the usage of one task type.
type ModuleBreakdown struct {
	Module string `json:"module"`
	AnalyticsCounts
	SuccessRate          float64               `json:"success_rate"`
	AvgCreditsPerRequest float64               `json:"avg_credits_per_request"`
	AvgDurationMs        float64               `json:"avg_duration_ms"`
	StatusCodes          []StatusCodeBreakdown `json:"status_codes"`
}

// TargetBreakdown is the usage of one target host.
type TargetBreakdown struct {
	Target string `json:"target"`
	AnalyticsCounts
}

// Analytics is the usage of the account over a date range. Percentages are
// 0-100.
type Analytics struct {
	Summary      AnalyticsSummary      `json:"summary"`
	Timeseries   []TimeseriesEntry     `json:"timeseries"`
	ByModule     []ModuleBreakdown     `json:"by_module"`
	TopTargets   []TargetBreakdown     `json:"top_targets"`
	ByStatusCode []StatusCodeBreakdown `json:"by_status_code"`
}

// Analytics returns totals, a time series, and breakdowns by task type,
// target host, and HTTP status code over a date range.
func (c *Client) Analytics(ctx context.Context, opts *AnalyticsOptions) (*Analytics, error) {
	q := url.Values{}
	if opts != nil {
		setDate(q, "start_date", opts.Start)
		setDate(q, "end_date", opts.End)
		setIfValue(q, "interval", opts.Interval)
		setIfValue(q, "module", opts.Module)
	}
	var out Analytics
	if err := c.do(ctx, request{method: http.MethodGet, path: "/task/analytics/dashboard", query: q}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
