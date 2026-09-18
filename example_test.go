package datafuel_test

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/Datafuel-ai/datafuel-go"
)

func Example() {
	client := datafuel.New("") // reads DATAFUEL_API_KEY

	markdown, err := client.Markdown(context.Background(), "https://example.com")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(markdown)
}

func ExampleClient_Scrape() {
	client := datafuel.New("df_key_...")

	res, err := client.Scrape(context.Background(), &datafuel.ScrapeRequest{
		URL:   "https://shop.example.com/p/42",
		Proxy: datafuel.Proxy{Type: datafuel.ProxyPremium, Country: "US"},
		ScrapeOptions: datafuel.ScrapeOptions{
			JSRendering:     true,
			WaitForSelector: "#price",
			Extract:         map[string]string{"title": "h1", "price": "#price"},
		},
	})
	switch {
	case errors.Is(err, datafuel.ErrBlocked):
		log.Fatalf("blocked by %s, refunded", res.Protection)
	case err != nil:
		log.Fatal(err)
	}

	var fields map[string][]string
	if err := res.Decode(&fields); err != nil {
		log.Fatal(err)
	}
	fmt.Println(fields["title"], fields["price"], res.CreditsUsed)
}

func ExampleClient_CrawlPages() {
	client := datafuel.New("df_key_...")
	ctx := context.Background()

	id, err := client.StartCrawl(ctx, &datafuel.CrawlRequest{
		URL:           "https://example.com/docs",
		MaxPages:      200,
		ExcludePaths:  []string{`\.pdf$`},
		ScrapeOptions: datafuel.ScrapeOptions{Format: datafuel.FormatMarkdown},
	})
	if err != nil {
		log.Fatal(err)
	}
	if _, err := client.WaitCrawl(ctx, id); err != nil {
		log.Fatal(err)
	}
	for page, err := range client.CrawlPages(ctx, id) {
		if err != nil {
			log.Fatal(err)
		}
		if page.Err() != nil {
			continue // failed or blocked page, refunded
		}
		fmt.Println(page.URL, len(page.Text()))
	}
}

func ExampleClient_RunJob() {
	client := datafuel.New("df_key_...")

	results, err := client.RunJob(context.Background(), &datafuel.JobRequest{
		URLs:          []string{"https://example.com/a", "https://example.com/b"},
		ScrapeOptions: datafuel.ScrapeOptions{Format: datafuel.FormatMarkdown},
	})
	if err != nil {
		log.Fatal(err)
	}
	for _, task := range results.Tasks {
		fmt.Println(task.FinalURL, task.Err())
	}
}
