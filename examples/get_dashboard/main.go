// Command get_dashboard prints the workspace's published figures for August 2026.
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
	// The portfolio for August 2026.
	dashboard, err := client.Dashboard.Get(ctx, allisall.GetDashboardParams{Month: "2026-08"})
	if err != nil {
		return err
	}
	roi := "undefined"
	if r := dashboard.Ratios.Roi; r != nil {
		roi = fmt.Sprint(*r)
	}
	fmt.Printf("value %s, cost %s %s\n", dashboard.ValueTotal, dashboard.CostTotal, dashboard.Currency)
	fmt.Printf("ROI %s\n", roi)
	for _, item := range dashboard.Clients {
		clientROI := "undefined"
		if r := item.Roi; r != nil {
			clientROI = fmt.Sprint(*r)
		}
		fmt.Printf("%s: ROI %s\n", item.Name, clientROI)
	}
	// The list holds at most 1,000 clients; the omitted count is the rest.
	fmt.Printf("clients not listed: %d\n", dashboard.ClientsOmitted)
	// The five counted clients with the highest value, whatever the sort.
	for _, item := range dashboard.TopClients {
		if v := item.ValueTotal; v != nil {
			fmt.Printf("top: %s, value %s\n", item.Name, v)
		}
	}
	return nil
}
