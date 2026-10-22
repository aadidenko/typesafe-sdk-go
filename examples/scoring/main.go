// Scoring rates a message against ordered urgency levels. It makes one live evaluation.
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	typesafe "github.com/aadidenko/typesafe-sdk-go"
)

func main() {
	client, err := typesafe.NewClient(typesafe.Config{APIKey: os.Getenv("TYPESAFE_API_KEY")})
	if err != nil {
		log.Fatal(err)
	}
	response, err := client.SystemOne(context.Background(), typesafe.SystemOneRequest{
		State: "My subscription was charged twice and I need the refund today.",
		Questions: typesafe.Questions{"urgency": typesafe.ScoreQuestion{
			Instructions: "How urgent is this message?",
			Criteria:     []any{"Can wait", "Needs attention this week", "Needs attention today"},
		}},
	})
	if err != nil {
		log.Fatal(err)
	}
	urgency, err := response.Score("urgency")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Expected urgency: %.2f, confidence %.2f, levels %v\n", urgency.Score, urgency.Confidence, urgency.Legend)
}
