package typesafe_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	typesafe "github.com/aadidenko/typesafe-sdk-go"
)

const validResponse = `{"model":"jev-actual","answers":{"billing":{"type":"noul","noul":0.98}},"usage":{"input_tokens":120,"output_tokens":12}}`

func TestSystemOneNoul(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/gateway/v1/systemone" {
			t.Errorf("unexpected operation: %s %s", r.Method, r.URL.Path)
		}

		if r.Header.Get("Authorization") != "Bearer test-key" || r.Header.Get("User-Agent") == "" || r.Header.Get("X-TypeSafe-SDK") == "" || r.Header.Get("X-TypeSafe-Runtime") == "" || r.Header.Get("X-TypeSafe-Retry-Count") != "0" {
			t.Error("missing authentication or SDK metadata")
		}

		var body map[string]any

		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}

		if body["model"] != "custom-model" {
			t.Errorf("model = %v", body["model"])
		}

		questions := body["questions"].(map[string]any)
		if questions["billing"].(map[string]any)["type"] != "noul" {
			t.Error("missing Noul discriminator")
		}

		w.Header().Set("x-typesafe-request-id", "req-42")
		w.Write([]byte(validResponse))
	}))
	defer server.Close()

	client, err := typesafe.NewClient(typesafe.Config{APIKey: "test-key", BaseURL: server.URL + "/gateway"})
	if err != nil {
		t.Fatal(err)
	}

	response, err := client.SystemOne(context.Background(), typesafe.SystemOneRequest{
		State:     map[string]any{"message": "Duplicate charge", "nested": []any{nil}},
		Questions: typesafe.Questions{"billing": typesafe.NoulQuestion{Instructions: "Is this about billing?", Criteria: &typesafe.NoulCriteria{True: "A billing request", False: map[string]any{"other": nil}}}},
	}, typesafe.WithModel("custom-model"))
	if err != nil {
		t.Fatal(err)
	}

	answer, err := response.Noul("billing")
	if err != nil || answer.Probability != .98 {
		t.Fatalf("answer=%+v error=%v", answer, err)
	}

	if response.Model != "jev-actual" || response.Usage.InputTokens != 120 || response.Usage.OutputTokens != 12 {
		t.Fatalf("response=%+v", response)
	}

	if response.StatusCode != 200 || response.RequestID != "req-42" || string(response.RawBody) != validResponse {
		t.Fatalf("metadata=%+v", response.HTTPMetadata)
	}

	if len(response.Answers) != 1 {
		t.Fatal("answers not retained")
	}
}

func TestInvalidAPIKeyNeverAppearsInError(t *testing.T) {
	for _, key := range []string{"", "secret key", "secret\nkey", "secret-é", "secret\x7f"} {
		_, err := typesafe.NewClient(typesafe.Config{APIKey: key})

		var validation *typesafe.ValidationError

		if !errors.As(err, &validation) || !errors.Is(err, typesafe.ErrValidation) {
			t.Fatalf("expected validation error, got %v", err)
		}

		if key != "" && strings.Contains(err.Error(), key) {
			t.Fatal("error exposes API key")
		}
	}
}

func TestInvalidContentRejectedBeforeHTTP(t *testing.T) {
	var traffic atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { traffic.Add(1); w.Write([]byte(validResponse)) }))

	defer server.Close()

	client, _ := typesafe.NewClient(typesafe.Config{APIKey: "test", BaseURL: server.URL})
	tests := []struct {
		name    string
		request typesafe.SystemOneRequest
	}{
		{"null state", typesafe.SystemOneRequest{State: nil, Questions: typesafe.Questions{"billing": typesafe.NoulQuestion{}}}},
		{"scalar state", typesafe.SystemOneRequest{State: 42, Questions: typesafe.Questions{"billing": typesafe.NoulQuestion{}}}},
		{"nonfinite nested value", typesafe.SystemOneRequest{State: []any{math.Inf(1)}, Questions: typesafe.Questions{"billing": typesafe.NoulQuestion{}}}},
		{"unencodable state", typesafe.SystemOneRequest{State: map[string]any{"fn": func() {}}, Questions: typesafe.Questions{"billing": typesafe.NoulQuestion{}}}},
		{"scalar instructions", typesafe.SystemOneRequest{State: "state", Questions: typesafe.Questions{"billing": typesafe.NoulQuestion{Instructions: true}}}},
		{"scalar criterion", typesafe.SystemOneRequest{State: "state", Questions: typesafe.Questions{"billing": typesafe.NoulQuestion{Criteria: &typesafe.NoulCriteria{True: 1}}}}},
		{"empty questions", typesafe.SystemOneRequest{State: "state"}},
		{"nil question", typesafe.SystemOneRequest{State: "state", Questions: typesafe.Questions{"billing": nil}}},
		{"typed nil question", typesafe.SystemOneRequest{State: "state", Questions: typesafe.Questions{"billing": (*typesafe.NoulQuestion)(nil)}}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := client.SystemOne(context.Background(), test.request)
			if !errors.Is(err, typesafe.ErrValidation) {
				t.Fatalf("expected validation failure: %v", err)
			}
		})
	}

	if traffic.Load() != 0 {
		t.Fatal("invalid request reached server")
	}
}

func noulRequest() typesafe.SystemOneRequest {
	return typesafe.SystemOneRequest{State: "A duplicate subscription charge", Questions: typesafe.Questions{"billing": typesafe.NoulQuestion{Instructions: "Billing?"}}}
}

func TestSuccessfulResponseMustSatisfyContract(t *testing.T) {
	cases := []struct{ name, body, path string }{
		{"malformed", "{", "$"},
		{"null root", "null", "$"},
		{"model missing", `{"answers":{"billing":{"type":"noul","noul":0.5}},"usage":{"input_tokens":0,"output_tokens":0}}`, "model"},
		{"model wrong type", `{"model":1,"answers":{},"usage":{}}`, "model"},
		{"usage missing", `{"model":"actual","answers":{"billing":{"type":"noul","noul":0.5}}}`, "usage"},
		{"input tokens missing", `{"model":"actual","answers":{"billing":{"type":"noul","noul":0.5}},"usage":{"output_tokens":0}}`, "usage.input_tokens"},
		{"output tokens null", `{"model":"actual","answers":{"billing":{"type":"noul","noul":0.5}},"usage":{"input_tokens":0,"output_tokens":null}}`, "usage.output_tokens"},
		{"fractional tokens", `{"model":"actual","answers":{"billing":{"type":"noul","noul":0.5}},"usage":{"input_tokens":0.5,"output_tokens":1}}`, "usage.input_tokens"},
		{"negative tokens", `{"model":"actual","answers":{"billing":{"type":"noul","noul":0.5}},"usage":{"input_tokens":0,"output_tokens":-1}}`, "usage.output_tokens"},
		{"answers missing", `{"model":"actual","usage":{"input_tokens":0,"output_tokens":0}}`, "answers"},
		{"answer wrong shape", `{"model":"actual","answers":{"billing":[]},"usage":{"input_tokens":0,"output_tokens":0}}`, "answers.billing"},
		{"answer name missing", `{"model":"actual","answers":{"other":{"type":"noul","noul":0.5}},"usage":{"input_tokens":0,"output_tokens":0}}`, "answers.billing"},
		{"wrong discriminator", `{"model":"actual","answers":{"billing":{"type":"future","noul":0.5}},"usage":{"input_tokens":0,"output_tokens":0}}`, "answers.billing.type"},
		{"discriminator missing", `{"model":"actual","answers":{"billing":{"noul":0.5}},"usage":{"input_tokens":0,"output_tokens":0}}`, "answers.billing.type"},
		{"probability missing", `{"model":"actual","answers":{"billing":{"type":"noul"}},"usage":{"input_tokens":0,"output_tokens":0}}`, "answers.billing.noul"},
		{"probability null", `{"model":"actual","answers":{"billing":{"type":"noul","noul":null}},"usage":{"input_tokens":0,"output_tokens":0}}`, "answers.billing.noul"},
		{"probability string", `{"model":"actual","answers":{"billing":{"type":"noul","noul":"0.5"}},"usage":{"input_tokens":0,"output_tokens":0}}`, "answers.billing.noul"},
		{"probability too high", `{"model":"actual","answers":{"billing":{"type":"noul","noul":1.1}},"usage":{"input_tokens":0,"output_tokens":0}}`, "answers.billing.noul"},
		{"probability negative", `{"model":"actual","answers":{"billing":{"type":"noul","noul":-0.1}},"usage":{"input_tokens":0,"output_tokens":0}}`, "answers.billing.noul"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("x-typesafe-request-id", "invalid-req")
				w.Write([]byte(test.body))
			}))
			defer server.Close()

			client, _ := typesafe.NewClient(typesafe.Config{APIKey: "test", BaseURL: server.URL})
			_, err := client.SystemOne(context.Background(), noulRequest())

			var protocol *typesafe.ProtocolError

			if !errors.Is(err, typesafe.ErrProtocol) || !errors.As(err, &protocol) {
				t.Fatalf("expected protocol error: %v", err)
			}

			if protocol.Field != test.path || string(protocol.RawBody) != test.body || protocol.RequestID != "invalid-req" || protocol.StatusCode != 200 {
				t.Fatalf("protocol error=%+v", protocol)
			}
		})
	}
}

func TestAllAnswersAndAccessorErrors(t *testing.T) {
	body := `{"model":"actual","answers":{"billing":{"type":"noul","noul":0},"future":{"type":"future","extra":{"kept":true}}},"usage":{"input_tokens":0,"output_tokens":0},"new_field":42}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(body)) }))

	defer server.Close()

	client, _ := typesafe.NewClient(typesafe.Config{APIKey: "test", BaseURL: server.URL})

	response, err := client.SystemOne(context.Background(), noulRequest())
	if err != nil {
		t.Fatal(err)
	}

	if _, err = response.Noul("missing"); !errors.Is(err, typesafe.ErrAnswerMissing) {
		t.Fatalf("missing error=%v", err)
	}

	if _, err = response.Noul("future"); !errors.Is(err, typesafe.ErrAnswerKind) {
		t.Fatalf("kind error=%v", err)
	}

	unknown, ok := response.Answers["future"].(typesafe.UnknownAnswer)
	if !ok || unknown.Kind() != "future" || !strings.Contains(string(unknown.Raw), `"kept":true`) {
		t.Fatalf("unknown=%+v", unknown)
	}

	if string(response.RawBody) != body {
		t.Fatal("original response lost extensions")
	}
}

func TestHTTPFailuresRetainMetadataAndCategories(t *testing.T) {
	cases := []struct {
		status   int
		category error
		body     string
	}{
		{400, typesafe.ErrBadRequest, `{"detail":"invalid"}`},
		{401, typesafe.ErrAuthentication, ""},
		{403, typesafe.ErrPermission, "forbidden"},
		{404, typesafe.ErrNotFound, "missing"},
		{422, typesafe.ErrServerValidation, `{"detail":[]}`},
		{429, typesafe.ErrRateLimit, "try later"},
		{503, typesafe.ErrServer, "gateway failure"},
	}
	for _, test := range cases {
		t.Run(strconv.Itoa(test.status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("x-typesafe-request-id", "failed-req")
				w.Header().Set("X-Diagnostic", "retained")
				w.WriteHeader(test.status)
				w.Write([]byte(test.body))
			}))
			defer server.Close()

			client, _ := typesafe.NewClient(typesafe.Config{APIKey: "test", BaseURL: server.URL})
			_, err := client.SystemOne(context.Background(), noulRequest(), typesafe.WithRetryPolicy(typesafe.RetryPolicy{}))

			var failure *typesafe.HTTPError

			if !errors.As(err, &failure) || !errors.Is(err, typesafe.ErrHTTP) || !errors.Is(err, test.category) {
				t.Fatalf("error=%v", err)
			}

			if failure.StatusCode != test.status || string(failure.RawBody) != test.body || failure.RequestID != "failed-req" || failure.Headers.Get("X-Diagnostic") != "retained" {
				t.Fatalf("failure=%+v", failure)
			}
		})
	}
}

func TestCancellationInterruptsActiveHTTPAndBodyRead(t *testing.T) {
	for _, bodyStarted := range []bool{false, true} {
		t.Run(strconv.FormatBool(bodyStarted), func(t *testing.T) {
			started := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				io.Copy(io.Discard, r.Body)

				if bodyStarted {
					w.WriteHeader(200)
					w.Write([]byte(`{"model":`))
					w.(http.Flusher).Flush()
				}

				close(started)
				<-r.Context().Done()
			}))

			defer server.Close()

			client, _ := typesafe.NewClient(typesafe.Config{APIKey: "test", BaseURL: server.URL})
			ctx, cancel := context.WithCancel(context.Background())

			defer cancel()

			done := make(chan error, 1)

			go func() { _, err := client.SystemOne(ctx, noulRequest()); done <- err }()

			<-started
			cancel()

			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) || !errors.Is(err, typesafe.ErrTransport) {
					t.Fatalf("error=%v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("cancellation did not stop HTTP")
			}
		})
	}
}

func TestShortestDeadlineBoundsCall(t *testing.T) {
	tests := []struct {
		name                  string
		attempt, call, caller time.Duration
	}{
		{"attempt", 25 * time.Millisecond, time.Second, time.Second},
		{"call", time.Second, 25 * time.Millisecond, time.Second},
		{"caller", time.Second, time.Second, 25 * time.Millisecond},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(200)
				w.(http.Flusher).Flush()
				<-r.Context().Done()
			}))
			defer server.Close()

			client, _ := typesafe.NewClient(typesafe.Config{APIKey: "test", BaseURL: server.URL})
			ctx, cancel := context.WithTimeout(context.Background(), test.caller)

			defer cancel()

			start := time.Now()
			_, err := client.SystemOne(ctx, noulRequest(), typesafe.WithTimeouts(test.attempt, test.call), typesafe.WithRetryPolicy(typesafe.RetryPolicy{}))

			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("deadline identity lost: %v", err)
			}

			if time.Since(start) > time.Second {
				t.Fatal("shortest deadline was not enforced")
			}
		})
	}
}

func TestConnectionFailure(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	address := listener.Addr().String()
	listener.Close()

	client, _ := typesafe.NewClient(typesafe.Config{APIKey: "test", BaseURL: "http://" + address})

	_, err = client.SystemOne(context.Background(), noulRequest())

	var transport *typesafe.TransportError

	if !errors.Is(err, typesafe.ErrTransport) || !errors.As(err, &transport) {
		t.Fatalf("error=%v", err)
	}
}

func TestConcurrentPerCallSettingsAreIsolated(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model string `json:"model"`
		}

		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}

		if body.Model != r.Header.Get("X-Call") {
			t.Errorf("settings leaked: %s %s", body.Model, r.Header.Get("X-Call"))
		}

		w.Write([]byte(validResponse))
	}))
	defer server.Close()

	headers := http.Header{"X-Original": []string{"original"}}
	client, _ := typesafe.NewClient(typesafe.Config{APIKey: "test", BaseURL: server.URL, Headers: headers})
	headers.Set("X-Original", "changed")

	request := noulRequest()

	var group sync.WaitGroup

	for i := range 20 {
		group.Add(1)

		go func() {
			defer group.Done()

			model := strconv.Itoa(i)

			_, err := client.SystemOne(context.Background(), request, typesafe.WithModel(model), typesafe.WithHeaders(http.Header{"X-Call": []string{model}}))
			if err != nil {
				t.Error(err)
			}
		}()
	}

	group.Wait()

	if request.Questions["billing"].(typesafe.NoulQuestion).Instructions != "Billing?" {
		t.Fatal("request mutated")
	}
}

type changingJSON struct{ calls int }

func (v *changingJSON) MarshalJSON() ([]byte, error) {
	v.calls++
	if v.calls == 1 {
		return []byte(`"original"`), nil
	}

	return []byte(`false`), nil
}

func TestContentIsValidatedAndSerializedAsOneSnapshot(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			State     string `json:"state"`
			Questions map[string]struct {
				Instructions string            `json:"instructions"`
				Criteria     map[string]string `json:"criteria"`
			} `json:"questions"`
		}

		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("wire content differs from validated content: %v", err)
		}

		if body.State != "original" || body.Questions["billing"].Instructions != "original" || body.Questions["billing"].Criteria["true"] != "original" {
			t.Errorf("content snapshot changed: %+v", body)
		}

		w.Write([]byte(validResponse))
	}))
	defer server.Close()

	client, _ := typesafe.NewClient(typesafe.Config{APIKey: "test", BaseURL: server.URL})

	_, err := client.SystemOne(context.Background(), typesafe.SystemOneRequest{State: &changingJSON{}, Questions: typesafe.Questions{"billing": typesafe.NoulQuestion{Instructions: &changingJSON{}, Criteria: &typesafe.NoulCriteria{True: &changingJSON{}}}}})
	if err != nil {
		t.Fatal(err)
	}
}

func TestExplicitConfigurationIgnoresEnvironmentAndClonesHeaders(t *testing.T) {
	t.Setenv("TYPESAFE_API_KEY", "environment-key")
	t.Setenv("TYPESAFE_BASE_URL", "http://invalid.local")
	t.Setenv("TYPESAFE_DEFAULT_MODEL", "environment-model")

	if _, err := typesafe.NewClient(typesafe.Config{}); !errors.Is(err, typesafe.ErrValidation) {
		t.Fatal("construction implicitly loaded environment")
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any

		json.NewDecoder(r.Body).Decode(&body)

		if body["model"] != "jev-latest" || r.Header.Get("X-Custom") != "original" || r.Header.Get("Authorization") != "Bearer explicit-key" {
			t.Errorf("configuration=%v headers=%v", body, r.Header)
		}

		w.Write([]byte(validResponse))
	}))

	defer server.Close()

	headers := http.Header{"X-Custom": []string{"original"}}

	client, err := typesafe.NewClient(typesafe.Config{APIKey: "explicit-key", BaseURL: server.URL, Headers: headers})
	if err != nil {
		t.Fatal(err)
	}

	headers.Set("X-Custom", "mutated")

	if _, err := client.SystemOne(context.Background(), noulRequest()); err != nil {
		t.Fatal(err)
	}
}

func TestInvalidConfiguration(t *testing.T) {
	cases := []typesafe.Config{
		{APIKey: "test", BaseURL: "relative/path"},
		{APIKey: "test", BaseURL: "file:///tmp/test"},
		{APIKey: "test", BaseURL: "https://user:password@example.com"},
		{APIKey: "test", BaseURL: "https://example.com?api_key=secret"},
		{APIKey: "test", BaseURL: "https://example.com#fragment"},
		{APIKey: "test", Model: " "},
		{APIKey: "test", AttemptTimeout: -1},
		{APIKey: "test", CallTimeout: -1},
	}
	for _, config := range cases {
		if _, err := typesafe.NewClient(config); !errors.Is(err, typesafe.ErrValidation) {
			t.Fatalf("expected validation error: %v", err)
		}
	}
}
