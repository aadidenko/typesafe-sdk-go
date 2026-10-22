package typesafe_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	typesafe "github.com/aadidenko/typesafe-sdk-go"
)

func TestListModelsUsesGatewayAndRetainsMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/gateway/v1/models" || r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}

		w.Header().Set("x-typesafe-request-id", "models-request")
		fixtureWriteString(t, w, `{"models":[{"name":"jev-test","description":"Test model","release_date":"2026-09-15","extension":true}],"future":42}`)
	}))
	defer server.Close()

	client, err := typesafe.NewClient(typesafe.Config{APIKey: "test-key", BaseURL: server.URL + "/gateway/"})
	if err != nil {
		t.Fatal(err)
	}

	response, err := client.ListModels(context.Background(), typesafe.WithHeaders(http.Header{"x-custom": {"model-header"}}))
	if err != nil {
		t.Fatal(err)
	}

	assertModelDescription(t, response.Models)

	if response.StatusCode != 200 || response.RequestID != "models-request" || !strings.Contains(string(response.RawBody), `"future":42`) {
		t.Fatalf("incomplete HTTP response: %+v", response.HTTPMetadata)
	}
}

func TestListModelsRejectsMalformedSuccessAndRetainsErrorMetadata(t *testing.T) {
	for _, body := range []string{`{`, `{"models":null}`, `{"models":[{"name":"model","release_date":"2026-09-15"}]}`, `{"models":[{"name":"model","description":null,"release_date":"2026-09-15"}]}`, `{"models":[{"name":"model","description":"ok","release_date":12}]}`} {
		t.Run(body, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("x-typesafe-request-id", "invalid-models")
				fixtureWriteString(t, w, body)
			}))
			defer server.Close()

			client, err := typesafe.NewClient(typesafe.Config{APIKey: "test-key", BaseURL: server.URL})
			if err != nil {
				t.Fatal(err)
			}

			_, err = client.ListModels(context.Background())

			var protocol *typesafe.ProtocolError

			if !errors.As(err, &protocol) || protocol.RequestID != "invalid-models" || string(protocol.RawBody) != body {
				t.Fatalf("expected retained protocol error: %v", err)
			}
		})
	}
}

func assertModelDescription(t *testing.T, models []typesafe.ModelMetadata) {
	t.Helper()

	if len(models) != 1 {
		t.Fatalf("model count: %d", len(models))
	}

	model := models[0]
	if model.Name != "jev-test" || model.Description != "Test model" || model.ReleaseDate != "2026-09-15" {
		t.Fatalf("model metadata: %+v", model)
	}
}
