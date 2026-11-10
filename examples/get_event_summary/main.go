// Command get_event_summary counts August 2026's events in Tokyo, in all,
// per day and per category.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	allisall "github.com/All-is-AI/allisall-go"
)

func main() {
	// Reads the API key from AIA_API_KEY.
	client, err := allisall.NewClient()
	if err != nil {
		log.Fatal(err)
	}

	if err := run(context.Background(), client); err != nil {
		// The API refused the request, for example a key without the read scope.
		var apiErr *allisall.APIError
		if errors.As(err, &apiErr) {
			log.Fatalf("the API refused the request: %d %s (request %s)", apiErr.Status, apiErr.Code, apiErr.RequestID)
		}
		log.Fatal(err)
	}
}

func run(ctx context.Context, client *allisall.Client) error {
	// Each day runs from local midnight in Tokyo.
	tokyo := time.FixedZone("JST", 9*60*60)
	summary, err := client.Events.GetSummary(ctx, allisall.GetEventSummaryParams{
		From: time.Date(2026, 8, 1, 0, 0, 0, 0, tokyo),
		To:   time.Date(2026, 9, 1, 0, 0, 0, 0, tokyo),
	})
	if err != nil {
		return err
	}
	fmt.Printf("%d events\n", summary.Total)
	for _, day := range summary.Days {
		fmt.Printf("%s: %d\n", day.Start.Format(time.RFC3339), day.Count)
	}
	for _, category := range summary.Categories {
		fmt.Printf("%s: %d\n", category.Category, category.Count)
	}
	return nil
}
