// Command update_client marks a client as churned.
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
	// A member left out keeps its value; allisall.Null clears it.
	updated, err := client.Clients.Update(ctx, clientID, allisall.ClientPatch{
		Status: new(allisall.ClientStatusChurned),
	})
	if err != nil {
		return err
	}
	fmt.Printf("%s is now %s\n", updated.Name, updated.Status)
	return nil
}
