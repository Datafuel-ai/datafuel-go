package datafuel

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// fakeAPI serves handler and returns a client pointed at it.
func fakeAPI(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return New("df_key_test", WithBaseURL(srv.URL), WithPollInterval(time.Millisecond), WithMaxRetries(2))
}

func readBody(t *testing.T, r *http.Request) map[string]any {
	t.Helper()
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatal(err)
	}
	body := map[string]any{}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("request body is not JSON: %v: %s", err, raw)
	}
	return body
}

func TestScrapeSendsEnvelopeAndDecodesResult(t *testing.T) {
	client := fakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/task" {
			t.Errorf("got %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("X-API-Key") != "df_key_test" {
			t.Errorf("missing api key header")
		}
		if r.Header.Get("Idempotency-Key") == "" {
			t.Errorf("writes must carry an Idempotency-Key")
		}
		body := readBody(t, r)
		if body["type"] != "unlocker" || body["proxy_type"] != "Premium" || body["proxy_country"] != "US" {
			t.Errorf("envelope = %v", body)
		}
		attrs := body["attributes"].(map[string]any)
		if attrs["url"] != "https://example.com" || attrs["result_format"] != "markdown" || attrs["js_rendering"] != true {
			t.Errorf("attributes = %v", attrs)
		}
		if attrs["include_images"] != false {
			t.Errorf("include_images=false must be sent, got %v", attrs["include_images"])
		}
		if attrs["extract_selector"] != `{"title":"h1"}` {
			t.Errorf("extract_selector must be a JSON object string, got %#v", attrs["extract_selector"])
		}
		if attrs["result_use_ai"] != true || attrs["result_ai_prompt"] != "list prices" {
			t.Errorf("AI options not flattened: %v", attrs)
		}
		if _, ok := attrs["wait_for_selector"]; ok {
			t.Errorf("zero options must be omitted: %v", attrs)
		}
		io.WriteString(w, `{"id":"t1","status":"completed","status_code":200,"final_url":"https://example.com/",
			"redirected":true,"credits_used":20,"result":{"data":"# Hello"}}`)
	})

	noImages := false
	res, err := client.Scrape(context.Background(), &ScrapeRequest{
		URL:   "https://example.com",
		Proxy: Proxy{Type: ProxyPremium, Country: "US"},
		ScrapeOptions: ScrapeOptions{
			Format: FormatMarkdown, JSRendering: true, IncludeImages: &noImages,
			Extract: map[string]string{"title": "h1"},
			AI:      &AIOptions{Prompt: "list prices"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Text() != "# Hello" || !res.Redirected || res.CreditsUsed != 20 || res.Pending() {
		t.Fatalf("result = %+v", res)
	}
}

func TestScrapeBlockedReturnsResultAndTaskError(t *testing.T) {
	client := fakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"id":"t1","status":"failed","status_code":403,"blocked":true,"protection":"cloudflare",
			"credits_used":0,"error":"blocked by cloudflare (status 403)",
			"result":{"status":"failed","error_detail":"blocked by cloudflare (status 403)","status_code":403}}`)
	})
	res, err := client.Scrape(context.Background(), &ScrapeRequest{URL: "https://example.com"})
	if !errors.Is(err, ErrBlocked) || !errors.Is(err, ErrTaskFailed) {
		t.Fatalf("err = %v", err)
	}
	var taskErr *TaskError
	if !errors.As(err, &taskErr) || taskErr.Result.Protection != "cloudflare" {
		t.Fatalf("TaskError missing envelope: %v", err)
	}
	if res == nil || res.StatusCode != 403 || res.CreditsUsed != 0 {
		t.Fatalf("result must come back with the error: %+v", res)
	}
}

func TestAPIErrorsMatchSentinels(t *testing.T) {
	client := fakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPaymentRequired)
		io.WriteString(w, `{"code":"INSUFFICIENT_CREDITS","message":"Not enough credits to run this task"}`)
	})
	_, err := client.Scrape(context.Background(), &ScrapeRequest{URL: "https://example.com"})
	if !errors.Is(err, ErrInsufficientCredits) || errors.Is(err, ErrUnauthorized) {
		t.Fatalf("err = %v", err)
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != 402 {
		t.Fatalf("APIError = %+v", apiErr)
	}
}

func TestRetriesReuseTheIdempotencyKey(t *testing.T) {
	var calls atomic.Int32
	var keys []string
	client := fakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		keys = append(keys, r.Header.Get("Idempotency-Key"))
		if calls.Add(1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		io.WriteString(w, `{"id":"t1","status":"completed","credits_used":1,"result":{"data":"ok"}}`)
	})
	res, err := client.Scrape(context.Background(), &ScrapeRequest{URL: "https://example.com"})
	if err != nil || res.Text() != "ok" {
		t.Fatalf("res=%v err=%v", res, err)
	}
	if len(keys) != 3 || keys[0] == "" || keys[0] != keys[1] || keys[1] != keys[2] {
		t.Fatalf("retries must reuse one key: %v", keys)
	}
}

func TestClientErrorsAreNotRetried(t *testing.T) {
	var calls atomic.Int32
	client := fakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, `{"code":"INVALID_ATTRIBUTES","message":"bad"}`)
	})
	_, err := client.Scrape(context.Background(), &ScrapeRequest{URL: "x"})
	if !errors.Is(err, ErrInvalidAttributes) || calls.Load() != 1 {
		t.Fatalf("err=%v calls=%d", err, calls.Load())
	}
}

func TestMapDecodesLinks(t *testing.T) {
	client := fakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/map" {
			t.Errorf("path = %s", r.URL.Path)
		}
		attrs := readBody(t, r)["attributes"].(map[string]any)
		if attrs["url"] != "https://example.com" || attrs["search"] != "blog" || attrs["limit"] != float64(50) {
			t.Errorf("attributes = %v", attrs)
		}
		io.WriteString(w, `{"id":"m1","status":"completed","credits_used":1,"result":{"data":{
			"url":"https://example.com","total":1,"credits":1,"sitemaps":["https://example.com/sitemap.xml"],
			"links":[{"url":"https://example.com/blog/a","source":"sitemap","lastmod":"2026-01-01"}]}}}`)
	})
	site, err := client.Map(context.Background(), &MapRequest{URL: "https://example.com", Search: "blog", Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if site.Total != 1 || site.Links[0].Source != "sitemap" || site.Task.ID != "m1" {
		t.Fatalf("site = %+v", site)
	}
}

func TestRunJobPollsUntilDone(t *testing.T) {
	var polls atomic.Int32
	client := fakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/job":
			body := readBody(t, r)
			if body["multithreaded"] != true {
				t.Errorf("jobs run concurrently by default: %v", body)
			}
			if urls := body["attributes"].(map[string]any)["urls"].([]any); len(urls) != 2 {
				t.Errorf("urls = %v", urls)
			}
			io.WriteString(w, `{"id":"j1"}`)
		case r.URL.Path == "/job/j1":
			status := "processing"
			if polls.Add(1) >= 3 {
				status = "completed"
			}
			io.WriteString(w, `{"status":"`+status+`","tasks_count":2}`)
		case r.URL.Path == "/job/j1/results":
			io.WriteString(w, `{"tasks_count":2,"tasks_failed":1,"tasks_complete":1,"tasks_result":[
				{"id":"a","status":"completed","credits_used":1,"result":{"data":"<html>"}},
				{"id":"b","status":"failed","credits_used":0,"error":"request timed out","result":{"status":"failed"}}]}`)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	})
	results, err := client.RunJob(context.Background(), &JobRequest{URLs: []string{"https://a.io", "https://b.io"}})
	if err != nil {
		t.Fatal(err)
	}
	if polls.Load() != 3 || len(results.Tasks) != 2 {
		t.Fatalf("polls=%d tasks=%d", polls.Load(), len(results.Tasks))
	}
	if results.Tasks[0].Err() != nil || !errors.Is(results.Tasks[1].Err(), ErrTaskFailed) {
		t.Fatalf("per-task errors wrong: %v / %v", results.Tasks[0].Err(), results.Tasks[1].Err())
	}
}

func TestCrawlFollowsCursors(t *testing.T) {
	client := fakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/crawl":
			attrs := readBody(t, r)["attributes"].(map[string]any)
			if attrs["max_pages"] != float64(3) || attrs["result_format"] != "markdown" {
				t.Errorf("attributes = %v", attrs)
			}
			if _, ok := attrs["max_depth"]; ok {
				t.Errorf("unset limits must be left to the API defaults: %v", attrs)
			}
			w.WriteHeader(http.StatusAccepted)
			io.WriteString(w, `{"job_id":"c1"}`)
		case r.URL.Path == "/crawl/c1":
			io.WriteString(w, `{"status":"completed","stop_reason":"max_pages","pages":{"done":3},"total_cost":3}`)
		case r.URL.Path == "/crawl/c1/results" && r.URL.Query().Get("cursor") == "":
			io.WriteString(w, `{"pages":[
				{"url":"https://a.io/","depth":0,"task_id":"p0","status":"completed","credits_used":1,"result":{"data":"root"}},
				{"url":"https://a.io/x","depth":1,"task_id":"p1","status":"completed","credits_used":1,"result":{"data":"x"}}],
				"next_cursor":"n1"}`)
		case r.URL.Path == "/crawl/c1/results" && r.URL.Query().Get("cursor") == "n1":
			io.WriteString(w, `{"pages":[{"url":"https://a.io/y","depth":1,"task_id":"p2","status":"failed","blocked":true,
				"credits_used":0,"result":{"status":"failed"}}]}`)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.String())
		}
	})
	crawl, err := client.Crawl(context.Background(), &CrawlRequest{
		URL: "https://a.io", MaxPages: 3, ScrapeOptions: ScrapeOptions{Format: FormatMarkdown},
	})
	if err != nil {
		t.Fatal(err)
	}
	if crawl.ID != "c1" || crawl.StopReason != "max_pages" || crawl.TotalCost != 3 {
		t.Fatalf("crawl status lost: %+v", crawl.CrawlStatus)
	}
	pages := crawl.Pages
	if len(pages) != 3 || pages[1].Text() != "x" || pages[1].Depth != 1 || pages[1].TaskID != "p1" {
		t.Fatalf("pages = %+v", pages)
	}
	if !errors.Is(pages[2].Err(), ErrBlocked) {
		t.Fatalf("blocked page must report ErrBlocked, got %v", pages[2].Err())
	}
}

func TestWaitStopsWithContext(t *testing.T) {
	client := fakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"status":"processing"}`)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	status, err := client.WaitCrawl(ctx, "c1")
	if !errors.Is(err, context.DeadlineExceeded) || status == nil || status.Status != StatusProcessing {
		t.Fatalf("status=%v err=%v", status, err)
	}
}

func TestResultHelpers(t *testing.T) {
	structured := &Result{Payload: Payload{Data: json.RawMessage(`{"title":["Hi"]}`)}}
	var fields map[string][]string
	if err := structured.Decode(&fields); err != nil || fields["title"][0] != "Hi" {
		t.Fatalf("Decode: %v %v", fields, err)
	}
	if structured.Text() != `{"title":["Hi"]}` {
		t.Fatalf("Text on structured data = %q", structured.Text())
	}
	shot := &Result{Payload: Payload{Data: json.RawMessage(`"aGk="`)}}
	if img, err := shot.Image(); err != nil || string(img) != "hi" {
		t.Fatalf("Image: %q %v", img, err)
	}
	stub := &Result{Payload: Payload{Status: StatusProcessing}}
	if !stub.Pending() || stub.Err() != nil {
		t.Fatalf("running stub must be pending without error")
	}
	if err := stub.Decode(&fields); err == nil {
		t.Fatal("Decode without data must fail")
	}
}

func TestBalance(t *testing.T) {
	client := fakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/users/@me/balance" {
			t.Errorf("path = %s", r.URL.Path)
		}
		io.WriteString(w, `{"balance":4200}`)
	})
	if got, err := client.Balance(context.Background()); err != nil || got != 4200 {
		t.Fatalf("balance=%d err=%v", got, err)
	}
}

func TestCancelledIsTerminal(t *testing.T) {
	client := fakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"status":"cancelled","stop_reason":"cancelled"}`)
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	status, err := client.WaitCrawl(ctx, "c1")
	if err != nil || status.Status != StatusCancelled {
		t.Fatalf("a cancelled crawl must end the wait: status=%v err=%v", status, err)
	}
}

func TestStickySessionTravelsInAttributes(t *testing.T) {
	client := fakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		body := readBody(t, r)
		attrs := body["attributes"].(map[string]any)
		if attrs["proxy_session_id"] != "s1" || attrs["proxy_ttl"] != float64(300) {
			t.Errorf("%s: session must be in attributes, got %v", r.URL.Path, attrs)
		}
		if _, ok := body["proxy_session_id"]; ok {
			t.Errorf("%s: the API ignores an envelope-level session id", r.URL.Path)
		}
		if body["proxy_country"] != "DE" {
			t.Errorf("%s: country belongs in the envelope: %v", r.URL.Path, body)
		}
		io.WriteString(w, `{"id":"t1","status":"completed","result":{"data":{"url":"https://a.io","links":[]}}}`)
	})
	proxy := Proxy{Country: "DE", SessionID: "s1", TTL: 300}
	if _, err := client.Scrape(context.Background(), &ScrapeRequest{URL: "https://a.io", Proxy: proxy}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Map(context.Background(), &MapRequest{URL: "https://a.io", Proxy: proxy}); err != nil {
		t.Fatal(err)
	}
}

func TestGetTaskStillProcessing(t *testing.T) {
	client := fakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		io.WriteString(w, `{"code":"TASK_STILL_PROCESSING","message":"Task is still processing"}`)
	})
	res, err := client.GetTask(context.Background(), "t9")
	if err != nil || !res.Pending() || res.ID != "t9" || res.Status != StatusProcessing {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func TestAskSendsPromptAndEngine(t *testing.T) {
	client := fakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		body := readBody(t, r)
		attrs := body["attributes"].(map[string]any)
		if body["type"] != "llm_scraping" || attrs["prompt"] != "best crm" || attrs["engine"] != "perplexity" || attrs["websearch"] != true {
			t.Errorf("body = %v", body)
		}
		io.WriteString(w, `{"id":"t1","status":"completed","credits_used":100,"result":{"data":"HubSpot"}}`)
	})
	res, err := client.Ask(context.Background(), &AskRequest{Prompt: "best crm", Engine: EnginePerplexity, WebSearch: true})
	if err != nil || res.Text() != "HubSpot" {
		t.Fatalf("res=%v err=%v", res, err)
	}
}

func TestNoAPIKeyFailsBeforeAnyRequest(t *testing.T) {
	t.Setenv("DATAFUEL_API_KEY", "")
	client := New("", WithBaseURL("http://127.0.0.1:1"), WithHTTPClient(nil))
	if _, err := client.Balance(context.Background()); !errors.Is(err, ErrNoAPIKey) {
		t.Fatalf("err = %v", err)
	}
}

func TestRunJobKeepsIDWhenContextEnds(t *testing.T) {
	client := fakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			io.WriteString(w, `{"id":"j7"}`)
			return
		}
		io.WriteString(w, `{"status":"processing"}`)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	results, err := client.RunJob(ctx, &JobRequest{URLs: []string{"https://a.io", "https://b.io"}})
	if !errors.Is(err, context.DeadlineExceeded) || results == nil || results.ID != "j7" {
		t.Fatalf("a timed-out job must still report its id: results=%+v err=%v", results, err)
	}
}

func TestBackoffNeverOverflows(t *testing.T) {
	for _, attempt := range []int{0, 4, 35, 63, 1000} {
		if d := backoff(attempt, 0); d <= 0 || d > 8*time.Second {
			t.Fatalf("backoff(%d) = %v", attempt, d)
		}
	}
	if d := backoff(0, 90*time.Second); d != 30*time.Second {
		t.Fatalf("Retry-After must be capped at 30s, got %v", d)
	}
}

func TestNilRequestsReturnAnError(t *testing.T) {
	client := New("df_key_test")
	ctx := context.Background()
	if _, err := client.Scrape(ctx, nil); !errors.Is(err, ErrNilRequest) {
		t.Fatalf("Scrape: %v", err)
	}
	if _, err := client.Map(ctx, nil); !errors.Is(err, ErrNilRequest) {
		t.Fatalf("Map: %v", err)
	}
	if _, err := client.Crawl(ctx, nil); !errors.Is(err, ErrNilRequest) {
		t.Fatalf("Crawl: %v", err)
	}
	if _, err := client.RunJob(ctx, nil); !errors.Is(err, ErrNilRequest) {
		t.Fatalf("RunJob: %v", err)
	}
	if _, err := client.Ask(ctx, nil); !errors.Is(err, ErrNilRequest) {
		t.Fatalf("Ask: %v", err)
	}
}

func TestSwitchedOffEngineIsNotRetried(t *testing.T) {
	var calls atomic.Int32
	client := fakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
		io.WriteString(w, `{"code":"ENGINE_UNAVAILABLE","message":"This engine is switched off at the moment: temporarily unavailable"}`)
	})
	_, err := client.Ask(context.Background(), &AskRequest{Prompt: "hi", Engine: EngineGoogleAIMode})
	if !errors.Is(err, ErrEngineUnavailable) || errors.Is(err, ErrModuleUnavailable) {
		t.Fatalf("err = %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("a deliberate switch-off must not be retried, got %d calls", calls.Load())
	}
}

func TestCapabilities(t *testing.T) {
	client := fakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/capabilities" {
			t.Errorf("path = %s", r.URL.Path)
		}
		io.WriteString(w, `{"modules":[{"name":"crawl","enabled":true}],
			"engines":[{"name":"openai","enabled":true},{"name":"google_ai_mode","enabled":false,"reason":"temporarily unavailable"}]}`)
	})
	caps, err := client.Capabilities(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !caps.EngineEnabled(EngineOpenAI) || caps.EngineEnabled(EngineGoogleAIMode) || caps.EngineEnabled("clippy") {
		t.Fatalf("caps = %+v", caps)
	}
	if caps.Engines[1].Reason != "temporarily unavailable" {
		t.Fatalf("reason lost: %+v", caps.Engines[1])
	}
}
