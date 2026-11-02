// Command create_client creates a client and prints its id.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"

	allisall "github.com/All-is-AI/allisall-go"
)

func main() {
	// Reads the API key from AIA_API_KEY.
	client, err := allisall.NewClient()
	if err != nil {
		log.Fatal(err)
	}

	if err := run(context.Background(), client); err != nil {
		// The API refused the request, for example a key without the write scope.
		var apiErr *allisall.APIError
		if errors.As(err, &apiErr) {
			log.Fatalf("the API refused the request: %d %s (request %s)", apiErr.Status, apiErr.Code, apiErr.RequestID)
		}
		log.Fatal(err)
	}
}

func run(ctx context.Context, client *allisall.Client) error {
	// The SDK sends one Idempotency-Key on every attempt, so a retry cannot
	// create a second client.
	created, err := client.Clients.Create(ctx, allisall.ClientCreate{
		Name:        "Acme Logistics",
		Status:      allisall.ClientStatusActive,
		ClientKey:   new("acme"),
		RenewalDate: new("2026-12-31"),
	})
	if err != nil {
		return err
	}
	fmt.Printf("created client %s (%s)\n", created.ID, created.Name)
	return nil
}
