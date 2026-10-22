package typesafe_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	typesafe "github.com/aadidenko/typesafe-sdk-go"
)

func TestEnvironmentIsExplicitAndExplicitConfigWins(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct{ Model string }
		json.NewDecoder(r.Body).Decode(&request)
		if r.URL.Path != "/gateway/v1/systemone" || r.Header.Get("Authorization") != "Bearer explicit-key" || request.Model != "explicit-model" {
			t.Errorf("unexpected environment-derived request: %s %s %s", r.URL.Path, r.Header.Get("Authorization"), request.Model)
		}
		io.WriteString(w, `{"model":"actual","answers":{"yes":{"type":"noul","noul":0.8}},"usage":{"input_tokens":0,"output_tokens":0}}`)
	}))
	defer server.Close()
	t.Setenv("TYPESAFE_API_KEY", "env-key")
	t.Setenv("TYPESAFE_BASE_URL", server.URL+"/gateway")
	t.Setenv("TYPESAFE_DEFAULT_MODEL", "env-model")
	t.Setenv("TYPESAFE_LOG_LEVEL", " debug ")
	if _, err := typesafe.NewClient(typesafe.Config{}); err == nil {
		t.Fatal("constructor implicitly loaded environment")
	}
	config := typesafe.LoadEnv(typesafe.Config{APIKey: " explicit-key ", Model: "explicit-model"})
	if config.LogLevel == nil || *config.LogLevel != slog.LevelDebug {
		t.Fatalf("missing opt-in log level: %+v", config.LogLevel)
	}
	client, err := typesafe.NewClient(config)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.SystemOne(context.Background(), typesafe.SystemOneRequest{State: "sample", Questions: typesafe.Questions{"yes": typesafe.NoulQuestion{Instructions: "yes?"}}})
	if err != nil {
		t.Fatal(err)
	}
}

func TestBlankEnvironmentAndExplicitLogLevel(t *testing.T) {
	for _, name := range []string{"TYPESAFE_API_KEY", "TYPESAFE_BASE_URL", "TYPESAFE_DEFAULT_MODEL", "TYPESAFE_LOG_LEVEL"} {
		t.Setenv(name, " \t ")
	}
	config := typesafe.LoadEnv(typesafe.Config{APIKey: "key"})
	if config.BaseURL != "" || config.Model != "" || config.LogLevel != nil {
		t.Fatalf("blank environment replaced defaults: %+v", config)
	}
	t.Setenv("TYPESAFE_LOG_LEVEL", "debug")
	level := slog.LevelError
	config = typesafe.LoadEnv(typesafe.Config{APIKey: "key", LogLevel: &level})
	if config.LogLevel == nil || *config.LogLevel != slog.LevelError {
		t.Fatal("explicit log level lost")
	}
}
