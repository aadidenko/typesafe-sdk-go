package typesafe_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"

	typesafe "github.com/aadidenko/typesafe-sdk-go"
)

func TestManagedHeadersAreRejectedBeforeHTTP(t *testing.T) {
	var requests atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		fixtureWriteString(t, w, `{"models":[]}`)
	}))

	defer server.Close()

	for _, name := range []string{"authorization", "Accept", "user-agent", "x-typesafe-sdk", "X-TypeSafe-Runtime", "x-typesafe-retry-count", "content-type", "content-encoding", "Host", "Content-Length", "Transfer-Encoding", "Connection", "Trailer", "TE", "Upgrade", "TypeSafe-Danger-Zone-Images"} {
		t.Run(name, func(t *testing.T) {
			_, err := typesafe.NewClient(typesafe.Config{APIKey: "test-key", BaseURL: server.URL, Headers: http.Header{name: {"override"}}})
			if !errors.Is(err, typesafe.ErrValidation) {
				t.Errorf("constructor allowed protected header %s: %v", name, err)
			}

			client, err := typesafe.NewClient(typesafe.Config{APIKey: "test-key", BaseURL: server.URL})
			if err != nil {
				t.Fatal(err)
			}

			_, err = client.ListModels(context.Background(), typesafe.WithHeaders(http.Header{name: {"override"}}))
			if !errors.Is(err, typesafe.ErrValidation) {
				t.Errorf("call allowed protected header %s: %v", name, err)
			}
		})
	}

	if requests.Load() != 0 {
		t.Fatalf("invalid headers reached HTTP: %d", requests.Load())
	}
}

func TestHeadersAreCanonicalOverridesAndCallerOwned(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !reflect.DeepEqual(r.Header.Values("X-Project"), []string{"call", "second"}) || r.Header.Get("TypeSafe-Danger-Zone-Long-Context") != "true" {
			t.Errorf("unexpected headers: %v", r.Header)
		}

		fixtureWriteString(t, w, `{"models":[]}`)
	}))
	defer server.Close()

	defaults := http.Header{"x-project": {"default"}}
	overrides := http.Header{"x-PROJECT": {"call", "second"}, "typesafe-danger-zone-long-context": {"true"}}

	client, err := typesafe.NewClient(typesafe.Config{APIKey: "test-key", BaseURL: server.URL, Headers: defaults})
	if err != nil {
		t.Fatal(err)
	}

	option := typesafe.WithHeaders(overrides)

	//nolint:staticcheck // SA1008: mutate the original noncanonical key to verify the SDK snapshot.
	defaults["x-project"][0] = "mutated-default"
	//nolint:staticcheck // SA1008: mutate mixed-case caller data after the option snapshots it.
	overrides["x-PROJECT"][0] = "mutated-call"

	if _, err := client.ListModels(context.Background(), option); err != nil {
		t.Fatal(err)
	}

	//nolint:staticcheck // SA1008: assert the SDK has preserved both original noncanonical caller keys.
	if defaults["x-project"][0] != "mutated-default" || overrides["x-PROJECT"][0] != "mutated-call" {
		t.Fatal("SDK modified caller headers")
	}
}
