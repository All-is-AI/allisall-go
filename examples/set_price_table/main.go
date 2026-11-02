// Command set_price_table replaces the price table.
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
		// The API refused the request, for example a key without the write scope.
		var apiErr *allisall.APIError
		if errors.As(err, &apiErr) {
			log.Fatalf("the API refused the request: %d %s (request %s)", apiErr.Status, apiErr.Code, apiErr.RequestID)
		}
		log.Fatal(err)
	}
}

func run(ctx context.Context, client *allisall.Client) error {
	// The models replace the whole price table.
	table, err := client.Cost.SetPriceTable(ctx, allisall.PriceTableSet{
		Models: []allisall.ModelRateInput{
			{
				Model:       "claude-sonnet",
				InputPer1K:  allisall.MustParseDecimal("0.45"),
				OutputPer1K: allisall.MustParseDecimal("2.25"),
			},
			{
				Model:       "gpt-4o",
				InputPer1K:  allisall.MustParseDecimal("0.375"),
				OutputPer1K: allisall.MustParseDecimal("1.5"),
			},
		},
	})
	if err != nil {
		return err
	}
	fmt.Printf("the price table holds %d models\n", len(table.Models))
	return nil
}
