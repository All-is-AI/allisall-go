// Command list_agents prints every agent in the workspace.
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
	// Every agent in the workspace, page by page.
	page, err := client.Agents.List(ctx, nil)
	if err != nil {
		return err
	}
	for agent, err := range page.All(ctx) {
		if err != nil {
			return err
		}
		fmt.Printf("%s %s: assigned to %d clients\n", agent.AgentKey, agent.Name, agent.ClientCount)
	}
	return nil
}
