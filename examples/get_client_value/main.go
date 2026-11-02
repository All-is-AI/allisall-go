// Command get_client_value prints the value a client's outcomes produced in August 2026.
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
	// The value the client's outcomes produced in August 2026.
	result, err := client.Analytics.GetValueForClient(ctx, clientID, &allisall.GetClientValueParams{
		From: new(time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)),
		To:   new(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)),
	})
	if err != nil {
		return err
	}
	value := "none"
	if v := result.Total; v != nil {
		value = v.String()
	}
	fmt.Printf("value: %s %s\n", value, result.Currency)
	for _, line := range result.Lines {
		fmt.Printf("%s: %g at %s, %s %s\n", line.Label, line.Quantity, line.UnitValue, line.Total, result.Currency)
	}
	return nil
}
