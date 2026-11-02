// Command get_client_value_model reads a client's value model and prints its line items.
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
	model, err := client.ValueModels.GetForClient(ctx, clientID)
	if err != nil {
		return err
	}
	fmt.Printf("%s, version %d (%s)\n", model.Name, model.Version, model.Status)
	for _, item := range model.LineItems {
		fmt.Printf("%s: %s %s each, %s\n", item.OutcomeType, item.UnitValue, model.Currency, item.Label)
	}
	return nil
}
