package typesafe_test

import (
	"context"
	"errors"
	"fmt"
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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fixtureWriteString(t, w, `{"models":[]}`) }))
	defer server.Close()

	defaultTransport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		t.Fatal("default transport does not support cloning")
	}

	clonedTransport := defaultTransport.Clone()
	transport := &ownedTransport{RoundTripper: clonedTransport}

	defer clonedTransport.CloseIdleConnections()

	redirect := func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	shared := &http.Client{Transport: transport, Timeout: 9 * time.Second, CheckRedirect: redirect}

	client := mustClient(t, typesafe.Config{APIKey: "test-key", BaseURL: server.URL, HTTPClient: shared})

	if _, err := client.ListModels(context.Background()); err != nil {
		t.Fatal(err)
	}

	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}

	response, err := shared.Do(request)
	if err != nil {
		t.Fatal(err)
	}

	if err := response.Body.Close(); err != nil {
		t.Fatal(err)
	}

	if shared.Transport != transport || shared.Timeout != 9*time.Second || shared.CheckRedirect == nil || transport.closed.Load() {
		t.Fatal("SDK mutated or closed caller resources")
	}
}

func TestConcurrentModelsAndEvaluationsKeepCallSettingsIsolated(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serveIsolatedCall(t, w, r)
	}))
	defer server.Close()

	headers := http.Header{"x-session": {"default"}}

	client, err := typesafe.NewClient(typesafe.Config{APIKey: "test-key", BaseURL: server.URL + "/gateway", Model: "default", Headers: headers})
	if err != nil {
		t.Fatal(err)
	}

	//nolint:staticcheck // SA1008: deliberately mutate the original noncanonical map key to verify snapshot isolation.
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
		fixtureWriteString(t, w, "denied")
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

func serveIsolatedCall(t *testing.T, w http.ResponseWriter, r *http.Request) {
	t.Helper()

	if r.URL.Path == "/gateway/v1/models" {
		if r.Header.Get("X-Session") == "" {
			t.Error("missing model-call header")
		}

		fixtureWriteString(t, w, `{"models":[]}`)

		return
	}

	var body struct{ Model string }

	if !fixtureDecode(t, r.Body, &body) {
		return
	}

	if r.Header.Get("X-Session") != body.Model {
		t.Errorf("settings leaked: %s/%s", r.Header.Get("X-Session"), body.Model)
	}

	fixtureWriteString(t, w, fmt.Sprintf(`{"model":%q,"answers":{"billing":{"type":"noul","noul":0.5}},"usage":{"input_tokens":0,"output_tokens":0}}`, body.Model))
}
