// Command create_report composes a report on a published calculation.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"

	allisall "github.com/All-is-AI/allisall-go"
)

// Replace CALCULATION_ID with the id of one of your calculations.
const calculationID = "CALCULATION_ID"

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
	// create a second report.
	report, err := client.Reports.CreateForCalculation(ctx, calculationID)
	if err != nil {
		return err
	}
	fmt.Printf("created report %s for %s\n", report.ID, report.ClientName)
	return nil
}
