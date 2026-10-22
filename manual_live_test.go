//go:build typesafe_live

package typesafe_test

import (
	"context"
	"os"
	"testing"
	"time"

	typesafe "github.com/aadidenko/typesafe-sdk-go"
)

// These tests are deliberately absent from ordinary test/CI builds. The second
// gate protects against accidental execution when a live-tag build is selected.
func liveClient(t *testing.T) *typesafe.Client {
	t.Helper()
	if os.Getenv("TYPESAFE_RUN_LIVE") != "1" {
		t.Skip("manual live verification requires TYPESAFE_RUN_LIVE=1")
	}
	if os.Getenv(typesafe.APIKeyEnv) == "" {
		t.Skip("manual live verification requires an externally supplied API key")
	}
	client, err := typesafe.NewClient(typesafe.LoadEnv(typesafe.Config{}))
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestLiveModels(t *testing.T) {
	client := liveClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	response, err := client.ListModels(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 200 || response.Models == nil || len(response.RawBody) == 0 {
		t.Fatal("missing model response metadata")
	}
	// This checks provider contract compatibility, not model quality or availability
	// of any particular hard-coded model name.
}

func TestLiveSystemOneNoul(t *testing.T) {
	client := liveClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	response, err := client.SystemOne(ctx, typesafe.SystemOneRequest{State: "A red rectangle is visible.", Questions: typesafe.Questions{"visible": typesafe.NoulQuestion{Instructions: "Does the supplied text describe a red rectangle?"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := response.Noul("visible"); err != nil {
		t.Fatal(err)
	}
	if response.Model == "" || response.StatusCode != 200 || len(response.RawBody) == 0 {
		t.Fatal("missing evaluation response metadata")
	}
	// Do not assert a semantic probability threshold for probabilistic model output.
}
