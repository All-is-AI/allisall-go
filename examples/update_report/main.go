// Command update_report edits a report's note and disclosures.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"

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
	// A member left out keeps its value; allisall.Null clears it. The
	// disclosures replace the stored ones whole.
	report, err := client.Reports.Update(ctx, reportID, allisall.ReportPatch{
		Note: allisall.NewNullable("August was the first full month on the new routing rules."),
		Disclosures: &[]allisall.DisclosureEdit{
			{Kind: allisall.DisclosureKindUnpriced, Included: true},
			{
				Kind:     allisall.DisclosureKindTokenValued,
				Included: true,
				Text:     new("Model costs for 220 events were valued from token counts at list prices."),
			},
		},
	})
	if err != nil {
		return err
	}
	fmt.Printf("report %s: %s\n", report.ID, report.Note)
	return nil
}
