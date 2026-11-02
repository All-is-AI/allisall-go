// Command list_client_outcome_types prints every outcome type a client's events carry.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"

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
	// Every outcome type the client's events carry, page by page.
	page, err := client.OutcomeTypes.ListForClient(ctx, clientID, nil)
	if err != nil {
		return err
	}
	for usage, err := range page.All(ctx) {
		if err != nil {
			return err
		}
		fmt.Printf("%s: %d events, quantity %g\n", usage.OutcomeType, usage.Events, usage.Quantity)
	}
	return nil
}
