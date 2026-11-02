// Command preview_import previews a CSV import and prints what it would store, without storing anything.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	allisall "github.com/All-is-AI/allisall-go"
)

func main() {
	// Reads the API key from AIA_API_KEY.
	client, err := allisall.NewClient()
	if err != nil {
		log.Fatal(err)
	}

	if err := run(context.Background(), client); err != nil {
		// The API refused the request, for example a key without the ingest scope.
		var apiErr *allisall.APIError
		if errors.As(err, &apiErr) {
			log.Fatalf("the API refused the request: %d %s (request %s)", apiErr.Status, apiErr.Code, apiErr.RequestID)
		}
		log.Fatal(err)
	}
}

func run(ctx context.Context, client *allisall.Client) error {
	upload := allisall.ImportUpload{
		// The mapping names the CSV column that holds each event member.
		Mapping: allisall.ImportMapping{
			Columns: &map[string]string{
				"timestamp":  "Time",
				"agent_key":  "Agent",
				"client_key": "Client",
				"category":   "Category",
				"event_type": "Type",
				"cost":       "Cost",
				"currency":   "Currency",
			},
			Timezone:  new("Asia/Tokyo"),
			Delimiter: new(allisall.ImportDelimiterComma),
		},
		File: strings.NewReader("Time,Agent,Client,Category,Type,Cost,Currency\n" +
			"2026-08-14 12:12:40,support-bot,acme,model,completion,0.3683,JPY\n"),
	}

	// Nothing is stored: the preview reports what an import would do.
	preview, err := client.Imports.Preview(ctx, upload)
	if err != nil {
		return err
	}
	fmt.Printf("%d of %d rows valid, %d rejected\n", preview.Valid, preview.TotalRows, preview.Rejected)
	for _, row := range preview.Errors {
		fmt.Printf("row %d: %s: %s\n", row.Row, row.Code, row.Detail)
	}
	return nil
}
