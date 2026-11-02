// Command list_cost_line_items prints every cost line item in the workspace.
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
	// Every cost line item in the workspace, page by page.
	page, err := client.Cost.ListLineItems(ctx, nil)
	if err != nil {
		return err
	}
	for item, err := range page.All(ctx) {
		if err != nil {
			return err
		}
		fmt.Printf("%s: %s %s a month from %s\n", item.Label, item.MonthlyAmount, item.Currency, item.StartDate)
	}
	return nil
}
