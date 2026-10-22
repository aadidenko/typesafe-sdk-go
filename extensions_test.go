package typesafe_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	typesafe "github.com/aadidenko/typesafe-sdk-go"
)

func TestFutureQuestionAndUnknownAnswersRoundTrip(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Questions map[string]json.RawMessage }

		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)

			return
		}

		var question struct {
			Type    string
			Options []any
			Nested  map[string]any
		}

		if err := json.Unmarshal(body.Questions["future"], &question); err != nil {
			t.Error(err)

			return
		}

		if question.Type != "future-ranking" || len(question.Options) != 2 || question.Nested["null"] != nil {
			t.Errorf("future question changed: %+v", question)
		}

		fixtureWriteString(t, w, `{"model":"actual","answers":{"future":{"type":"future-ranking","ranking":[2,1]},"another":{"type":"new-additional-kind","data":true}},"usage":{"input_tokens":0,"output_tokens":0},"future_top":42}`)
	}))
	defer server.Close()

	client := mustClient(t, typesafe.Config{APIKey: "key", BaseURL: server.URL})

	response, err := client.SystemOne(context.Background(), typesafe.SystemOneRequest{State: "sample", Questions: typesafe.Questions{"future": typesafe.RawQuestion(`{"type":"future-ranking","options":[{"arbitrary":true},null],"nested":{"null":null}}`)}})
	if err != nil {
		t.Fatal(err)
	}

	assertFutureAnswers(t, response)

	var full struct {
		FutureTop int `json:"future_top"`
	}

	if err := response.DecodeResponse(&full); err != nil || full.FutureTop != 42 {
		t.Fatal("future response field lost")
	}
}

func TestRawQuestionRejectsInvalidEnvelopeAndKnownKindsBeforeHTTP(t *testing.T) {
	var requests atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)

		fixtureWriteString(t, w, validResponse)
	}))

	defer server.Close()

	client := mustClient(t, typesafe.Config{APIKey: "key", BaseURL: server.URL})

	for _, raw := range []string{``, `{`, `null`, `[]`, `"text"`, `{}`, `{"type":null}`, `{"type":12}`, `{"type":" "}`, `{"type":"noul"}`, `{"type":"choice"}`, `{"type":"score"}`, `{"type":"box"}`} {
		t.Run(raw, func(t *testing.T) {
			_, err := client.SystemOne(context.Background(), typesafe.SystemOneRequest{State: "sample", Questions: typesafe.Questions{"future": typesafe.RawQuestion(raw)}})
			if !errors.Is(err, typesafe.ErrValidation) {
				t.Fatalf("expected local envelope error: %v", err)
			}
		})
	}

	if requests.Load() != 0 {
		t.Fatalf("invalid raw questions reached HTTP: %d", requests.Load())
	}
}

func TestFutureQuestionStillRequiresMatchingNamedAnswer(t *testing.T) {
	for _, answers := range []string{`{"another":{"type":"future-ranking"}}`, `{"future":{"type":"different"}}`, `{"future":{"type":"noul","noul":0.5}}`} {
		t.Run(answers, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				fixtureWriteString(t, w, `{"model":"actual","answers":`+answers+`,"usage":{"input_tokens":0,"output_tokens":0}}`)
			}))
			defer server.Close()

			client := mustClient(t, typesafe.Config{APIKey: "key", BaseURL: server.URL})
			_, err := client.SystemOne(context.Background(), typesafe.SystemOneRequest{State: "sample", Questions: typesafe.Questions{"future": typesafe.RawQuestion(`{"type":"future-ranking"}`)}})

			if !errors.Is(err, typesafe.ErrProtocol) {
				t.Fatalf("expected requested answer protocol error: %v", err)
			}
		})
	}
}

func TestExtraBodyAddsFutureFieldsWithoutReplacingCoreRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]json.RawMessage

		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)

			return
		}

		if string(body["priority"]) != `{"labels":["fast",null]}` || string(body["model"]) != `"chosen"` {
			t.Errorf("extension request changed: %s", body)
		}

		fixtureWriteString(t, w, validResponse)
	}))
	defer server.Close()

	client := mustClient(t, typesafe.Config{APIKey: "key", BaseURL: server.URL})

	_, err := client.SystemOne(context.Background(), typesafe.SystemOneRequest{State: "sample", Questions: typesafe.Questions{"billing": typesafe.NoulQuestion{Instructions: "billing?"}}, ExtraBody: map[string]any{"priority": map[string]any{"labels": []any{"fast", nil}}}}, typesafe.WithModel("chosen"))
	if err != nil {
		t.Fatal(err)
	}
}

func TestExtraBodyRejectsCoreCollisionsAndInvalidValuesBeforeHTTP(t *testing.T) {
	var requests atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)

		fixtureWriteString(t, w, validResponse)
	}))

	defer server.Close()

	client := mustClient(t, typesafe.Config{APIKey: "key", BaseURL: server.URL})

	for _, name := range []string{"state", "model", "questions", "assets"} {
		t.Run(name, func(t *testing.T) {
			_, err := client.SystemOne(context.Background(), typesafe.SystemOneRequest{State: "sample", Questions: typesafe.Questions{"billing": typesafe.NoulQuestion{Instructions: "billing?"}}, ExtraBody: map[string]any{name: nil}})
			if !errors.Is(err, typesafe.ErrValidation) {
				t.Fatalf("expected protected field error: %v", err)
			}
		})
	}

	_, err := client.SystemOne(context.Background(), typesafe.SystemOneRequest{State: "sample", Questions: typesafe.Questions{"billing": typesafe.NoulQuestion{Instructions: "billing?"}}, ExtraBody: map[string]any{"future": make(chan int)}})
	if !errors.Is(err, typesafe.ErrValidation) {
		t.Fatalf("expected unencodable extension error: %v", err)
	}

	if requests.Load() != 0 {
		t.Fatalf("invalid extensions reached HTTP: %d", requests.Load())
	}
}

func assertFutureAnswers(t *testing.T, response *typesafe.SystemOneResponse) {
	t.Helper()

	unknown, ok := response.Answers["future"].(typesafe.UnknownAnswer)
	if !ok || unknown.Type != "future-ranking" || string(unknown.Raw) != `{"type":"future-ranking","ranking":[2,1]}` {
		t.Fatalf("future answer lost: %+v", response.Answers)
	}

	if additional, ok := response.Answers["another"].(typesafe.UnknownAnswer); !ok || additional.Type != "new-additional-kind" {
		t.Fatal("additional future answer lost")
	}
}
