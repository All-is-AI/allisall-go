// Command get_calculation reads a calculation and prints its figures.
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
	calculation, err := client.Calculations.Get(ctx, calculationID)
	if err != nil {
		return err
	}
	roi := "undefined"
	if r := calculation.Ratios.Roi; r != nil {
		roi = fmt.Sprint(*r)
	}
	value := "none"
	if v := calculation.ValueTotal; v != nil {
		value = v.String()
	}
	fmt.Printf("%s, ROI %s\n", calculation.State, roi)
	fmt.Printf("value %s, cost %s %s\n", value, calculation.CostTotal, calculation.Currency)
	return nil
}
