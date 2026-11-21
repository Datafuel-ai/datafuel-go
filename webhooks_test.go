package datafuel

import (
	"context"
	"errors"
	"io"
	"net/http"
	"testing"
)

func TestJobAndCrawlSendWebhook(t *testing.T) {
	client := fakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		hook, _ := readBody(t, r)["webhook"].(map[string]any)
		if hook["url"] != "https://hooks.test/df" || hook["metadata"].(map[string]any)["run"] != "42" {
			t.Errorf("webhook = %v", hook)
		}
		if r.URL.Path == "/crawl" {
			w.WriteHeader(http.StatusAccepted)
			io.WriteString(w, `{"job_id":"c1"}`)
			return
		}
		io.WriteString(w, `{"id":"j1"}`)
	})
	hook := &Webhook{URL: "https://hooks.test/df", Metadata: map[string]string{"run": "42"}}
	ctx := context.Background()
	if _, err := client.CreateJob(ctx, &JobRequest{URLs: []string{"https://a.io", "https://b.io"}, Webhook: hook}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.StartCrawl(ctx, &CrawlRequest{URL: "https://a.io", Webhook: hook}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.CreateAskJob(ctx, &AskJobRequest{Prompts: []string{"a", "b"}, Webhook: hook, AskRequest: AskRequest{Engine: EngineOpenAI}}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.CreateSearchJob(ctx, &SearchJobRequest{Queries: []string{"a", "b"}, Webhook: hook}); err != nil {
		t.Fatal(err)
	}
}

func TestJobStatusDecodesWebhookURL(t *testing.T) {
	client := fakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"status":"completed","webhook_url":"https://hooks.test/df"}`)
	})
	status, err := client.GetJob(context.Background(), "j1")
	if err != nil || status.WebhookURL != "https://hooks.test/df" {
		t.Fatalf("status=%+v err=%v", status, err)
	}
}

func TestWebhookRoutes(t *testing.T) {
	const ep = `{"id":"e1","url":"https://hooks.test/df","event_types":["crawl.*"],"enabled":true,"created_at":"2026-11-17T10:00:00Z","updated_at":"2026-11-17T10:00:00Z"}`
	const delivery = `{"id":"d1","event_id":"ev1","event_type":"webhook.test","url":"https://hooks.test/df","status":"pending","attempts":0,"created_at":"2026-11-17T10:00:00Z","updated_at":"2026-11-17T10:00:00Z"}`
	const secret = `{"secret":"whsec_abc","created_at":"2026-11-17T10:00:00Z"}`
	var seen []string
	client := fakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		route := r.Method + " " + r.URL.Path
		seen = append(seen, route)
		switch route {
		case "GET /webhooks/secret", "POST /webhooks/secret/rotate", "POST /webhooks/endpoints/e1/secret/rotate":
			io.WriteString(w, secret)
		case "POST /webhooks/endpoints":
			if body := readBody(t, r); body["url"] != "https://hooks.test/df" || len(body["event_types"].([]any)) != 1 {
				t.Errorf("body = %v", body)
			}
			w.WriteHeader(http.StatusCreated)
			io.WriteString(w, ep[:len(ep)-1]+`,"secret":"whsec_ep"}`)
		case "GET /webhooks/endpoints":
			io.WriteString(w, `{"endpoints":[`+ep+`]}`)
		case "GET /webhooks/endpoints/e1":
			io.WriteString(w, ep)
		case "PATCH /webhooks/endpoints/e1":
			if body := readBody(t, r); len(body) != 1 || body["enabled"] != false {
				t.Errorf("body = %v", body)
			}
			io.WriteString(w, ep)
		case "DELETE /webhooks/endpoints/e1":
			w.WriteHeader(http.StatusNoContent)
		case "POST /webhooks/endpoints/e1/test", "POST /webhooks/deliveries/d1/retry":
			w.WriteHeader(http.StatusAccepted)
			io.WriteString(w, delivery)
		case "GET /webhooks/deliveries":
			if q := r.URL.Query(); q.Get("job_id") != "j1" || q.Get("status") != "failed" || q.Get("limit") != "5" {
				t.Errorf("query = %s", r.URL.RawQuery)
			}
			io.WriteString(w, `{"deliveries":[`+delivery+`],"next_cursor":"n1"}`)
		default:
			t.Errorf("unexpected %s", route)
		}
	})
	ctx := context.Background()
	if s, err := client.WebhookSecret(ctx); err != nil || s.Secret != "whsec_abc" {
		t.Fatalf("secret=%+v err=%v", s, err)
	}
	if _, err := client.RotateWebhookSecret(ctx); err != nil {
		t.Fatal(err)
	}
	created, err := client.CreateWebhookEndpoint(ctx, &CreateWebhookEndpointRequest{URL: "https://hooks.test/df", EventTypes: []string{"crawl.*"}})
	if err != nil || created.Secret != "whsec_ep" || created.ID != "e1" {
		t.Fatalf("created=%+v err=%v", created, err)
	}
	if eps, err := client.ListWebhookEndpoints(ctx); err != nil || len(eps) != 1 {
		t.Fatalf("endpoints=%v err=%v", eps, err)
	}
	if _, err := client.GetWebhookEndpoint(ctx, "e1"); err != nil {
		t.Fatal(err)
	}
	off := false
	if _, err := client.UpdateWebhookEndpoint(ctx, "e1", &UpdateWebhookEndpointRequest{Enabled: &off}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.RotateWebhookEndpointSecret(ctx, "e1"); err != nil {
		t.Fatal(err)
	}
	if d, err := client.TestWebhookEndpoint(ctx, "e1"); err != nil || d.ID != "d1" {
		t.Fatalf("delivery=%+v err=%v", d, err)
	}
	page, err := client.ListWebhookDeliveries(ctx, &WebhookDeliveriesOptions{JobID: "j1", Status: "failed", Limit: 5})
	if err != nil || page.NextCursor != "n1" || len(page.Deliveries) != 1 {
		t.Fatalf("page=%+v err=%v", page, err)
	}
	if _, err := client.RetryWebhookDelivery(ctx, "d1"); err != nil {
		t.Fatal(err)
	}
	if err := client.DeleteWebhookEndpoint(ctx, "e1"); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 11 {
		t.Fatalf("routes hit: %v", seen)
	}
}

func TestWebhookWritesAreNotRetried(t *testing.T) {
	calls := 0
	client := fakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusServiceUnavailable)
		io.WriteString(w, `{"code":"WEBHOOKS_UNAVAILABLE","message":"down"}`)
	})
	_, err := client.RotateWebhookSecret(context.Background())
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code != "WEBHOOKS_UNAVAILABLE" || calls != 1 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
}
