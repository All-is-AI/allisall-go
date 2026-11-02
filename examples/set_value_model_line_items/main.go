// Command set_value_model_line_items replaces the line items of a client's value model.
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
	// The line items replace the model's whole set, and Version must be the
	// model's current version, so read it first.
	current, err := client.ValueModels.GetForClient(ctx, clientID)
	if err != nil {
		return err
	}
	model, err := client.ValueModels.SetLineItemsForClient(ctx, clientID, allisall.ValueLineItemsSet{
		Version: current.Version,
		LineItems: []allisall.ValueLineItemInput{
			{
				OutcomeType:    "ticket_resolved",
				Label:          "Support ticket resolved autonomously",
				UnitValue:      new(allisall.MustParseDecimal("1200")),
				ProvenanceNote: new("Agreed with Acme ops lead, 2026-07-20: fully loaded cost of a tier-1 ticket."),
			},
			{
				OutcomeType: "escalation_avoided",
				Label:       "Escalation to a human agent avoided",
				UnitValue:   new(allisall.MustParseDecimal("450.5")),
			},
		},
	})
	if err != nil {
		return err
	}
	fmt.Printf("version %d holds %d line items\n", model.Version, len(model.LineItems))
	return nil
}
