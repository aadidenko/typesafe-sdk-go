// Command logging demonstrates optional diagnostics and error inspection against
// a local HTTP fixture. It requires no API credentials or provider requests.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"

	typesafe "github.com/aadidenko/typesafe-sdk-go"
)

func main() {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-typesafe-request-id", "local-error-example")
		w.WriteHeader(http.StatusForbidden)
		io.WriteString(w, `{"detail":"Account cannot use this resource."}`)
	}))
	defer server.Close()
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	config := typesafe.LoadEnv(typesafe.Config{APIKey: "fixture-key", BaseURL: server.URL, Logger: logger})
	// LogBodies defaults to false. Opting in exposes application content and logs
	// only a bounded prefix of the wire body, which may be compressed or binary.
	client, err := typesafe.NewClient(config)
	if err != nil {
		log.Fatal(err)
	}
	_, err = client.ListModels(context.Background())
	var failure *typesafe.HTTPError
	if errors.As(err, &failure) {
		fmt.Printf("status: %d, request ID: %s, permission denied: %v\n", failure.StatusCode, failure.RequestID, errors.Is(err, typesafe.ErrPermission))
		// failure.RawBody remains available for explicit application error decoding.
	} else if err != nil {
		log.Fatal(err)
	}
}
