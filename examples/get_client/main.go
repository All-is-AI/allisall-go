// Command get_client reads a client and prints its assigned agents.
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
	detail, err := client.Clients.Get(ctx, clientID)
	if err != nil {
		return err
	}
	fmt.Printf("%s (%s)\n", detail.Name, detail.Status)
	for _, agent := range detail.Agents {
		fmt.Printf("assigned agent %s: %s\n", agent.AgentKey, agent.Name)
	}
	return nil
}
