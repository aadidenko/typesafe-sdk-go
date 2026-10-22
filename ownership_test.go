package typesafe_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	typesafe "github.com/aadidenko/typesafe-sdk-go"
)

type ownedTransport struct {
	http.RoundTripper
	closed atomic.Bool
}

func (t *ownedTransport) CloseIdleConnections() { t.closed.Store(true) }

func TestSuppliedHTTPClientAndTransportRemainCallerOwned(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, `{"models":[]}`) }))
	defer server.Close()

	transport := &ownedTransport{RoundTripper: http.DefaultTransport.(*http.Transport).Clone()}

	defer transport.RoundTripper.(*http.Transport).CloseIdleConnections()

	redirect := func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }
	shared := &http.Client{Transport: transport, Timeout: 9 * time.Second, CheckRedirect: redirect}

	client, err := typesafe.NewClient(typesafe.Config{APIKey: "test-key", BaseURL: server.URL, HTTPClient: shared})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := client.ListModels(context.Background()); err != nil {
		t.Fatal(err)
	}

	response, err := shared.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}

	response.Body.Close()

	if shared.Transport != transport || shared.Timeout != 9*time.Second || shared.CheckRedirect == nil || transport.closed.Load() {
		t.Fatal("SDK mutated or closed caller resources")
	}
}

func TestConcurrentModelsAndEvaluationsKeepCallSettingsIsolated(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/gateway/v1/models" {
			if r.Header.Get("X-Session") == "" {
				t.Error("missing model-call header")
			}

			io.WriteString(w, `{"models":[]}`)

			return
		}

		var body struct{ Model string }

		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}

		if r.Header.Get("X-Session") != body.Model {
			t.Errorf("settings leaked: %s/%s", r.Header.Get("X-Session"), body.Model)
		}

		fmt.Fprintf(w, `{"model":%q,"answers":{"billing":{"type":"noul","noul":0.5}},"usage":{"input_tokens":0,"output_tokens":0}}`, body.Model)
	}))
	defer server.Close()

	headers := http.Header{"x-session": {"default"}}

	client, err := typesafe.NewClient(typesafe.Config{APIKey: "test-key", BaseURL: server.URL + "/gateway", Model: "default", Headers: headers})
	if err != nil {
		t.Fatal(err)
	}

	headers["x-session"][0] = "caller-changed"

	var wg sync.WaitGroup

	for i := range 16 {
		wg.Add(1)

		go func() {
			defer wg.Done()

			name := fmt.Sprintf("call-%d", i)
			options := []typesafe.CallOption{typesafe.WithModel(name), typesafe.WithHeaders(http.Header{"X-Session": {name}})}

			if _, err := client.ListModels(context.Background(), options...); err != nil {
				t.Error(err)
			}

			response, err := client.SystemOne(context.Background(), typesafe.SystemOneRequest{State: "sample", Questions: typesafe.Questions{"billing": typesafe.NoulQuestion{Instructions: "yes?"}}}, options...)
			if err != nil {
				t.Error(err)

				return
			}

			if response.Model != name {
				t.Errorf("wrong model: %s", response.Model)
			}
		}()
	}

	wg.Wait()

	response, err := client.SystemOne(context.Background(), typesafe.SystemOneRequest{State: "sample", Questions: typesafe.Questions{"billing": typesafe.NoulQuestion{Instructions: "yes?"}}})
	if err != nil || response.Model != "default" {
		t.Fatalf("defaults mutated: %v %v", response, err)
	}
}

func TestListModelsSharesCancellationHTTPErrorAndTimeoutBehavior(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Behavior") == "hang" {
			<-r.Context().Done()

			return
		}

		w.Header().Set("x-typesafe-request-id", "models-forbidden")
		w.WriteHeader(http.StatusForbidden)
		io.WriteString(w, "denied")
	}))
	defer server.Close()

	client, err := typesafe.NewClient(typesafe.Config{APIKey: "test-key", BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}

	_, err = client.ListModels(context.Background())

	var failure *typesafe.HTTPError

	if !errors.As(err, &failure) || !errors.Is(err, typesafe.ErrPermission) || failure.RequestID != "models-forbidden" || string(failure.RawBody) != "denied" {
		t.Fatalf("metadata lost: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = client.ListModels(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}

	_, err = client.ListModels(context.Background(), typesafe.WithHeaders(http.Header{"X-Behavior": {"hang"}}), typesafe.WithTimeouts(20*time.Millisecond, 40*time.Millisecond))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline lost: %v", err)
	}
}
