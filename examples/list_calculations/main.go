// Command list_calculations prints every published calculation in the
// workspace.
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
	// Every published calculation in the workspace, page by page.
	page, err := client.Calculations.List(ctx, &allisall.ListCalculationsParams{State: new(allisall.CalculationStatePublished)})
	if err != nil {
		return err
	}
	for item, err := range page.All(ctx) {
		if err != nil {
			return err
		}
		fmt.Printf("%s %v to %v: %d coverage findings\n", item.ClientName, item.Period.From, item.Period.To, item.CoverageFindings)
	}
	return nil
}
