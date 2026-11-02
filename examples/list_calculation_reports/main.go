// Command list_calculation_reports prints every report of a calculation.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

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
	// Every report of the calculation, page by page.
	page, err := client.Reports.ListForCalculation(ctx, calculationID, nil)
	if err != nil {
		return err
	}
	for report, err := range page.All(ctx) {
		if err != nil {
			return err
		}
		shared := "never"
		if report.SharedAt != nil {
			shared = report.SharedAt.Format(time.RFC3339)
		}
		fmt.Printf("%s for %s: shared %s\n", report.ID, report.ClientName, shared)
	}
	return nil
}
