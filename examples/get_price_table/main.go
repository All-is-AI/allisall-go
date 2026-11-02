// Command get_price_table prints the price table's rate for each model.
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
	table, err := client.Cost.GetPriceTable(ctx)
	if err != nil {
		return err
	}
	for _, rate := range table.Models {
		fmt.Printf("%s: %s in, %s out, per 1,000 tokens in %s\n", rate.Model, rate.InputPer1K, rate.OutputPer1K, table.Currency)
	}
	return nil
}
