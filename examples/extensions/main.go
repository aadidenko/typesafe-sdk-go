// Command extensions illustrates a hypothetical future Question against a local
// HTTP fixture. It needs no credentials and makes no provider requests. A real
// future discriminator must be supported by the provider before using it there.
package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"

	typesafe "github.com/aadidenko/typesafe-sdk-go"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("x-typesafe-request-id", "local-extension-example")

		if _, err := io.WriteString(w, `{"model":"fixture","answers":{"ranking":{"type":"future-ranking","order":["a","b"]}},"usage":{"input_tokens":0,"output_tokens":0},"revision":2}`); err != nil {
			log.Printf("write fixture response: %v", err)
		}
	}))
	defer server.Close()

	client, err := typesafe.NewClient(typesafe.Config{APIKey: "fixture-key", BaseURL: server.URL})
	if err != nil {
		return err
	}

	response, err := client.SystemOne(context.Background(), typesafe.SystemOneRequest{
		State:     "Compare the alternatives.",
		Questions: typesafe.Questions{"ranking": typesafe.RawQuestion(`{"type":"future-ranking","alternatives":["a","b"]}`)},
		ExtraBody: map[string]any{"future_settings": map[string]any{"mode": "example"}},
	})
	if err != nil {
		return err
	}

	unknown, ok := response.Answers["ranking"].(typesafe.UnknownAnswer)
	if !ok {
		return fmt.Errorf("ranking answer has an unexpected kind")
	}

	if _, err := fmt.Printf("kind: %s, raw answer: %s\n", unknown.Type, unknown.Raw); err != nil {
		return err
	}

	var app struct{ Ranking struct{ Order []string } }

	if err := response.DecodeAnswers(&app); err != nil {
		return err
	}

	if len(app.Ranking.Order) == 0 {
		return fmt.Errorf("ranking answer has no alternatives")
	}

	if _, err := fmt.Printf("first: %s, request ID: %s\n", app.Ranking.Order[0], response.RequestID); err != nil {
		return err
	}

	return nil
}
