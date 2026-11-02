// Command create_value_model creates a client's value model from a template.
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
	// The SDK sends one Idempotency-Key on every attempt, so a retry cannot
	// create a second value model.
	model, err := client.ValueModels.CreateForClient(ctx, clientID, allisall.ValueModelCreate{
		Name:       "Acme support value",
		TemplateID: new("support-automation"),
	})
	if err != nil {
		return err
	}
	fmt.Printf("created value model %s with %d line items\n", model.ID, len(model.LineItems))
	return nil
}
