package datafuel

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"testing"
)

func scrapeAttributes(t *testing.T, opts ScrapeOptions) map[string]any {
	t.Helper()
	var attrs map[string]any
	client := fakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		attrs = readBody(t, r)["attributes"].(map[string]any)
		io.WriteString(w, `{"id":"t1","status":"completed","result":{"data":"ok"}}`)
	})
	if _, err := client.Scrape(context.Background(), &ScrapeRequest{URL: "https://example.com", ScrapeOptions: opts}); err != nil {
		t.Fatal(err)
	}
	return attrs
}

func TestJSInstructionsTravelAsAnOrderedArray(t *testing.T) {
	attrs := scrapeAttributes(t, ScrapeOptions{
		JSRendering: true,
		JSInstructions: []map[string]any{
			{"click": "#more"},
			{"wait_ms": 500},
			{"click": "#more"},
		},
	})
	raw, _ := json.Marshal(attrs["js_instructions"])
	if string(raw) != `[{"click":"#more"},{"wait_ms":500},{"click":"#more"}]` {
		t.Fatalf("js_instructions = %s", raw)
	}

	attrs = scrapeAttributes(t, ScrapeOptions{JSInstructions: map[string]any{"wait_ms": 2000}})
	raw, _ = json.Marshal(attrs["js_instructions"])
	if string(raw) != `{"wait_ms":2000}` {
		t.Fatalf("legacy js_instructions = %s", raw)
	}
}

func TestBlockResourceTakesOneOrSeveral(t *testing.T) {
	attrs := scrapeAttributes(t, ScrapeOptions{BlockResource: "Image"})
	if attrs["block_resource"] != "Image" {
		t.Fatalf("single block_resource = %#v", attrs["block_resource"])
	}

	attrs = scrapeAttributes(t, ScrapeOptions{BlockResources: []string{"Image", "Font"}})
	if !reflect.DeepEqual(attrs["block_resource"], []any{"Image", "Font"}) {
		t.Fatalf("block_resource list = %#v", attrs["block_resource"])
	}

	attrs = scrapeAttributes(t, ScrapeOptions{BlockResource: "Media", BlockResources: []string{"Image"}})
	if !reflect.DeepEqual(attrs["block_resource"], []any{"Image"}) {
		t.Fatalf("BlockResources must win, got %#v", attrs["block_resource"])
	}

	attrs = scrapeAttributes(t, ScrapeOptions{})
	if _, ok := attrs["block_resource"]; ok {
		t.Fatalf("unset block_resource sent: %v", attrs)
	}
}

func TestAIModelIsOptional(t *testing.T) {
	attrs := scrapeAttributes(t, ScrapeOptions{AI: &AIOptions{Prompt: "p", Provider: "openai", APIKey: "sk"}})
	if attrs["ai_provider"] != "openai" || attrs["ai_api_key"] != "sk" {
		t.Fatalf("attributes = %v", attrs)
	}
	if _, ok := attrs["ai_model"]; ok {
		t.Fatalf("empty model must be omitted: %v", attrs)
	}
}

func TestJSInstructionsCatalogue(t *testing.T) {
	client := fakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/config/js-instructions" {
			t.Errorf("got %s %s", r.Method, r.URL.Path)
		}
		io.WriteString(w, `{"instructions":[{"action":"fill","description":"Type text into an input.","value":"array",
			"args":[{"name":"selector","type":"string","required":true},{"name":"text","type":"string","required":true}],
			"iframe":true,"example":{"fill":["input[name=q]","laptops"]}}]}`)
	})
	actions, err := client.JSInstructions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(actions) != 1 || actions[0].Action != "fill" || actions[0].Value != "array" || !actions[0].IFrame ||
		len(actions[0].Args) != 2 || !actions[0].Args[1].Required || len(actions[0].Example) == 0 {
		t.Fatalf("actions = %+v", actions)
	}
}

func TestAIProviders(t *testing.T) {
	client := fakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/config/ai-providers" {
			t.Errorf("path = %s", r.URL.Path)
		}
		io.WriteString(w, `{"providers":[{"name":"openai","models":["gpt-4o"]},{"name":"google","models":["gemini-2.5-flash"]}]}`)
	})
	providers, err := client.AIProviders(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(providers) != 2 || providers[0].Name != "openai" || providers[0].Models[0] != "gpt-4o" {
		t.Fatalf("providers = %+v", providers)
	}
}

func TestProxyLocationsAndASNs(t *testing.T) {
	client := fakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/config/proxy/locations":
			if r.URL.RawQuery != "proxy_type=Premium" {
				t.Errorf("locations query = %q", r.URL.RawQuery)
			}
			io.WriteString(w, `[{"code":"us","name":"United States","regions":[{"code":"ny","name":"New York",
				"cities":[{"code":"newyork","name":"New York"}]}]}]`)
		case "/config/proxy/asn":
			if r.URL.RawQuery != "country=US" {
				t.Errorf("asn query = %q", r.URL.RawQuery)
			}
			io.WriteString(w, `[{"code":"7922","name":"Comcast Cable"}]`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})
	countries, err := client.ProxyLocations(context.Background(), ProxyPremium)
	if err != nil {
		t.Fatal(err)
	}
	if len(countries) != 1 || countries[0].Regions[0].Cities[0].Code != "newyork" {
		t.Fatalf("countries = %+v", countries)
	}
	asns, err := client.ProxyASNs(context.Background(), "US", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(asns) != 1 || asns[0].Code != "7922" {
		t.Fatalf("asns = %+v", asns)
	}
}

func TestCheckProtectionPostsTheURLList(t *testing.T) {
	client := fakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/filter/check" {
			t.Errorf("got %s %s", r.Method, r.URL.Path)
		}
		var urls []string
		if err := json.NewDecoder(r.Body).Decode(&urls); err != nil || len(urls) != 1 || urls[0] != "https://example.com/p/1" {
			t.Errorf("body = %v (%v)", urls, err)
		}
		io.WriteString(w, `[{"host":"example.com","path":"/p/1","protection_type":"cloudflare"}]`)
	})
	checks, err := client.CheckProtection(context.Background(), []string{"https://example.com/p/1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(checks) != 1 || checks[0].Host != "example.com" || checks[0].ProtectionType != "cloudflare" {
		t.Fatalf("checks = %+v", checks)
	}
}

func TestHealthReturnsADegradedReportWithoutAnError(t *testing.T) {
	calls := 0
	client := fakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/healthz" || r.URL.RawQuery != "deep=1" {
			t.Errorf("got %s?%s", r.URL.Path, r.URL.RawQuery)
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		io.WriteString(w, `{"status":"degraded","checks":{"postgres":{"status":"ok","latency_ms":1},"redis":{"status":"fail","latency_ms":2000}}}`)
	})
	health, err := client.Health(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	if health.OK() || health.Status != "degraded" || health.Checks["redis"].Status != "fail" || health.Checks["redis"].LatencyMs != 2000 {
		t.Fatalf("health = %+v", health)
	}
	if calls != 1 {
		t.Fatalf("a degraded report must not be retried, got %d calls", calls)
	}
}

func TestHealthShallow(t *testing.T) {
	client := fakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawQuery != "" {
			t.Errorf("query = %q", r.URL.RawQuery)
		}
		io.WriteString(w, `{"status":"ok"}`)
	})
	health, err := client.Health(context.Background(), false)
	if err != nil || !health.OK() || health.Checks != nil {
		t.Fatalf("health = %+v, err = %v", health, err)
	}
}

func TestHealthUnavailableWithoutAReportIsAnError(t *testing.T) {
	client := fakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		io.WriteString(w, `upstream connect error`)
	})
	_, err := client.Health(context.Background(), true)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("err = %v", err)
	}
}
