// Command create_calculation freezes a client's ROI for August 2026 as a draft calculation.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	allisall "github.com/All-is-AI/allisall-go"
)

// Replace CLIENT_ID with the id of one of your clients.
const clientID = "CLIENT_ID"

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
	// create a second calculation.
	calculation, err := client.Calculations.CreateForClient(ctx, clientID, allisall.CalculationCreate{
		Period: allisall.PeriodInput{
			From: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
			To:   time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		},
	})
	if err != nil {
		return err
	}
	value := "none"
	if v := calculation.ValueTotal; v != nil {
		value = v.String()
	}
	fmt.Printf("calculation %s (%s)\n", calculation.ID, calculation.State)
	fmt.Printf("value %s, cost %s %s\n", value, calculation.CostTotal, calculation.Currency)
	return nil
}
