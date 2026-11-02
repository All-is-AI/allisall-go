// Command update_value_model archives a client's value model.
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
	// A member left out keeps its value.
	model, err := client.ValueModels.UpdateForClient(ctx, clientID, allisall.ValueModelPatch{
		Status: new(allisall.ValueModelStatusArchived),
	})
	if err != nil {
		return err
	}
	fmt.Printf("%s is now %s\n", model.Name, model.Status)
	return nil
}
