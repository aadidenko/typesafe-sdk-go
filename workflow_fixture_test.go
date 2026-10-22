package typesafe_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
)

type completeWorkflowFixture struct {
	t                   *testing.T
	source              []byte
	modelsCalls         atomic.Int32
	attempts            atomic.Int32
	mu                  sync.Mutex
	originalWire        []byte
	originalContentType string
}

func (f *completeWorkflowFixture) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.t.Helper()

	if r.Header.Get("Authorization") != "Bearer workflow-key" {
		f.t.Error("authentication missing")
	}

	if r.Method == http.MethodGet && r.URL.Path == "/gateway/v1/models" {
		f.serveModels(w)

		return
	}

	if r.Method != http.MethodPost || r.URL.Path != "/gateway/v1/systemone" {
		f.t.Error("wrong operation")
		w.WriteHeader(http.StatusNotFound)

		return
	}

	if r.Header.Get("X-Simulate-Error") == "true" {
		f.serveDenied(w)

		return
	}

	f.serveEvaluation(w, r)
}

func (f *completeWorkflowFixture) serveModels(w http.ResponseWriter) {
	f.t.Helper()
	f.modelsCalls.Add(1)
	w.Header().Set("x-typesafe-request-id", "model-discovery")
	fixtureWriteString(f.t, w, `{"models":[{"name":"vision-alias","description":"Available model","release_date":"2026-09-15"}]}`)
}

func (f *completeWorkflowFixture) serveDenied(w http.ResponseWriter) {
	f.t.Helper()
	w.Header().Set("x-typesafe-request-id", "workflow-denied")
	w.WriteHeader(http.StatusForbidden)
	fixtureWriteString(f.t, w, "plain-text denied")
}

func (f *completeWorkflowFixture) serveEvaluation(w http.ResponseWriter, r *http.Request) {
	f.t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()

	count := f.attempts.Add(1)
	if r.Header.Get("Content-Encoding") != "zstd" || r.Header.Get("TypeSafe-Danger-Zone-Images") != "true" || r.Header.Get("X-TypeSafe-Retry-Count") != fmt.Sprint(count-1) {
		f.t.Error("compression/media/retry metadata missing")
	}

	raw, wire, err := decodeCompressedRequest(r)
	if err != nil {
		f.t.Error(err)

		return
	}

	f.observeWire(count, wire, r.Header.Get("Content-Type"))

	request, images, err := parseWorkflowMultipart(raw, r.Header.Get("Content-Type"))
	if err != nil {
		f.t.Error(err)
		w.WriteHeader(http.StatusBadRequest)

		return
	}

	if !assertCompleteWireRequest(f.t, request, images, f.source) {
		return
	}

	if count == 1 {
		w.Header().Set("retry-after-ms", "0")
		w.WriteHeader(http.StatusServiceUnavailable)
		fixtureWriteString(f.t, w, "transient fixture")

		return
	}

	w.Header().Set("x-typesafe-request-id", "workflow-success")
	w.Header().Set("X-Checkpoint", "retained")
	fixtureWriteString(f.t, w, completeResponse)
}

func (f *completeWorkflowFixture) observeWire(count int32, wire []byte, contentType string) {
	f.t.Helper()

	if count == 1 {
		f.originalWire = wire
		f.originalContentType = contentType

		return
	}

	if !bytes.Equal(f.originalWire, wire) || f.originalContentType != contentType {
		f.t.Error("compressed multipart changed across attempts")
	}
}

func assertCompleteWireRequest(t *testing.T, request workflowWire, images map[string][]byte, source []byte) bool {
	t.Helper()
	assertWorkflowEnvelope(t, request)

	if !assertWorkflowQuestions(t, request.Questions) {
		return false
	}

	descriptor := request.Assets["photo"]
	if len(images) != 1 || len(request.Assets) != 1 || descriptor.Type != "image" || !bytes.Equal(images[descriptor.Part], source) {
		t.Error("image descriptor/upload mismatch")
	}

	return true
}

func assertWorkflowEnvelope(t *testing.T, request workflowWire) {
	t.Helper()

	if request.Model != "vision-alias" || request.State.Image != "photo" || len(request.Questions) != 5 || request.Extension != "allowed" {
		t.Errorf("request=%+v", request)
	}

	if len(request.State.Nested) != 2 || request.State.Nested[0] != nil || request.State.Nested[1] != "Compare photo again" {
		t.Error("nested content changed")
	}
}

func assertWorkflowQuestions(t *testing.T, questions map[string]json.RawMessage) bool {
	t.Helper()

	for _, name := range []string{"visible", "color", "size", "location", "future"} {
		if questions[name] == nil {
			t.Errorf("missing %s", name)
		}
	}

	for name, kind := range map[string]string{"visible": "noul", "color": "choice", "size": "score", "location": "box"} {
		if !assertStructuredInstruction(t, questions[name], kind, "photo") {
			return false
		}
	}

	if !assertWorkflowNoulCriterion(t, questions["visible"]) {
		return false
	}

	return assertWorkflowScoreCriteria(t, questions["size"])
}

func assertStructuredInstruction(t *testing.T, raw json.RawMessage, kind, reference string) bool {
	t.Helper()

	var question struct {
		Type         string `json:"type"`
		Instructions struct {
			Image string `json:"image"`
		} `json:"instructions"`
	}

	if err := json.Unmarshal(raw, &question); err != nil {
		t.Error(err)

		return false
	}

	if question.Type != kind || question.Instructions.Image != reference {
		t.Errorf("structured %s instruction changed", kind)
	}

	return true
}

func assertWorkflowNoulCriterion(t *testing.T, raw json.RawMessage) bool {
	t.Helper()

	var question struct {
		Criteria struct {
			True struct {
				Image string `json:"image"`
			} `json:"true"`
		} `json:"criteria"`
	}

	if err := json.Unmarshal(raw, &question); err != nil {
		t.Error(err)

		return false
	}

	if question.Criteria.True.Image != "photo" {
		t.Error("structured Noul criterion changed")
	}

	return true
}

func assertWorkflowScoreCriteria(t *testing.T, raw json.RawMessage) bool {
	t.Helper()

	var question struct {
		Criteria []json.RawMessage `json:"criteria"`
	}

	if err := json.Unmarshal(raw, &question); err != nil || len(question.Criteria) != 2 {
		t.Error("structured Score criteria missing")

		return false
	}

	var level struct {
		Description string `json:"description"`
		Tags        []any  `json:"tags"`
	}

	if err := json.Unmarshal(question.Criteria[1], &level); err != nil || level.Description != "Large" || len(level.Tags) != 1 || level.Tags[0] != nil {
		t.Error("struct criterion or nested null changed")
	}

	return true
}
