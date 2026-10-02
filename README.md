# datafuel-go

Go client for the [DataFuel](https://datafuel.ai) scraping API. No dependencies outside the standard library. Go 1.23+.

```bash
go get github.com/Datafuel-ai/datafuel-go
```

```go
client := datafuel.New(os.Getenv("DATAFUEL_API_KEY"))

markdown, err := client.Markdown(ctx, "https://example.com")
```

## Pick the call

| You have | Call | Blocks? |
|---|---|---|
| One URL | `Scrape` / `Markdown` | yes |
| A site, need its URL list | `Map` | yes |
| A start URL, need many pages | `Crawl`, or `StartCrawl` + `WaitCrawl` + `CrawlPages` | `Crawl` does |
| A list of known URLs | `RunJob`, or `CreateJob` + `WaitJob` + `JobResults` | `RunJob` does |
| A Google search | `Search`, or `CreateSearchJob` for two or more queries | `Search` does |
| A question for an AI engine | `Ask`, or `CreateAskJob` for two or more prompts | `Ask` does |
| A task ID from a job or crawl | `GetTask` / `WaitTask` | `WaitTask` does |
| Earlier jobs, tasks, usage | `ListJobs` / `ListTasks` / `Analytics` / `Transactions` | yes |
| What a request may contain | `JSInstructions` / `AIProviders` / `ProxyLocations` / `ProxyASNs` / `Capabilities` | yes |
| The anti-bot wall in front of a URL | `CheckProtection` | yes |

Start with plain `Scrape`. Turn on `JSRendering` only when the page comes back empty: it is slower and costs five times the credits on a Basic proxy. `Map` a section before you `Crawl` it, it costs one credit and tells you how big it is.

## Scrape

```go
res, err := client.Scrape(ctx, &datafuel.ScrapeRequest{
    URL:   "https://shop.example.com/p/42",
    Proxy: datafuel.Proxy{Type: datafuel.ProxyPremium, Country: "US"},
    ScrapeOptions: datafuel.ScrapeOptions{
        JSRendering:     true,
        WaitForSelector: "#price",
        Extract:         map[string]string{"title": "h1", "price": "#price"},
    },
})
if errors.Is(err, datafuel.ErrBlocked) {
    // 403/429/503 or an anti-bot wall. Refunded. res.Protection names the vendor.
}

var fields map[string][]string
err = res.Decode(&fields)
```

`Result` carries the metadata next to the content. Read it first:

| Field | Meaning |
|---|---|
| `StatusCode` | What the target answered. A 404 still completes and bills. |
| `FinalURL`, `Redirected` | Where the page really came from. |
| `Blocked`, `Protection` | The target refused the request. The task failed and was refunded. |
| `CreditsUsed` | Charged for this task, 0 when it failed. |

`res.Text()` returns html or markdown, `res.Decode(&v)` structured output, `res.Image()` screenshot bytes.

With `JSRendering`, `JSInstructions` runs browser actions before the capture. Pass a slice of single-action maps: they run in the order you list them and an action can repeat. `BlockResources` skips resource types you do not need (`BlockResource` takes a single one).

```go
ScrapeOptions: datafuel.ScrapeOptions{
    JSRendering: true,
    JSInstructions: []map[string]any{
        {"fill": []string{"input[name=q]", "laptops"}},
        {"click": "button[type=submit]"},
        {"wait_ms": 1000},
    },
    BlockResources: []string{"Image", "Font", "Media"},
},
```

The older form, one map keyed by action, still works, but its order is not guaranteed and an action cannot repeat. `client.JSInstructions(ctx)` lists the actions.

`AI: &datafuel.AIOptions{Prompt: ..., Provider: "openai", APIKey: ...}` post-processes the page with an LLM. `Provider` is required, `Model` is optional and must be one of those `client.AIProviders(ctx)` lists. Crawls reject `AI`.

## Crawl

```go
id, err := client.StartCrawl(ctx, &datafuel.CrawlRequest{
    URL:           "https://example.com/docs",
    MaxPages:      200,
    ExcludePaths:  []string{`\.pdf$`},
    ScrapeOptions: datafuel.ScrapeOptions{Format: datafuel.FormatMarkdown},
})
status, err := client.WaitCrawl(ctx, id) // status.StopReason: max_pages, max_depth_exhausted, ...

for page, err := range client.CrawlPages(ctx, id) {
    if err != nil { return err }
    if page.Err() != nil { continue } // failed or blocked page, refunded
    fmt.Println(page.URL, page.Depth, len(page.Text()))
}
```

`client.Crawl(ctx, req)` does all three in one call:

```go
crawl, err := client.Crawl(ctx, req)
fmt.Println(crawl.StopReason, crawl.TotalCost, len(crawl.Pages))
```

Check `StopReason`: `insufficient_credits` means the crawl ended early. If the context ends first, `crawl.ID` is still set. The crawl keeps running and billing server side, so pick it up with `WaitCrawl` and `CrawlPages`. `RunJob` does the same with `results.ID`.

Unset limits use the API defaults: 100 pages, depth 3, 5 pages in flight.

`client.CancelCrawl(ctx, id)` stops a running crawl, `client.CancelJob(ctx, id)` a job. Pending pages and tasks are refunded (`RefundedTasks`, `RefundedCredits`); those already in flight finish and bill. A finished one returns `ErrJobNotCancellable`.

## Jobs

```go
results, err := client.RunJob(ctx, &datafuel.JobRequest{
    URLs:          urls,
    ScrapeOptions: datafuel.ScrapeOptions{Format: datafuel.FormatMarkdown},
})
for _, task := range results.Tasks {
    if err := task.Err(); err != nil { /* this URL failed, the others did not */ }
}
```

A job needs at least two URLs; one fails with `ErrJobRequiresMultipleTargets`. Use `Scrape` for a single URL.

## Search

```go
res, err := client.Search(ctx, &datafuel.SearchRequest{Query: "best crm", Country: "us", Language: "en"})
var serp map[string]any
err = res.Decode(&serp)
```

Results are JSON unless you set `Format` to `FormatHTML` or `FormatMarkdown`. `Location`, `UULE` and `Lat`/`Lon` are mutually exclusive. Searches leave through DataFuel's own pool: pick the market with `Country` and `Language`; `ProxyCountry` is accepted but not used yet.

## Map

```go
site, err := client.Map(ctx, &datafuel.MapRequest{URL: "https://example.com", Search: "blog", Limit: 500})
for _, link := range site.Links { fmt.Println(link.URL) }
```

An empty `site.Links` comes with a `site.Reason`. `no_links_on_page` usually means the navigation is rendered client-side.

## Usage and history

```go
usage, err := client.Analytics(ctx, &datafuel.AnalyticsOptions{Interval: "weekly"})
fmt.Println(usage.Summary.SuccessRate, usage.Summary.AvgCreditsPerRequest)

page, err := client.ListJobs(ctx, &datafuel.ListOptions{Type: "crawl", Limit: 20})
failed, err := client.ListTasks(ctx, &datafuel.ListTasksOptions{
	ListOptions: datafuel.ListOptions{Status: datafuel.StatusFailed},
	JobID:       page.Jobs[0].ID,
})
history, err := client.Transactions(ctx, &datafuel.TransactionsOptions{Operation: "refund", Limit: 50})
```

`ListJobs` and `ListTasks` run newest first; pass `NextCursor` back as `Cursor` until it is empty. Task items carry no result: call `GetTask` for it. `Start` and `End` are sent as UTC days and `End` is inclusive. `Transactions` pages with `Page` and `Limit`, and its `Sums` total each operation over the whole range. In `Analytics`, `StatusCode` 0 means the target never answered (timeout, DNS).

```go
credits, err := client.Balance(ctx)
split, err := client.BalanceSplit(ctx)
fmt.Println(split.Plan, split.Payg)
```

`Balance` is what you can spend. It is made of plan credits and pay-as-you-go credits. Plan credits are spent first; unused ones roll over when the plan renews and expire if it is not renewed. Pay-as-you-go credits come from one-time credit packs (a `purchase` transaction), are spent after plan credits and never expire. Each transaction's `PlanAmount` is the part of `Amount` that moved plan credits.

## Account and config

```go
caps, err := client.Capabilities(ctx)                         // which task types and LLM engines are on
actions, err := client.JSInstructions(ctx)                    // browser actions JSInstructions accepts
providers, err := client.AIProviders(ctx)                     // providers and models AIOptions accepts
countries, err := client.ProxyLocations(ctx, datafuel.ProxyPremium) // countries, regions, cities
asns, err := client.ProxyASNs(ctx, "US", datafuel.ProxyPremium)
checks, err := client.CheckProtection(ctx, []string{"https://shop.example.com/"}) // anti-bot vendor per URL, nothing scraped
health, err := client.Health(ctx, true)                       // health.OK() is false while a dependency is down
```

## Errors

All work with `errors.Is` and `errors.As`.

- `datafuel.ErrNoAPIKey`: no key was passed and `DATAFUEL_API_KEY` is empty. Returned before any request.
- `*datafuel.APIError`: the API refused the request. Sentinels: `ErrUnauthorized`, `ErrInsufficientCredits`, `ErrRateLimited`, `ErrNotFound`, `ErrInvalidAttributes`, `ErrIdempotencyKeyReused`, `ErrJobRequiresMultipleTargets`, `ErrJobNotCancellable`, `ErrInvalidQueryParam`.
- `ErrTaskStillProcessing`: the API answered 202, the task has not finished. `Scrape`, `Map`, `Search` and `Ask` handle it by re-sending with the same `Idempotency-Key` until the task is done, so you only see it, together with the context error, when your context ends first. Send the request again with the same `IdempotencyKey` to pick the task up.
- `ErrModuleUnavailable`, `ErrEngineUnavailable`: an operator switched a task type or LLM engine off, e.g. during a provider outage. The reason is in the error message, nothing is charged, and the SDK does not retry. `client.Capabilities(ctx)` lists what is on.
- `*datafuel.TaskError`: the API accepted the task but the page could not be scraped. Matches `ErrTaskFailed`, and `ErrBlocked` when the target refused. The `Result` is returned together with the error. Failed tasks are refunded.

## Retries and idempotency

Every write carries an `Idempotency-Key`, generated unless you set `IdempotencyKey` on the request. That makes retries safe: network errors and 429/502/503/504 answers are retried with backoff, reusing the key, so a retry attaches to the task already running instead of charging twice. Other 4xx errors are never retried.

A key replays its stored result, failures included. To run a failed scrape again, send a new request rather than the same key.

## Options

```go
datafuel.New(key,
    datafuel.WithBaseURL("https://scraping-api.staging.datafuel.ai/api/v1"),
    datafuel.WithMaxRetries(4),
    datafuel.WithPollInterval(5*time.Second),
    datafuel.WithHTTPClient(myClient),
    datafuel.WithUserAgent("my-app/1.2"),
)
```

Do not put a short `Timeout` on the HTTP client: `Scrape` blocks until the page is ready. Bound calls with the context.

## Try it

```bash
DATAFUEL_API_KEY=df_key_... go run ./examples/quickstart https://example.com
```

Guides and the full API reference: https://docs.datafuel.ai

## License

MIT, see [LICENSE](LICENSE).
