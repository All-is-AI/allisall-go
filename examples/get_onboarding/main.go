// Command get_onboarding prints the guided setup's progress, step by step.
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
	progress, err := client.Onboarding.Get(ctx)
	if err != nil {
		return err
	}
	for _, step := range progress.Steps {
		state := "to do"
		switch {
		case step.Completed:
			state = "completed"
		case step.SkippedAt != nil:
			state = fmt.Sprintf("skipped at %s", step.SkippedAt)
		}
		fmt.Printf("%s: %s\n", step.Step, state)
	}
	if progress.CurrentStep == nil {
		fmt.Println("resume at: every step is done")
	} else {
		fmt.Printf("resume at: %s\n", *progress.CurrentStep)
	}
	return nil
}
