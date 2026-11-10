// Command set_onboarding_skipped_steps replaces the guided setup's skipped
// steps.
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
	// The steps replace the whole set of skipped steps. An empty, non-nil
	// slice clears every skip.
	progress, err := client.Onboarding.SetSkippedSteps(ctx, allisall.OnboardingSkippedStepsSet{
		Steps: []allisall.OnboardingStep{allisall.OnboardingStepAddAgent},
	})
	if err != nil {
		return err
	}
	if progress.CurrentStep == nil {
		fmt.Println("resume at: every step is done")
	} else {
		fmt.Printf("resume at: %s\n", *progress.CurrentStep)
	}
	return nil
}
