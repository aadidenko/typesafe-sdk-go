// Classification selects the tone of a message. It makes one live evaluation.
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
		State: "I was charged twice for my subscription. Please help.",
		Questions: typesafe.Questions{"tone": typesafe.ChoiceQuestion{
			Instructions: "What is the tone of this message?",
			Criteria:     map[string]any{"angry": "An upset or hostile message", "calm": nil, "excited": "An enthusiastic message"},
		}},
	})
	if err != nil {
		return err
	}

	tone, err := response.Choice("tone")
	if err != nil {
		return err
	}

	if _, err := fmt.Printf("Tone: %s, confidence %.2f, probabilities %v\n", tone.Choice, tone.Confidence, tone.Probabilities); err != nil {
		return err
	}

	return nil
}
