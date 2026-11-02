// Command create_agent creates an agent and prints its id.
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
	// create a second agent.
	agent, err := client.Agents.Create(ctx, allisall.AgentCreate{
		AgentKey:  "support-bot",
		Name:      "Support bot",
		Framework: new("LangGraph"),
		Version:   new("1.4.0"),
		Status:    allisall.AgentStatusActive,
	})
	if err != nil {
		return err
	}
	fmt.Printf("created agent %s (%s)\n", agent.ID, agent.AgentKey)
	return nil
}
