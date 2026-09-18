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
| A question for an AI engine | `Ask` | yes |

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

## Map

```go
site, err := client.Map(ctx, &datafuel.MapRequest{URL: "https://example.com", Search: "blog", Limit: 500})
for _, link := range site.Links { fmt.Println(link.URL) }
```

An empty `site.Links` comes with a `site.Reason`. `no_links_on_page` usually means the navigation is rendered client-side.

## Errors

All work with `errors.Is` and `errors.As`.

- `datafuel.ErrNoAPIKey`: no key was passed and `DATAFUEL_API_KEY` is empty. Returned before any request.
- `*datafuel.APIError`: the API refused the request. Sentinels: `ErrUnauthorized`, `ErrInsufficientCredits`, `ErrRateLimited`, `ErrNotFound`, `ErrInvalidAttributes`, `ErrIdempotencyKeyReused`.
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

Full API reference: https://scraping-api.datafuel.ai/docs

## License

MIT, see [LICENSE](LICENSE).
