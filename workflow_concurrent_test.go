package typesafe_test

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"net/http"
	"strconv"
	"sync/atomic"
	"testing"

	typesafe "github.com/aadidenko/typesafe-sdk-go"
)

// The twelve independent image fixtures encode their call IDs without narrowing conversions.
var workflowPixelValues = [...]uint8{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}

func serveConcurrentWorkflow(t *testing.T, w http.ResponseWriter, r *http.Request, requests *atomic.Int32) {
	t.Helper()

	id, err := strconv.Atoi(r.Header.Get("X-Call"))
	if err != nil || id < 0 || id >= len(workflowPixelValues) {
		t.Errorf("invalid concurrent call ID: %q", r.Header.Get("X-Call"))
		w.WriteHeader(http.StatusBadRequest)

		return
	}

	expectedEncoding := []string{"zstd", "gzip", ""}[id%3]
	if r.Header.Get("Content-Encoding") != expectedEncoding || r.Header.Get("TypeSafe-Danger-Zone-Images") != "true" {
		t.Errorf("call %d settings leaked", id)
	}

	raw, _, err := decodeCompressedRequest(r)
	if err != nil {
		t.Error(err)

		return
	}

	request, images, err := parseWorkflowMultipart(raw, r.Header.Get("Content-Type"))
	if err != nil {
		t.Error(err)
		w.WriteHeader(http.StatusBadRequest)

		return
	}

	if !assertConcurrentWorkflowInput(t, id, request, images) {
		w.WriteHeader(http.StatusBadRequest)

		return
	}

	requests.Add(1)
	writeConcurrentWorkflowResponse(t, w, id)
}

func assertConcurrentWorkflowInput(t *testing.T, id int, request workflowWire, images map[string][]byte) bool {
	t.Helper()

	suffix := ":" + strconv.Itoa(id)
	reference := "image-" + strconv.Itoa(id)

	if request.Model != "alias-"+strconv.Itoa(id) || request.State.Image != reference || len(request.Questions) != 4 || len(request.Assets) != 1 || len(images) != 1 {
		t.Errorf("call %d input leaked: %+v", id, request)
	}

	for name, kind := range map[string]string{"visible": "noul", "color": "choice", "size": "score", "location": "box"} {
		if !assertStructuredInstruction(t, request.Questions[name+suffix], kind, reference) {
			return false
		}
	}

	return assertConcurrentWorkflowImage(t, images[request.Assets[reference].Part], workflowPixelValues[id])
}

func assertConcurrentWorkflowImage(t *testing.T, data []byte, want uint8) bool {
	t.Helper()

	decoded, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		t.Error(err)

		return false
	}

	pixel, ok := color.RGBAModel.Convert(decoded.At(0, 0)).(color.RGBA)
	if !ok {
		t.Error("fixture color is not RGBA")

		return false
	}

	if pixel.R != want {
		t.Errorf("image pixel belongs to %d, want %d", pixel.R, want)
	}

	return true
}

func writeConcurrentWorkflowResponse(t *testing.T, w http.ResponseWriter, id int) {
	t.Helper()

	suffix := ":" + strconv.Itoa(id)
	w.Header().Set("x-typesafe-request-id", "concurrent-"+strconv.Itoa(id))

	if err := json.NewEncoder(w).Encode(map[string]any{"model": "actual-" + strconv.Itoa(id), "answers": concurrentWorkflowAnswers(suffix), "usage": map[string]int{"input_tokens": 285, "output_tokens": 20}}); err != nil {
		t.Error(err)
	}
}

func exerciseConcurrentWorkflow(t *testing.T, client *typesafe.Client, id int) {
	t.Helper()

	pixels := image.NewRGBA(image.Rect(0, 0, 2, 2))
	pixels.Set(0, 0, color.RGBA{R: workflowPixelValues[id], A: 255})

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

	assertConcurrentWorkflowResponse(t, response, id, suffix)
}

func assertConcurrentWorkflowResponse(t *testing.T, response *typesafe.SystemOneResponse, id int, suffix string) {
	t.Helper()

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
}

func concurrentWorkflowAnswers(suffix string) map[string]any {
	return map[string]any{
		"visible" + suffix:  map[string]any{"type": "noul", "noul": .98},
		"color" + suffix:    map[string]any{"type": "choice", "choice": "red", "confidence": .7, "probabilities": map[string]float64{"red": .8, "blue": .2}},
		"size" + suffix:     map[string]any{"type": "score", "score": .75, "confidence": .6, "legend": map[string]any{"0": "Small", "1": map[string]any{"description": "Large", "tags": []any{nil}}}, "probabilities": map[string]float64{"0": .25, "1": .75}},
		"location" + suffix: map[string]any{"type": "box", "box": map[string]float64{"top": .2, "bottom": .8, "left": .1, "right": .9}, "confidence": nil},
	}
}
