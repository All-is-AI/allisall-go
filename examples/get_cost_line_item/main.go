// Command get_cost_line_item reads a cost line item.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"

	allisall "github.com/All-is-AI/allisall-go"
)

// Replace COST_LINE_ITEM_ID with the id of one of your cost line items.
const costLineItemID = "COST_LINE_ITEM_ID"

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
	item, err := client.Cost.GetLineItem(ctx, costLineItemID)
	if err != nil {
		return err
	}
	end := "no end date"
	if item.EndDate != nil {
		end = *item.EndDate
	}
	fmt.Printf("%s: %s %s a month\n", item.Label, item.MonthlyAmount, item.Currency)
	fmt.Printf("from %s to %s\n", item.StartDate, end)
	return nil
}
