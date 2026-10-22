package typesafe_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	typesafe "github.com/aadidenko/typesafe-sdk-go"
)

func TestCustomDecodingPreservesOriginalAnswersAndMetadata(t *testing.T) {
	const body = `{"model":"actual","answers":{"billing":{"type":"noul","noul":0.7,"explanation":"billing request"},"future":{"type":"future","payload":[1,null]}},"usage":{"input_tokens":10,"output_tokens":2},"extra":{"revision":3}}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-typesafe-request-id", "decode-request")
		io.WriteString(w, body)
	}))
	defer server.Close()
	client, err := typesafe.NewClient(typesafe.Config{APIKey: "key", BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.SystemOne(context.Background(), typesafe.SystemOneRequest{State: "sample", Questions: typesafe.Questions{"billing": typesafe.NoulQuestion{Instructions: "billing?"}}})
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		var answers struct {
			Billing struct {
				Value       float64 `json:"noul"`
				Explanation string  `json:"explanation"`
			}
			Future struct {
				Type    string
				Payload []any
			}
		}
		if err := response.DecodeAnswers(&answers); err != nil {
			t.Fatal(err)
		}
		if answers.Billing.Value != 0.7 || answers.Billing.Explanation != "billing request" || answers.Future.Type != "future" || len(answers.Future.Payload) != 2 {
			t.Fatalf("lost answer data: %+v", answers)
		}
		var full struct {
			Extra struct{ Revision int }
			Model string
		}
		if err := response.DecodeResponse(&full); err != nil {
			t.Fatal(err)
		}
		if full.Extra.Revision != 3 || full.Model != "actual" {
			t.Fatalf("lost full response data: %+v", full)
		}
	}
	if _, err := response.Noul("billing"); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 200 || response.RequestID != "decode-request" || string(response.RawBody) != body {
		t.Fatal("custom decoding changed HTTP metadata")
	}
}

func TestCustomDecodingUsesOrdinaryGoJSONErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"model":"actual","answers":{"billing":{"type":"noul","noul":0.7,"count":"12"}},"usage":{"input_tokens":0,"output_tokens":0}}`)
	}))
	defer server.Close()
	client, _ := typesafe.NewClient(typesafe.Config{APIKey: "key", BaseURL: server.URL})
	response, err := client.SystemOne(context.Background(), typesafe.SystemOneRequest{State: "sample", Questions: typesafe.Questions{"billing": typesafe.NoulQuestion{Instructions: "billing?"}}})
	if err != nil {
		t.Fatal(err)
	}
	var app struct{ Billing struct{ Count int } }
	err = response.DecodeAnswers(&app)
	var mismatch *json.UnmarshalTypeError
	if !errors.As(err, &mismatch) || !strings.Contains(err.Error(), "decode answers") {
		t.Fatalf("expected typed JSON mismatch: %v", err)
	}
	for _, destination := range []any{nil, struct{}{}, (*int)(nil)} {
		err := response.DecodeResponse(destination)
		var invalid *json.InvalidUnmarshalError
		if !errors.As(err, &invalid) {
			t.Fatalf("expected invalid destination error: %v", err)
		}
	}
	var noLifting struct{ Billing any }
	if err := response.DecodeResponse(&noLifting); err != nil || noLifting.Billing != nil {
		t.Fatal("full-response decoder unexpectedly lifted answer fields")
	}
}
