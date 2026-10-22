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
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("x-typesafe-request-id", "local-error-example")
		w.WriteHeader(http.StatusForbidden)

		if _, err := io.WriteString(w, `{"detail":"Account cannot use this resource."}`); err != nil {
			log.Printf("write fixture response: %v", err)
		}
	}))
	defer server.Close()

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	config := typesafe.LoadEnv(typesafe.Config{APIKey: "fixture-key", BaseURL: server.URL, Logger: logger})
	// LogBodies defaults to false. Opting in exposes application content and logs
	// only a bounded prefix of the wire body, which may be compressed or binary.
	client, err := typesafe.NewClient(config)
	if err != nil {
		return err
	}

	_, err = client.ListModels(context.Background())

	var failure *typesafe.HTTPError

	if errors.As(err, &failure) {
		if _, err := fmt.Printf("status: %d, request ID: %s, permission denied: %v\n", failure.StatusCode, failure.RequestID, errors.Is(err, typesafe.ErrPermission)); err != nil {
			return err
		}

		// failure.RawBody remains available for explicit application error decoding.
	} else if err != nil {
		return err
	}

	return nil
}
