package typesafe_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
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
		}{reference, []any{nil, "Compare " + reference + " again"}, strings.Repeat("context ", 200)},
		Assets: typesafe.Assets{reference: photo},
		Questions: typesafe.Questions{
			"visible" + suffix:  typesafe.NoulQuestion{Instructions: instruction{"Is a rectangle visible?", reference}, Criteria: &typesafe.NoulCriteria{True: instruction{"A rectangle is visible", reference}, False: "No rectangle"}},
			"color" + suffix:    typesafe.ChoiceQuestion{Instructions: instruction{"Select the rectangle's color", reference}, Criteria: map[string]any{"red": nil, "blue": instruction{"A blue rectangle", reference}}},
			"size" + suffix:     typesafe.ScoreQuestion{Instructions: instruction{"Rate rectangle size", reference}, Criteria: []any{"Small", level{"Large", []any{nil}}}},
			"location" + suffix: typesafe.BoxQuestion{Instructions: instruction{"Locate the rectangle", reference}},
		},
		ExtraBody: map[string]any{"extension": "allowed"},
	}
}

func parseWorkflowMultipart(raw []byte, contentType string) (workflowWire, map[string][]byte, error) {
	var request workflowWire

	media, params, err := mime.ParseMediaType(contentType)

	if err != nil || media != "multipart/form-data" {
		return request, nil, fmt.Errorf("expected multipart: %s", contentType)
	}

	reader := multipart.NewReader(bytes.NewReader(raw), params["boundary"])

	first, err := reader.NextPart()
	if err != nil {
		return request, nil, err
	}

	if first.FormName() != "request" || first.Header.Get("Content-Type") != "application/json" {
		return request, nil, fmt.Errorf("first part must be application/json request")
	}

	if err := json.NewDecoder(first).Decode(&request); err != nil {
		return request, nil, err
	}

	images := make(map[string][]byte)

	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}

		if err != nil {
			return request, nil, err
		}

		if part.FormName() == "" || images[part.FormName()] != nil {
			return request, nil, fmt.Errorf("empty or duplicate part name")
		}

		body, err := io.ReadAll(part)
		if err != nil {
			return request, nil, err
		}

		images[part.FormName()] = body
	}

	return request, images, nil
}

func TestCompleteWorkflow(t *testing.T) {
	source := pngBytes(t)

	photo, err := typesafe.ImageFromBytes(source)
	if err != nil {
		t.Fatal(err)
	}

	var (
		modelsCalls, attempts atomic.Int32
		originalWire          []byte
		originalContentType   string
		logs                  bytes.Buffer
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer workflow-key" {
			t.Error("authentication missing")
		}

		if r.Method == http.MethodGet && r.URL.Path == "/gateway/v1/models" {
			modelsCalls.Add(1)
			w.Header().Set("x-typesafe-request-id", "model-discovery")
			io.WriteString(w, `{"models":[{"name":"vision-alias","description":"Available model","release_date":"2026-09-15"}]}`)

			return
		}

		if r.Method != http.MethodPost || r.URL.Path != "/gateway/v1/systemone" {
			t.Error("wrong operation")
			w.WriteHeader(404)

			return
		}

		if r.Header.Get("X-Simulate-Error") == "true" {
			w.Header().Set("x-typesafe-request-id", "workflow-denied")
			w.WriteHeader(403)
			io.WriteString(w, "plain-text denied")

			return
		}

		count := attempts.Add(1)
		if r.Header.Get("Content-Encoding") != "zstd" || r.Header.Get("TypeSafe-Danger-Zone-Images") != "true" || r.Header.Get("X-TypeSafe-Retry-Count") != fmt.Sprint(count-1) {
			t.Error("compression/media/retry metadata missing")
		}

		raw, wire := decodeCompressedRequest(t, r)
		if count == 1 {
			originalWire = wire
			originalContentType = r.Header.Get("Content-Type")
		} else if !bytes.Equal(originalWire, wire) || originalContentType != r.Header.Get("Content-Type") {
			t.Error("compressed multipart changed across attempts")
		}

		request, images, err := parseWorkflowMultipart(raw, r.Header.Get("Content-Type"))
		if err != nil {
			t.Error(err)
			w.WriteHeader(400)

			return
		}

		if request.Model != "vision-alias" || request.State.Image != "photo" || len(request.Questions) != 5 || request.Extension != "allowed" {
			t.Errorf("request=%+v", request)
		}

		if len(request.State.Nested) != 2 || request.State.Nested[0] != nil || request.State.Nested[1] != "Compare photo again" {
			t.Error("nested content changed")
		}

		for _, name := range []string{"visible", "color", "size", "location", "future"} {
			if request.Questions[name] == nil {
				t.Errorf("missing %s", name)
			}
		}

		for name, kind := range map[string]string{"visible": "noul", "color": "choice", "size": "score", "location": "box"} {
			var question struct {
				Type         string `json:"type"`
				Instructions struct {
					Image string `json:"image"`
				} `json:"instructions"`
			}

			if err := json.Unmarshal(request.Questions[name], &question); err != nil || question.Type != kind || question.Instructions.Image != "photo" {
				t.Errorf("structured instructions changed for %s", name)
			}
		}

		var noul struct {
			Criteria struct {
				True struct {
					Image string `json:"image"`
				} `json:"true"`
			} `json:"criteria"`
		}

		if err := json.Unmarshal(request.Questions["visible"], &noul); err != nil || noul.Criteria.True.Image != "photo" {
			t.Error("structured Noul criterion changed")
		}

		var scoreQuestion struct {
			Criteria []json.RawMessage `json:"criteria"`
		}

		if err := json.Unmarshal(request.Questions["size"], &scoreQuestion); err != nil || len(scoreQuestion.Criteria) != 2 {
			t.Error("structured Score criteria missing")
		} else {
			var level struct {
				Description string `json:"description"`
				Tags        []any  `json:"tags"`
			}

			if err := json.Unmarshal(scoreQuestion.Criteria[1], &level); err != nil || level.Description != "Large" || len(level.Tags) != 1 || level.Tags[0] != nil {
				t.Error("struct criterion or nested null changed")
			}
		}

		descriptor := request.Assets["photo"]
		if len(images) != 1 || len(request.Assets) != 1 || descriptor.Type != "image" || !bytes.Equal(images[descriptor.Part], source) {
			t.Error("image descriptor/upload mismatch")
		}

		if count == 1 {
			w.Header().Set("retry-after-ms", "0")
			w.WriteHeader(503)
			io.WriteString(w, "transient fixture")

			return
		}

		w.Header().Set("x-typesafe-request-id", "workflow-success")
		w.Header().Set("X-Checkpoint", "retained")
		io.WriteString(w, completeResponse)
	}))

	defer server.Close()

	transport := http.DefaultTransport.(*http.Transport).Clone()

	defer transport.CloseIdleConnections()

	shared := &http.Client{Transport: transport}

	client, err := typesafe.NewClient(typesafe.Config{APIKey: "workflow-key", BaseURL: server.URL + "/gateway", HTTPClient: shared, Logger: slog.New(slog.NewJSONHandler(&logs, nil))})
	if err != nil {
		t.Fatal(err)
	}

	models, err := client.ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if len(models.Models) != 1 || models.Models[0].Name != "vision-alias" || models.RequestID != "model-discovery" {
		t.Fatalf("models=%+v", models)
	}

	request := workflowRequest(photo, "photo", "")

	request.Questions["future"] = typesafe.RawQuestion(`{"type":"future","instructions":{"task":"Preserve future data"}}`)

	response, err := client.SystemOne(context.Background(), request, typesafe.WithModel(models.Models[0].Name), typesafe.WithExperimentalImages(true))
	if err != nil {
		t.Fatal(err)
	}

	if modelsCalls.Load() != 1 || attempts.Load() != 2 || response.Model != "vision-actual" || response.Usage.InputTokens != 285 || response.Usage.OutputTokens != 20 {
		t.Fatalf("workflow counts/model/usage: %+v", response)
	}

	visible, err := response.Noul("visible")
	if err != nil || visible.Probability != .98 {
		t.Fatalf("Noul=%+v %v", visible, err)
	}

	choice, err := response.Choice("color")
	if err != nil || choice.Choice != "red" || choice.Confidence != .7 || choice.Probabilities["red"] != .8 {
		t.Fatalf("Choice=%+v %v", choice, err)
	}

	score, err := response.Score("size")
	if err != nil || score.Score != .75 || score.Confidence != .6 || score.Legend[0] != "Small" || score.Probabilities[1] != .75 {
		t.Fatalf("Score=%+v %v", score, err)
	}

	box, err := response.Box("location")
	if err != nil || box.Confidence != nil || box.Box.Top != .2 || box.Box.Bottom != .8 || box.Box.Left != .1 || box.Box.Right != .9 {
		t.Fatalf("Box=%+v %v", box, err)
	}

	for _, kind := range []string{"noul", "choice", "score", "box", "future", "vector"} {
		if len(response.AnswersByKind(kind)) != 1 {
			t.Errorf("kind %s not retained", kind)
		}
	}

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

	if response.StatusCode != 200 || response.RequestID != "workflow-success" || response.Headers.Get("X-Checkpoint") != "retained" || string(response.RawBody) != completeResponse {
		t.Fatal("retained metadata changed after decoding")
	}

	if strings.Contains(logs.String(), "workflow-key") || strings.Contains(logs.String(), "Compare photo again") || !strings.Contains(logs.String(), "typesafe retry") {
		t.Fatal("metadata logging leaked secrets/body or omitted retry")
	}

	_, err = client.SystemOne(context.Background(), request, typesafe.WithExperimentalImages(true), typesafe.WithHeaders(http.Header{"X-Simulate-Error": {"true"}}))

	var failure *typesafe.HTTPError

	if !errors.Is(err, typesafe.ErrPermission) || !errors.As(err, &failure) || failure.RequestID != "workflow-denied" || string(failure.RawBody) != "plain-text denied" {
		t.Fatalf("workflow error=%v", err)
	}

	// The same caller-owned standard HTTP client remains usable after SDK calls.
	directRequest, _ := http.NewRequest(http.MethodGet, server.URL+"/gateway/v1/models", nil)
	directRequest.Header.Set("Authorization", "Bearer workflow-key")

	direct, err := shared.Do(directRequest)
	if err != nil {
		t.Fatal(err)
	}

	direct.Body.Close()
}

func TestConcurrentCompleteRequestsRemainIsolated(t *testing.T) {
	var requests atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.Atoi(r.Header.Get("X-Call"))
		if err != nil {
			t.Error(err)
			w.WriteHeader(400)

			return
		}

		suffix := ":" + strconv.Itoa(id)
		reference := "image-" + strconv.Itoa(id)
		expectedEncoding := []string{"zstd", "gzip", ""}[id%3]

		if r.Header.Get("Content-Encoding") != expectedEncoding || r.Header.Get("TypeSafe-Danger-Zone-Images") != "true" {
			t.Errorf("call %d settings leaked", id)
		}

		raw, _ := decodeCompressedRequest(t, r)

		request, images, err := parseWorkflowMultipart(raw, r.Header.Get("Content-Type"))
		if err != nil {
			t.Error(err)
			w.WriteHeader(400)

			return
		}

		if request.Model != "alias-"+strconv.Itoa(id) || request.State.Image != reference || len(request.Questions) != 4 || len(request.Assets) != 1 || len(images) != 1 {
			t.Errorf("call %d input leaked: %+v", id, request)
		}

		for _, name := range []string{"visible", "color", "size", "location"} {
			var question struct {
				Instructions struct {
					Image string `json:"image"`
				} `json:"instructions"`
			}

			if err := json.Unmarshal(request.Questions[name+suffix], &question); err != nil || question.Instructions.Image != reference {
				t.Errorf("call %d question %s leaked", id, name)
			}
		}

		data := images[request.Assets[reference].Part]

		decoded, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			t.Error(err)
			w.WriteHeader(400)

			return
		}

		pixel := color.RGBAModel.Convert(decoded.At(0, 0)).(color.RGBA)
		if pixel.R != uint8(id+1) {
			t.Errorf("call %d image belongs to %d", id, pixel.R)
		}

		requests.Add(1)

		answers := map[string]any{
			"visible" + suffix:  map[string]any{"type": "noul", "noul": .98},
			"color" + suffix:    map[string]any{"type": "choice", "choice": "red", "confidence": .7, "probabilities": map[string]float64{"red": .8, "blue": .2}},
			"size" + suffix:     map[string]any{"type": "score", "score": .75, "confidence": .6, "legend": map[string]any{"0": "Small", "1": map[string]any{"description": "Large", "tags": []any{nil}}}, "probabilities": map[string]float64{"0": .25, "1": .75}},
			"location" + suffix: map[string]any{"type": "box", "box": map[string]float64{"top": .2, "bottom": .8, "left": .1, "right": .9}, "confidence": nil},
		}

		w.Header().Set("x-typesafe-request-id", "concurrent-"+strconv.Itoa(id))
		json.NewEncoder(w).Encode(map[string]any{"model": "actual-" + strconv.Itoa(id), "answers": answers, "usage": map[string]int{"input_tokens": 285, "output_tokens": 20}})
	}))

	defer server.Close()

	noRetry := typesafe.RetryPolicy{}

	client, err := typesafe.NewClient(typesafe.Config{APIKey: "test", BaseURL: server.URL, Compression: typesafe.CompressionGzip, Retry: &noRetry, Logger: slog.New(slog.NewJSONHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}

	var workers sync.WaitGroup

	for id := range 12 {
		workers.Add(1)

		go func() {
			defer workers.Done()

			pixels := image.NewRGBA(image.Rect(0, 0, 2, 2))
			pixels.Set(0, 0, color.RGBA{R: uint8(id + 1), A: 255})

			photo, err := typesafe.ImageFromGoImage(pixels, "png")
			if err != nil {
				t.Error(err)

				return
			}

			suffix := ":" + strconv.Itoa(id)
			request := workflowRequest(photo, "image-"+strconv.Itoa(id), suffix)
			mode := []typesafe.Compression{typesafe.CompressionDefault, typesafe.CompressionGzip, typesafe.CompressionNone}[id%3]

			response, err := client.SystemOne(context.Background(), request, typesafe.WithModel("alias-"+strconv.Itoa(id)), typesafe.WithExperimentalImages(true), typesafe.WithCompression(mode), typesafe.WithHeaders(http.Header{"X-Call": {strconv.Itoa(id)}}))
			if err != nil {
				t.Error(err)

				return
			}

			if response.Model != "actual-"+strconv.Itoa(id) || response.RequestID != "concurrent-"+strconv.Itoa(id) {
				t.Errorf("call %d response leaked: %+v", id, response)
			}

			for _, kind := range []string{"noul", "choice", "score", "box"} {
				if len(response.AnswersByKind(kind)) != 1 {
					t.Errorf("call %d missing %s", id, kind)
				}
			}

			if _, err := response.Box("location" + suffix); err != nil {
				t.Error(err)
			}
		}()
	}

	workers.Wait()

	if requests.Load() != 12 {
		t.Fatalf("requests=%d", requests.Load())
	}
}
