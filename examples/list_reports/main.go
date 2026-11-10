// Command list_reports prints every report in the workspace, newest first.
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
	// Every report in the workspace, newest first, page by page.
	page, err := client.Reports.List(ctx, nil)
	if err != nil {
		return err
	}
	for report, err := range page.All(ctx) {
		if err != nil {
			return err
		}
		fmt.Printf("%s for %s, %v to %v: %s\n", report.ID, report.ClientName, report.Period.From, report.Period.To, report.State)
	}
	return nil
}
