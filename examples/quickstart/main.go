// Quickstart: scrape one page as markdown and print the balance.
//
//	DATAFUEL_API_KEY=df_key_... go run ./examples/quickstart https://example.com
//
// Set DATAFUEL_BASE_URL to try another environment, e.g.
// https://scraping-api.staging.datafuel.ai/api/v1
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/Datafuel-ai/datafuel-go"
)

func main() {
	target := "https://example.com"
	if len(os.Args) > 1 {
		target = os.Args[1]
	}

	var opts []datafuel.Option
	if base := os.Getenv("DATAFUEL_BASE_URL"); base != "" {
		opts = append(opts, datafuel.WithBaseURL(base))
	}
	client := datafuel.New("", opts...)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	res, err := client.Scrape(ctx, &datafuel.ScrapeRequest{
		URL:           target,
		ScrapeOptions: datafuel.ScrapeOptions{Format: datafuel.FormatMarkdown},
	})
	switch {
	case errors.Is(err, datafuel.ErrBlocked):
		log.Fatalf("blocked (status %d, %s): refunded, retry with JSRendering or a Premium proxy", res.StatusCode, res.Protection)
	case err != nil:
		log.Fatal(err)
	}

	fmt.Printf("status %d, %d credits, %d ms, final url %s\n\n", res.StatusCode, res.CreditsUsed, res.DurationMs, res.FinalURL)
	fmt.Println(res.Text())

	balance, err := client.Balance(ctx)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("\nbalance: %d credits\n", balance)
}
