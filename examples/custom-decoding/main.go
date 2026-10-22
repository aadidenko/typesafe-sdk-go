// Command custom-decoding makes one evaluation when explicitly invoked with an
// externally supplied TYPESAFE_API_KEY. Normal tests only compile this example.
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

	response, err := client.SystemOne(context.Background(), typesafe.SystemOneRequest{State: "I was charged twice for my subscription.", Questions: typesafe.Questions{"billing": typesafe.NoulQuestion{Instructions: "Is this about billing?"}}})
	if err != nil {
		return err
	}

	var app struct {
		Billing struct {
			Probability float64 `json:"noul"`
		}
	}

	if err := response.DecodeAnswers(&app); err != nil {
		return err
	}

	// Application-specific validation and thresholds are explicit Go code.
	if _, err := fmt.Printf("billing probability: %.2f\n", app.Billing.Probability); err != nil {
		return err
	}

	var complete struct {
		Model string
		Usage typesafe.Usage
	}

	if err := response.DecodeResponse(&complete); err != nil {
		return err
	}

	if _, err := fmt.Printf("model: %s, input tokens: %d, request ID: %s\n", complete.Model, complete.Usage.InputTokens, response.RequestID); err != nil {
		return err
	}

	return nil
}
