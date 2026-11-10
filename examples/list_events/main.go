// Command list_events prints every event of August 2026 in the workspace,
// newest first.
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
	// Every page reads the same events, however many arrive meanwhile.
	page, err := client.Events.List(ctx, &allisall.ListEventsParams{
		From: new(time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)),
		To:   new(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)),
	})
	if err != nil {
		return err
	}
	for event, err := range page.All(ctx) {
		if err != nil {
			return err
		}
		clientName := "no client"
		if event.ClientName != nil {
			clientName = *event.ClientName
		}
		fmt.Printf("%s %s for %s\n", event.Timestamp.Format(time.RFC3339), event.EventType, clientName)
	}
	return nil
}
