package typesafe_test

import (
	"context"
	"errors"
	"io"
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
		io.WriteString(w, `{"models":[{"name":"jev-test","description":"Test model","release_date":"2026-09-15","extension":true}],"future":42}`)
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

	if len(response.Models) != 1 || response.Models[0].Name != "jev-test" || response.Models[0].Description != "Test model" || response.Models[0].ReleaseDate != "2026-09-15" || response.StatusCode != 200 || response.RequestID != "models-request" || !strings.Contains(string(response.RawBody), `"future":42`) {
		t.Fatalf("incomplete response: %+v", response)
	}
}

func TestListModelsRejectsMalformedSuccessAndRetainsErrorMetadata(t *testing.T) {
	for _, body := range []string{`{`, `{"models":null}`, `{"models":[{"name":"model","release_date":"2026-09-15"}]}`, `{"models":[{"name":"model","description":null,"release_date":"2026-09-15"}]}`, `{"models":[{"name":"model","description":"ok","release_date":12}]}`} {
		t.Run(body, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("x-typesafe-request-id", "invalid-models")
				io.WriteString(w, body)
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
