// Command list_outcome_types prints every outcome type the workspace's events carry.
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
		// The API refused the request, for example a key without the read scope.
		var apiErr *allisall.APIError
		if errors.As(err, &apiErr) {
			log.Fatalf("the API refused the request: %d %s (request %s)", apiErr.Status, apiErr.Code, apiErr.RequestID)
		}
		log.Fatal(err)
	}
}

func run(ctx context.Context, client *allisall.Client) error {
	// Every outcome type the workspace's events carry, page by page.
	page, err := client.OutcomeTypes.List(ctx, nil)
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
