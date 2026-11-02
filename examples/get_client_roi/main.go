// Command get_client_roi prints a client's return on investment for August 2026.
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
	// The client's return on investment for August 2026.
	result, err := client.Analytics.GetRoiForClient(ctx, clientID, allisall.GetClientRoiParams{
		From: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		To:   time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		return err
	}
	if roi := result.Ratios.Roi; roi != nil {
		fmt.Printf("ROI: %g\n", *roi)
	} else if reason := result.Ratios.UndefinedReason; reason != nil {
		fmt.Printf("ROI undefined: %s\n", *reason)
	}
	value := "none"
	if v := result.Value.Total; v != nil {
		value = v.String()
	}
	fmt.Printf("value %s, cost %s %s\n", value, result.Cost.Total, result.Currency)
	return nil
}
