package typesafe_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	typesafe "github.com/aadidenko/typesafe-sdk-go"
)

func TestLoggingIsOptionalAndBodiesNeedSeparateOptIn(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("x-typesafe-request-id", "logging-id")
		fixtureWriteString(t, w, `{"model":"response-body-marker","answers":{"billing":{"type":"noul","noul":0.5}},"usage":{"input_tokens":0,"output_tokens":0}}`)
	}))
	defer server.Close()

	for _, test := range []struct {
		name             string
		configured, body bool
	}{{name: "default silence", configured: false, body: false}, {name: "metadata only", configured: true, body: false}, {name: "bodies enabled", configured: true, body: true}} {
		t.Run(test.name, func(t *testing.T) {
			exerciseLoggingOptIn(t, server.URL, test.configured, test.body)
		})
	}
}

func TestLoggingRedactsSecretHeadersAcrossRetries(t *testing.T) {
	var requests atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Set-Cookie", "response-cookie-secret")
		w.Header().Set("X-Session-Token", "response-token-secret")
		w.Header().Set("x-typesafe-request-id", "retry-log-id")

		if requests.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			fixtureWriteString(t, w, "server-error-body")

			return
		}

		fixtureWriteString(t, w, `{"models":[]}`)
	}))

	defer server.Close()

	var logs bytes.Buffer

	client, err := typesafe.NewClient(typesafe.Config{APIKey: "api-key-secret", BaseURL: server.URL, Logger: slog.New(slog.NewJSONHandler(&logs, nil)), Headers: http.Header{"cookie": {"request-cookie-secret"}, "x-custom-token": {"request-token-secret"}, "X-Custom-Secret": {"request-secret-secret"}, "X-Public": {"visible"}}, Retry: &typesafe.RetryPolicy{MaxRetries: 1, HTTPStatuses: []int{503}}})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := client.ListModels(context.Background()); err != nil {
		t.Fatal(err)
	}

	output := logs.String()
	for _, secret := range []string{"api-key-secret", "request-cookie-secret", "request-token-secret", "request-secret-secret", "response-cookie-secret", "response-token-secret", "server-error-body"} {
		if strings.Contains(output, secret) {
			t.Errorf("secret/body leaked: %s", secret)
		}
	}

	for _, expected := range []string{`"status":503`, `"status":200`, `"retry":1`, `"attempt":2`, `"delay"`, `visible`, `[REDACTED]`} {
		if !strings.Contains(output, expected) {
			t.Errorf("missing retry metadata %s: %s", expected, output)
		}
	}

	decoder := json.NewDecoder(strings.NewReader(output))
	for decoder.More() {
		var record map[string]any

		if err := decoder.Decode(&record); err != nil {
			t.Fatal(err)
		}
	}
}

func TestLoggingProtectsHeaderVariantsAndFailureCauses(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		for _, name := range []string{"X-API-Key", "Api_Key", "X-Gateway-Key", "X-Auth-Token", "X-Client-Secret", "CoOkIe", "PrOxY-AuThOrIzAtIoN"} {
			w.Header().Set(name, "header-secret-"+name)
		}

		w.Header().Set("x-typesafe-request-id", "failure-id")
		fixtureWriteString(t, w, `{"models":[{"name":"model","description":"secret-body-marker"}]}`)
	}))
	defer server.Close()

	var logs bytes.Buffer

	logger := slog.New(slog.NewJSONHandler(&logs, nil))

	client, err := typesafe.NewClient(typesafe.Config{APIKey: "private-key", BaseURL: server.URL, Logger: logger})
	if err != nil {
		t.Fatal(err)
	}

	_, err = client.ListModels(context.Background())
	if !errors.Is(err, typesafe.ErrProtocol) {
		t.Fatalf("expected protocol failure: %v", err)
	}

	if !strings.Contains(logs.String(), `"failure":"protocol"`) || strings.Contains(logs.String(), "secret-body-marker") || strings.Contains(logs.String(), "header-secret-") {
		t.Fatalf("unsafe/missing protocol diagnostics: %s", logs.String())
	}

	assertTransportFailureLogs(t, logger, server.URL, &logs)
}

type logErrorTransport struct{}

func (logErrorTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return nil, fmt.Errorf("transport-private-marker %s", r.Header.Get("Authorization"))
}

func TestLoggingTruncatesBodiesWithoutChangingRetainedData(t *testing.T) {
	body := `{"model":"actual","answers":{"billing":{"type":"noul","noul":0.5}},"usage":{"input_tokens":0,"output_tokens":0},"extra":"` + strings.Repeat("x", 9000) + `END-MARKER"}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fixtureWriteString(t, w, body) }))

	defer server.Close()

	var logs bytes.Buffer

	client, err := typesafe.NewClient(typesafe.Config{APIKey: "key", BaseURL: server.URL, Logger: slog.New(slog.NewJSONHandler(&logs, nil)), LogBodies: true})
	if err != nil {
		t.Fatal(err)
	}

	response, err := client.SystemOne(context.Background(), typesafe.SystemOneRequest{State: "sample", Questions: typesafe.Questions{"billing": typesafe.NoulQuestion{Instructions: "billing?"}}})
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(logs.String(), `"body_truncated":true`) || strings.Contains(logs.String(), "END-MARKER") || logs.Len() > 15000 {
		t.Fatalf("body logging was not bounded: %d", logs.Len())
	}

	if string(response.RawBody) != body {
		t.Fatal("logging truncated retained response body")
	}

	var full struct{ Extra string }

	if err := response.DecodeResponse(&full); err != nil || !strings.HasSuffix(full.Extra, "END-MARKER") {
		t.Fatal("logging consumed complete decoding data")
	}
}

func TestOptionalLogLevelsAreSnapshotsAndNeverInstallLogger(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Fail") == "true" {
			w.WriteHeader(http.StatusForbidden)

			return
		}

		fixtureWriteString(t, w, `{"models":[]}`)
	}))
	defer server.Close()

	var logs bytes.Buffer

	logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	level := slog.LevelError

	client, err := typesafe.NewClient(typesafe.Config{APIKey: "key", BaseURL: server.URL, Logger: logger, LogLevel: &level})
	if err != nil {
		t.Fatal(err)
	}

	level = slog.LevelDebug

	if _, err := client.ListModels(context.Background()); err != nil {
		t.Fatal(err)
	}

	if logs.Len() != 0 {
		t.Fatal("caller mutation changed log level snapshot")
	}

	if _, err := client.ListModels(context.Background(), typesafe.WithHeaders(http.Header{"X-Fail": {"true"}})); err == nil {
		t.Fatal("expected error")
	}

	if !strings.Contains(logs.String(), `"failure":"http"`) {
		t.Fatal("error level suppressed failures")
	}

	logs.Reset()
	t.Setenv("TYPESAFE_LOG_LEVEL", "off")

	config := typesafe.LoadEnv(typesafe.Config{APIKey: "key", BaseURL: server.URL, Logger: logger, LogBodies: true})

	client, err = typesafe.NewClient(config)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := client.ListModels(context.Background(), typesafe.WithHeaders(http.Header{"X-Fail": {"true"}})); !errors.Is(err, typesafe.ErrPermission) {
		t.Fatalf("expected HTTP forbidden with logging disabled: %v", err)
	}

	if logs.Len() != 0 {
		t.Fatal("off log level emitted diagnostics")
	}
}

func TestConcurrentLoggingKeepsResponseMetadataSeparate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Call", r.Header.Get("X-Call"))
		w.Header().Set("x-typesafe-request-id", r.Header.Get("X-Call"))
		fixtureWriteString(t, w, `{"models":[]}`)
	}))
	defer server.Close()

	var logs bytes.Buffer

	client, err := typesafe.NewClient(typesafe.Config{APIKey: "key", BaseURL: server.URL, Logger: slog.New(slog.NewJSONHandler(&logs, nil))})
	if err != nil {
		t.Fatal(err)
	}

	var workers sync.WaitGroup

	for i := range 24 {
		workers.Add(1)

		go func() {
			defer workers.Done()

			_, err := client.ListModels(context.Background(), typesafe.WithHeaders(http.Header{"X-Call": {fmt.Sprintf("call-%d", i)}}))
			if err != nil {
				t.Error(err)
			}
		}()
	}

	workers.Wait()

	assertConcurrentResponseLogs(t, logs.Bytes())
}

func TestLoggingPreservesActiveCancellation(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-typesafe-request-id", "partial-id")

		if !fixtureWriteString(t, w, `{"models":`) {
			return
		}

		if !fixtureFlush(t, w) {
			return
		}

		close(started)
		<-r.Context().Done()
	}))

	defer server.Close()

	var logs bytes.Buffer

	client, err := typesafe.NewClient(typesafe.Config{APIKey: "key", BaseURL: server.URL, Logger: slog.New(slog.NewJSONHandler(&logs, nil))})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	defer cancel()

	result := make(chan error, 1)

	go func() { _, err := client.ListModels(ctx); result <- err }()

	<-started
	cancel()

	select {
	case err := <-result:
		var transport *typesafe.TransportError

		if !errors.Is(err, context.Canceled) || !errors.As(err, &transport) {
			t.Fatalf("cancellation metadata lost: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("logging delayed cancellation")
	}

	if !strings.Contains(logs.String(), `"failure":"canceled"`) {
		t.Fatalf("missing cancellation diagnostics: %s", logs.String())
	}
}

func TestLoggingReportsSystemOneProtocolFailures(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("x-typesafe-request-id", "protocol-evaluation-id")
		fixtureWriteString(t, w, `{"model":"actual","answers":{"other":{"type":"noul","noul":0.5}},"usage":{"input_tokens":0,"output_tokens":0}}`)
	}))
	defer server.Close()

	var logs bytes.Buffer

	client, err := typesafe.NewClient(typesafe.Config{APIKey: "key", BaseURL: server.URL, Logger: slog.New(slog.NewJSONHandler(&logs, nil))})
	if err != nil {
		t.Fatal(err)
	}

	_, err = client.SystemOne(context.Background(), typesafe.SystemOneRequest{State: "sample", Questions: typesafe.Questions{"billing": typesafe.NoulQuestion{Instructions: "billing?"}}})
	if !errors.Is(err, typesafe.ErrProtocol) || !strings.Contains(logs.String(), `"failure":"protocol"`) || !strings.Contains(logs.String(), "protocol-evaluation-id") {
		t.Fatalf("protocol error diagnostics missing: %v %s", err, logs.String())
	}
}

func assertLoggingOutput(t *testing.T, output string, bodies bool) {
	t.Helper()

	for _, expected := range []string{"/v1/systemone", "logging-id", `"status":200`, `"attempt":1`, `"duration"`} {
		if !strings.Contains(output, expected) {
			t.Errorf("missing metadata %q: %s", expected, output)
		}
	}

	for _, marker := range []string{"request-body-marker", "response-body-marker"} {
		if strings.Contains(output, marker) != bodies {
			t.Errorf("body opt-in not respected for %s: %s", marker, output)
		}
	}
}

func assertTransportFailureLogs(t *testing.T, logger *slog.Logger, baseURL string, logs *bytes.Buffer) {
	t.Helper()

	logs.Reset()

	transport := logErrorTransport{}

	client, err := typesafe.NewClient(typesafe.Config{APIKey: "private-key", BaseURL: baseURL, Logger: logger, HTTPClient: &http.Client{Transport: transport}, Retry: &typesafe.RetryPolicy{}})
	if err != nil {
		t.Fatal(err)
	}

	_, err = client.ListModels(context.Background())
	if !errors.Is(err, typesafe.ErrTransport) {
		t.Fatalf("expected transport error: %v", err)
	}

	if strings.Contains(logs.String(), "private-key") || strings.Contains(logs.String(), "transport-private-marker") || strings.Contains(err.Error(), "private-key") || !strings.Contains(logs.String(), `"failure":"transport"`) {
		t.Fatalf("unsafe/missing transport diagnostics: %s", logs.String())
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	logs.Reset()

	_, err = client.ListModels(ctx)
	if !errors.Is(err, context.Canceled) || !strings.Contains(logs.String(), `"failure":"canceled"`) {
		t.Fatalf("cancellation diagnostics missing: %v %s", err, logs.String())
	}
}

func assertConcurrentResponseLogs(t *testing.T, data []byte) {
	t.Helper()

	decoder := json.NewDecoder(bytes.NewReader(data))
	responses := 0

	for {
		var record struct {
			Message   string              `json:"msg"`
			RequestID string              `json:"request_id"`
			Headers   map[string][]string `json:"headers"`
		}

		if err := decoder.Decode(&record); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}

		if record.Message == "typesafe response" {
			responses++

			if len(record.Headers["X-Call"]) != 1 || record.Headers["X-Call"][0] != record.RequestID {
				t.Fatalf("concurrent response metadata leaked: %+v", record)
			}
		}
	}

	if responses != 24 {
		t.Fatalf("response log count: %d", responses)
	}
}

func exerciseLoggingOptIn(t *testing.T, baseURL string, configured, bodies bool) {
	t.Helper()

	var logs bytes.Buffer

	logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	original := slog.Default()

	slog.SetDefault(logger)

	defer slog.SetDefault(original)

	config := typesafe.Config{APIKey: "test-key", BaseURL: baseURL, LogBodies: bodies}
	if configured {
		config.Logger = logger
	}

	client, err := typesafe.NewClient(config)
	if err != nil {
		t.Fatal(err)
	}

	response, err := client.SystemOne(context.Background(), typesafe.SystemOneRequest{State: "request-body-marker", Questions: typesafe.Questions{"billing": typesafe.NoulQuestion{Instructions: "billing?"}}})
	if err != nil {
		t.Fatal(err)
	}

	if !configured {
		if logs.Len() != 0 {
			t.Fatalf("SDK used global logging: %s", logs.String())
		}

		return
	}

	assertLoggingOutput(t, logs.String(), bodies)

	if response.Model != "response-body-marker" || response.RequestID != "logging-id" {
		t.Fatal("logging consumed response")
	}

	if slog.Default() != logger {
		t.Fatal("SDK changed caller global logger")
	}
}
