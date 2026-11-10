// Command get_import reads an import and prints the mapping it ran with,
// ready to send as the next import's mapping.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"

	allisall "github.com/All-is-AI/allisall-go"
)

// Replace IMPORT_ID with the id of one of your imports.
const importID = "IMPORT_ID"

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
	item, err := client.Imports.Get(ctx, importID)
	if err != nil {
		return err
	}
	fmt.Printf("%s: %d rows\n", item.Filename, item.TotalRows)
	// The mapping it ran with, ready to send as the next import's mapping.
	mapping, err := json.Marshal(item.Mapping.ToImportMapping())
	if err != nil {
		return err
	}
	fmt.Println(string(mapping))
	return nil
}
