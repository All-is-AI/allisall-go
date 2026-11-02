// Command get_client_anomalies prints what looks unusual in a client's events in August 2026.
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
	// What looks unusual in the client's events for August 2026.
	anomalies, err := client.Analytics.GetAnomaliesForClient(ctx, clientID, allisall.GetClientAnomaliesParams{
		From: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		To:   time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		return err
	}
	fmt.Printf("days without outcomes: %d\n", len(anomalies.ZeroOutcomeDays))
	fmt.Printf("cost spikes: %d\n", len(anomalies.CostSpikes))
	// The list holds at most 100 outcome types; the omitted count is the rest.
	unmapped := len(anomalies.UnmappedOutcomeTypes) + anomalies.UnmappedOutcomeTypesOmitted
	fmt.Printf("unmapped outcome types: %d\n", unmapped)
	return nil
}
