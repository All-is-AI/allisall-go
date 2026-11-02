// Command list_renewals prints every client with a renewal date, and its latest ROI.
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
	// Every client with a renewal date, page by page.
	page, err := client.Dashboard.ListRenewals(ctx, nil)
	if err != nil {
		return err
	}
	for renewal, err := range page.All(ctx) {
		if err != nil {
			return err
		}
		roi := "undefined"
		if r := renewal.Roi; r != nil {
			roi = fmt.Sprint(*r)
		}
		fmt.Printf("%s: renews %s, ROI %s\n", renewal.Name, renewal.RenewalDate, roi)
	}
	return nil
}
