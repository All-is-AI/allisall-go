// Command create_cost_line_item creates a monthly cost line item for a client.
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
	// create a second line item.
	item, err := client.Cost.CreateLineItem(ctx, allisall.CostLineItemCreate{
		Label:         "Hosting",
		Target:        allisall.CostTargetInput{Kind: allisall.CostTargetKindClient, ID: clientID},
		MonthlyAmount: allisall.MustParseDecimal("30000"),
		StartDate:     "2026-07-01",
	})
	if err != nil {
		return err
	}
	fmt.Printf("created cost line item %s: %s %s a month\n", item.ID, item.MonthlyAmount, item.Currency)
	return nil
}
