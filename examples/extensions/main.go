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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-typesafe-request-id", "local-extension-example")
		io.WriteString(w, `{"model":"fixture","answers":{"ranking":{"type":"future-ranking","order":["a","b"]}},"usage":{"input_tokens":0,"output_tokens":0},"revision":2}`)
	}))
	defer server.Close()
	client, err := typesafe.NewClient(typesafe.Config{APIKey: "fixture-key", BaseURL: server.URL})
	if err != nil {
		log.Fatal(err)
	}
	response, err := client.SystemOne(context.Background(), typesafe.SystemOneRequest{
		State:     "Compare the alternatives.",
		Questions: typesafe.Questions{"ranking": typesafe.RawQuestion(`{"type":"future-ranking","alternatives":["a","b"]}`)},
		ExtraBody: map[string]any{"future_settings": map[string]any{"mode": "example"}},
	})
	if err != nil {
		log.Fatal(err)
	}
	unknown := response.Answers["ranking"].(typesafe.UnknownAnswer)
	fmt.Printf("kind: %s, raw answer: %s\n", unknown.Type, unknown.Raw)
	var app struct{ Ranking struct{ Order []string } }
	if err := response.DecodeAnswers(&app); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("first: %s, request ID: %s\n", app.Ranking.Order[0], response.RequestID)
}
