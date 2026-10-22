// Retries demonstrates bounded retry configuration. It makes a live evaluation.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	typesafe "github.com/aadidenko/typesafe-sdk-go"
)

func main() {
	policy := typesafe.DefaultRetryPolicy()

	policy.MaxRetries = 1

	client, err := typesafe.NewClient(typesafe.Config{APIKey: os.Getenv("TYPESAFE_API_KEY"), Retry: &policy, AttemptTimeout: 3 * time.Second, CallTimeout: 8 * time.Second})
	if err != nil {
		log.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)

	defer cancel()

	response, err := client.SystemOne(ctx, typesafe.SystemOneRequest{
		State: "Please refund my duplicate charge.", Questions: typesafe.Questions{"billing": typesafe.NoulQuestion{Instructions: "Is this about billing?"}},
	})
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			log.Fatal("evaluation exceeded its deadline")
		}

		log.Fatal(err)
	}

	fmt.Printf("Model %s, request ID %s\n", response.Model, response.RequestID)

	// For a single attempt on a future call, pass:
	// typesafe.WithRetryPolicy(typesafe.RetryPolicy{})
}
