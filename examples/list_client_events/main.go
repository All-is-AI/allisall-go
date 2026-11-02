// Command list_client_events prints every event attributed to a client in August 2026.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	allisall "github.com/All-is-AI/allisall-go"
)

// Replace CLIENT_ID with the id of one of your clients.
const clientID = "CLIENT_ID"

func main() {
	// Reads the API key from AIA_API_KEY.
	client, err := allisall.NewClient()
	if err != nil {
		log.Fatal(err)
	}

	if err := run(context.Background(), client); err != nil {
		// The API refused the request, for example an unknown id.
		var apiErr *allisall.APIError
		if errors.As(err, &apiErr) {
			log.Fatalf("the API refused the request: %d %s (request %s)", apiErr.Status, apiErr.Code, apiErr.RequestID)
		}
		log.Fatal(err)
	}
}

func run(ctx context.Context, client *allisall.Client) error {
	// Every event attributed to the client in August 2026, page by page.
	page, err := client.Events.ListForClient(ctx, clientID, allisall.ListClientEventsParams{
		From: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		To:   time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		return err
	}
	for event, err := range page.All(ctx) {
		if err != nil {
			return err
		}
		fmt.Printf("%s %s: %s %s\n", event.Timestamp.Format(time.RFC3339), event.AgentName, event.Category, event.EventType)
	}
	return nil
}
