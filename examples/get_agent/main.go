// Command get_agent reads an agent and prints its statistics.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"

	allisall "github.com/All-is-AI/allisall-go"
)

// Replace AGENT_ID with the id of one of your agents.
const agentID = "AGENT_ID"

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
	// A nil query reads the statistics over all time.
	agent, err := client.Agents.Get(ctx, agentID, nil)
	if err != nil {
		return err
	}
	fmt.Printf("%s %s (%s)\n", agent.AgentKey, agent.Name, agent.Status)
	fmt.Printf("%d events, costing %s %s\n", agent.Stats.Events, agent.Stats.CostTotal, agent.Stats.Currency)
	return nil
}
