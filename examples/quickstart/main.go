// Quickstart evaluates a billing question. Run with TYPESAFE_API_KEY set.
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	typesafe "github.com/aadidenko/typesafe-sdk-go"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	client, err := typesafe.NewClient(typesafe.Config{APIKey: os.Getenv("TYPESAFE_API_KEY")})
	if err != nil {
		return err
	}

	response, err := client.SystemOne(context.Background(), typesafe.SystemOneRequest{
		State:     "I was charged twice for my subscription. Please refund the extra charge.",
		Questions: typesafe.Questions{"billing": typesafe.NoulQuestion{Instructions: "Is this message about billing?"}},
	})
	if err != nil {
		return err
	}

	billing, err := response.Noul("billing")
	if err != nil {
		return err
	}

	if _, err := fmt.Printf("Billing probability: %.2f (model %s, input tokens %d)\n", billing.Probability, response.Model, response.Usage.InputTokens); err != nil {
		return err
	}

	return nil
}
