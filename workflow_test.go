package typesafe_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	typesafe "github.com/aadidenko/typesafe-sdk-go"
)

// These fixtures combine the pinned OpenAPI's four independent answer examples;
// Score's two levels and probabilities form a literal worked fractional example.
const completeResponse = `{"model":"vision-actual","answers":{"visible":{"type":"noul","noul":0.98},"color":{"type":"choice","choice":"red","confidence":0.7,"probabilities":{"red":0.8,"blue":0.2}},"size":{"type":"score","score":0.75,"confidence":0.6,"legend":{"0":"Small","1":{"description":"Large","tags":[null]}},"probabilities":{"0":0.25,"1":0.75}},"location":{"type":"box","box":{"top":0.2,"bottom":0.8,"left":0.1,"right":0.9},"confidence":null},"future":{"type":"future","value":{"retained":true}},"additional":{"type":"vector","coordinates":[1,2]}},"usage":{"input_tokens":285,"output_tokens":20},"future_top":{"trace":"kept"}}`

type workflowWire struct {
	Model string `json:"model"`
	State struct {
		Image   string `json:"image"`
		Nested  []any  `json:"nested"`
		Padding string `json:"padding"`
	} `json:"state"`
	Questions map[string]json.RawMessage             `json:"questions"`
	Assets    map[string]struct{ Type, Part string } `json:"assets"`
	Extension string                                 `json:"extension"`
}

func workflowRequest(photo typesafe.Image, reference, suffix string) typesafe.SystemOneRequest {
	type instruction struct {
		Task  string `json:"task"`
		Image string `json:"image"`
	}

	type level struct {
		Description string `json:"description"`
		Tags        []any  `json:"tags"`
	}

	return typesafe.SystemOneRequest{
		State: struct {
			Image   string `json:"image"`
			Nested  []any  `json:"nested"`
			Padding string `json:"padding"`
		}{Image: reference, Nested: []any{nil, "Compare " + reference + " again"}, Padding: strings.Repeat("context ", 200)},
		Assets: typesafe.Assets{reference: photo},
		Questions: typesafe.Questions{
			"visible" + suffix:  typesafe.NoulQuestion{Instructions: instruction{Task: "Is a rectangle visible?", Image: reference}, Criteria: &typesafe.NoulCriteria{True: instruction{Task: "A rectangle is visible", Image: reference}, False: "No rectangle"}},
			"color" + suffix:    typesafe.ChoiceQuestion{Instructions: instruction{Task: "Select the rectangle's color", Image: reference}, Criteria: map[string]any{"red": nil, "blue": instruction{Task: "A blue rectangle", Image: reference}}},
			"size" + suffix:     typesafe.ScoreQuestion{Instructions: instruction{Task: "Rate rectangle size", Image: reference}, Criteria: []any{"Small", level{Description: "Large", Tags: []any{nil}}}},
			"location" + suffix: typesafe.BoxQuestion{Instructions: instruction{Task: "Locate the rectangle", Image: reference}},
		},
		ExtraBody: map[string]any{"extension": "allowed"},
	}
}

func parseWorkflowMultipart(raw []byte, contentType string) (workflowWire, map[string][]byte, error) {
	var request workflowWire

	parts, err := parseMultipartFixture(raw, contentType)
	if err != nil {
		return request, nil, err
	}

	if err := json.Unmarshal(parts.requestJSON, &request); err != nil {
		return request, nil, err
	}

	images := make(map[string][]byte, len(parts.images))
	for name, part := range parts.images {
		images[name] = part.data
	}

	return request, images, nil
}

func TestCompleteWorkflow(t *testing.T) {
	source := pngBytes(t)
	photo := mustImage(t, source)
	fixture := &completeWorkflowFixture{t: t, source: source}
	server := httptest.NewServer(fixture)

	defer server.Close()

	defaultTransport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		t.Fatal("default transport does not support cloning")
	}

	transport := defaultTransport.Clone()

	defer transport.CloseIdleConnections()

	shared := &http.Client{Transport: transport}

	var logs bytes.Buffer

	client := mustClient(t, typesafe.Config{APIKey: "workflow-key", BaseURL: server.URL + "/gateway", HTTPClient: shared, Logger: slog.New(slog.NewJSONHandler(&logs, nil))})

	models, err := client.ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	assertWorkflowDiscovery(t, models)

	request := workflowRequest(photo, "photo", "")

	request.Questions["future"] = typesafe.RawQuestion(`{"type":"future","instructions":{"task":"Preserve future data"}}`)

	response, err := client.SystemOne(context.Background(), request, typesafe.WithModel(models.Models[0].Name), typesafe.WithExperimentalImages(true))
	if err != nil {
		t.Fatal(err)
	}

	assertWorkflowCounts(t, fixture, response)
	assertWorkflowJudgments(t, response)
	assertWorkflowDecoding(t, response)
	assertWorkflowMetadata(t, response, logs.String())
	assertWorkflowPermissionError(t, client, request)
	assertWorkflowHTTPReuse(t, shared, server.URL)
}

func TestConcurrentCompleteRequestsRemainIsolated(t *testing.T) {
	var requests atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serveConcurrentWorkflow(t, w, r, &requests)
	}))

	defer server.Close()

	noRetry := typesafe.RetryPolicy{}

	client, err := typesafe.NewClient(typesafe.Config{APIKey: "test", BaseURL: server.URL, Compression: typesafe.CompressionGzip, Retry: &noRetry, Logger: slog.New(slog.DiscardHandler)})
	if err != nil {
		t.Fatal(err)
	}

	var workers sync.WaitGroup

	for id := range 12 {
		workers.Add(1)

		go func() {
			defer workers.Done()

			exerciseConcurrentWorkflow(t, client, id)
		}()
	}

	workers.Wait()

	if requests.Load() != 12 {
		t.Fatalf("requests=%d", requests.Load())
	}
}

func assertWorkflowNoul(t *testing.T, response *typesafe.SystemOneResponse) {
	t.Helper()

	visible, err := response.Noul("visible")
	if err != nil || visible.Probability != .98 {
		t.Fatalf("Noul=%+v %v", visible, err)
	}
}

func assertWorkflowChoice(t *testing.T, response *typesafe.SystemOneResponse) {
	t.Helper()

	choice, err := response.Choice("color")
	if err != nil || choice.Choice != "red" || choice.Confidence != .7 || choice.Probabilities["red"] != .8 {
		t.Fatalf("Choice=%+v %v", choice, err)
	}
}

func assertWorkflowScore(t *testing.T, response *typesafe.SystemOneResponse) {
	t.Helper()

	score, err := response.Score("size")
	if err != nil || score.Score != .75 || score.Confidence != .6 || score.Legend[0] != "Small" || score.Probabilities[1] != .75 {
		t.Fatalf("Score=%+v %v", score, err)
	}
}

func assertWorkflowBox(t *testing.T, response *typesafe.SystemOneResponse) {
	t.Helper()

	box, err := response.Box("location")
	if err != nil || box.Confidence != nil || box.Box.Top != .2 || box.Box.Bottom != .8 || box.Box.Left != .1 || box.Box.Right != .9 {
		t.Fatalf("Box=%+v %v", box, err)
	}
}

func assertWorkflowDecoding(t *testing.T, response *typesafe.SystemOneResponse) {
	t.Helper()

	var app struct {
		Visible  typesafe.NoulAnswer
		Color    typesafe.ChoiceAnswer
		Size     typesafe.ScoreAnswer
		Location typesafe.BoxAnswer
		Future   struct{ Value struct{ Retained bool } }
	}

	if err := response.DecodeAnswers(&app); err != nil || app.Visible.Probability != .98 || app.Color.Choice != "red" || app.Size.Score != .75 || app.Location.Confidence != nil || !app.Future.Value.Retained {
		t.Fatalf("application decode=%+v error=%v", app, err)
	}

	var full struct {
		FutureTop struct{ Trace string } `json:"future_top"`
	}

	if err := response.DecodeResponse(&full); err != nil || full.FutureTop.Trace != "kept" {
		t.Fatalf("complete decode=%+v error=%v", full, err)
	}
}

func assertWorkflowMetadata(t *testing.T, response *typesafe.SystemOneResponse, logs string) {
	t.Helper()

	if response.StatusCode != 200 || response.RequestID != "workflow-success" || response.Headers.Get("X-Checkpoint") != "retained" || string(response.RawBody) != completeResponse {
		t.Fatal("retained metadata changed after decoding")
	}

	if strings.Contains(logs, "workflow-key") || strings.Contains(logs, "Compare photo again") || !strings.Contains(logs, "typesafe retry") {
		t.Fatal("metadata logging leaked secrets/body or omitted retry")
	}
}

func assertWorkflowPermissionError(t *testing.T, client *typesafe.Client, request typesafe.SystemOneRequest) {
	t.Helper()

	_, err := client.SystemOne(context.Background(), request, typesafe.WithExperimentalImages(true), typesafe.WithHeaders(http.Header{"X-Simulate-Error": {"true"}}))

	var failure *typesafe.HTTPError

	if !errors.Is(err, typesafe.ErrPermission) || !errors.As(err, &failure) || failure.RequestID != "workflow-denied" || string(failure.RawBody) != "plain-text denied" {
		t.Fatalf("workflow error=%v", err)
	}
}

func assertWorkflowHTTPReuse(t *testing.T, shared *http.Client, baseURL string) {
	t.Helper()

	// The same caller-owned standard HTTP client remains usable after SDK calls.
	directRequest, err := http.NewRequestWithContext(context.Background(), http.MethodGet, baseURL+"/gateway/v1/models", nil)
	if err != nil {
		t.Fatal(err)
	}

	directRequest.Header.Set("Authorization", "Bearer workflow-key")

	direct, err := shared.Do(directRequest)
	if err != nil {
		t.Fatal(err)
	}

	if err := direct.Body.Close(); err != nil {
		t.Fatal(err)
	}
}

func assertWorkflowDiscovery(t *testing.T, models *typesafe.ListModelsResponse) {
	t.Helper()

	if len(models.Models) != 1 || models.Models[0].Name != "vision-alias" || models.RequestID != "model-discovery" {
		t.Fatalf("models=%+v", models)
	}
}

func assertWorkflowCounts(t *testing.T, fixture *completeWorkflowFixture, response *typesafe.SystemOneResponse) {
	t.Helper()

	if fixture.modelsCalls.Load() != 1 || fixture.attempts.Load() != 2 || response.Model != "vision-actual" || response.Usage.InputTokens != 285 || response.Usage.OutputTokens != 20 {
		t.Fatalf("workflow counts/model/usage: %+v", response)
	}
}

func assertWorkflowJudgments(t *testing.T, response *typesafe.SystemOneResponse) {
	t.Helper()
	assertWorkflowNoul(t, response)
	assertWorkflowChoice(t, response)
	assertWorkflowScore(t, response)
	assertWorkflowBox(t, response)

	for _, kind := range []string{"noul", "choice", "score", "box", "future", "vector"} {
		if len(response.AnswersByKind(kind)) != 1 {
			t.Errorf("kind %s not retained", kind)
		}
	}
}
