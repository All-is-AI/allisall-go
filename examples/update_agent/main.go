// Command update_agent retires an agent.
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
	// A member left out keeps its value; allisall.Null clears it.
	agent, err := client.Agents.Update(ctx, agentID, allisall.AgentPatch{
		Status: new(allisall.AgentStatusRetired),
	})
	if err != nil {
		return err
	}
	fmt.Printf("%s is now %s\n", agent.AgentKey, agent.Status)
	return nil
}
