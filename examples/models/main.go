// Command models lists account models using explicitly loaded environment
// configuration and a caller-owned HTTP client. It makes a live API request
// only when invoked; go test compiles it without running main.
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	typesafe "github.com/aadidenko/typesafe-sdk-go"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	defaultTransport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return fmt.Errorf("default HTTP transport must be *http.Transport")
	}

	transport := defaultTransport.Clone()

	transport.MaxIdleConnsPerHost = 20

	defer transport.CloseIdleConnections()

	shared := &http.Client{Transport: transport}
	// Set TYPESAFE_API_KEY and optionally TYPESAFE_BASE_URL (including a gateway
	// prefix), TYPESAFE_DEFAULT_MODEL, and TYPESAFE_LOG_LEVEL outside the program.
	config := typesafe.LoadEnv(typesafe.Config{HTTPClient: shared})

	client, err := typesafe.NewClient(config)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)

	defer cancel()

	response, err := client.ListModels(ctx, typesafe.WithHeaders(http.Header{"X-Application": {"model-discovery"}}))
	if err != nil {
		return err
	}

	for _, model := range response.Models {
		if _, err := fmt.Printf("%s (%s): %s\n", model.Name, model.ReleaseDate, model.Description); err != nil {
			return err
		}
	}

	// A selected response.Models[i].Name may be supplied to WithModel on a
	// subsequent SystemOne call. That option does not mutate client defaults.
	if _, err := fmt.Printf("request ID: %s\n", response.RequestID); err != nil {
		return err
	}

	return nil
}
