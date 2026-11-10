// Command list_event_arrivals prints the latest events to arrive, then how
// many have arrived since.
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
	// The latest arrivals, newest first.
	latest, err := client.Events.ListArrivals(ctx, &allisall.ListEventArrivalsParams{Limit: new(20)})
	if err != nil {
		return err
	}
	for _, event := range latest.Data {
		fmt.Printf("%s %s\n", event.CreatedAt.Format(time.RFC3339), event.EventType)
	}
	// Later, ask how many arrived since: send the previous AsOf as Since.
	since, err := client.Events.ListArrivals(ctx, &allisall.ListEventArrivalsParams{Since: &latest.Body.AsOf, Limit: new(1)})
	if err != nil {
		return err
	}
	if since.Body.Count != nil {
		fmt.Printf("%d new events\n", *since.Body.Count)
	}
	return nil
}
