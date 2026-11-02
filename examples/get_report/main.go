// Command get_report reads a report and prints its disclosures.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	allisall "github.com/All-is-AI/allisall-go"
)

// Replace REPORT_ID with the id of one of your reports.
const reportID = "REPORT_ID"

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
	report, err := client.Reports.Get(ctx, reportID)
	if err != nil {
		return err
	}
	shared := "never"
	if report.SharedAt != nil {
		shared = report.SharedAt.Format(time.RFC3339)
	}
	fmt.Printf("report for %s: shared %s\n", report.ClientName, shared)
	for _, disclosure := range report.Disclosures {
		state := "left out"
		if disclosure.Included {
			state = "included"
		}
		fmt.Printf("%s: %s\n", disclosure.Kind, state)
	}
	return nil
}
