// Command ingest sends one event to the allisall API and prints the outcome.
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

	events := []allisall.EventInput{{
		// A fixed id: run the program again and the API counts a duplicate.
		EventID:   "quickstart-go-1",
		Timestamp: time.Now(),
		AgentKey:  "quickstart-agent",
		Category:  allisall.EventCategorySession,
		EventType: "session_started",
	}}

	summary, err := client.Events.Ingest(context.Background(), events)
	if err != nil {
		// The API refused the batch, for example an unknown key.
		var apiErr *allisall.APIError
		if errors.As(err, &apiErr) {
			log.Fatalf("the API refused the batch: %d %s (request %s)", apiErr.Status, apiErr.Code, apiErr.RequestID)
		}
		log.Fatal(err)
	}

	fmt.Printf("accepted %d, duplicates %d, rejected %d\n", summary.Accepted, summary.Duplicates, summary.Rejected)
	for _, row := range summary.Errors {
		fmt.Printf("event %d: %s at %s: %s\n", row.Index, row.Code, row.Pointer, row.Detail)
	}
}
