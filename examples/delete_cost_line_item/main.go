// Command delete_cost_line_item deletes a cost line item.
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
	if err := client.Cost.DeleteLineItem(ctx, costLineItemID); err != nil {
		return err
	}
	fmt.Printf("deleted cost line item %s\n", costLineItemID)
	return nil
}
