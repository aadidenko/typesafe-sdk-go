package typesafe_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	typesafe "github.com/aadidenko/typesafe-sdk-go"
)

type compressedReplayFixture struct {
	t            *testing.T
	mode         typesafe.Compression
	withImage    bool
	path         string
	imageData    []byte
	imageSource  typesafe.Image
	state        map[string]any
	attempts     atomic.Int32
	mu           sync.Mutex
	originalWire []byte
	originalType string
}

func newCompressedReplayFixture(t *testing.T, mode typesafe.Compression, withImage bool, revision *changingJSON) *compressedReplayFixture {
	t.Helper()

	path := filepath.Join(t.TempDir(), "photo.png")
	imageData := pngBytes(t)

	if err := os.WriteFile(path, imageData, 0o600); err != nil {
		t.Fatal(err)
	}

	imageSource, err := typesafe.ImageFromFile(path)
	if err != nil {
		t.Fatal(err)
	}

	return &compressedReplayFixture{
		t: t, mode: mode, withImage: withImage,
		path: path, imageData: imageData, imageSource: imageSource,
		state: map[string]any{"description": strings.Repeat("a", 2048), "revision": revision},
	}
}

func (f *compressedReplayFixture) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()

	count := f.attempts.Add(1)
	if r.Header.Get("Content-Encoding") != string(f.mode) {
		f.t.Error("selected encoding changed")
	}

	raw, wire, err := decodeCompressedRequest(r)
	if err != nil {
		f.t.Error(err)

		return
	}

	f.observeWire(count, wire, r.Header.Get("Content-Type"))

	requestJSON, err := f.requestJSON(raw, r.Header.Get("Content-Type"))
	if err != nil {
		f.t.Error(err)

		return
	}

	if !assertOriginalReplayState(f.t, requestJSON) {
		return
	}

	if count == 1 {
		// Keep the failure buffered until source mutation completes, before replay.
		w.WriteHeader(http.StatusServiceUnavailable)

		f.state["description"] = "changed after first attempt"
		f.state["revision"] = "changed after first attempt"

		if err := os.WriteFile(f.path, []byte("changed after first attempt"), 0o600); err != nil {
			f.t.Error(err)

			return
		}

		return
	}

	writeCompressionResponse(f.t, w)
}

func (f *compressedReplayFixture) observeWire(count int32, wire []byte, contentType string) {
	f.t.Helper()

	if count == 1 {
		f.originalWire = append([]byte(nil), wire...)
		f.originalType = contentType

		return
	}

	if !bytes.Equal(f.originalWire, wire) || contentType != f.originalType {
		f.t.Error("compressed representation changed on retry")
	}
}

func (f *compressedReplayFixture) requestJSON(raw []byte, contentType string) ([]byte, error) {
	if !f.withImage {
		if contentType != "application/json" {
			return nil, errors.New("lost JSON content type")
		}

		return raw, nil
	}

	parts, err := parseMultipartFixture(raw, contentType)
	if err != nil {
		return nil, err
	}

	if err := f.validateMultipartImage(parts); err != nil {
		return nil, err
	}

	return parts.requestJSON, nil
}

func (f *compressedReplayFixture) validateMultipartImage(parts multipartFixtureBody) error {
	var request struct {
		Assets map[string]struct{ Type, Part string } `json:"assets"`
	}

	if err := json.Unmarshal(parts.requestJSON, &request); err != nil {
		return err
	}

	descriptor := request.Assets["photo"]
	image, exists := parts.images[descriptor.Part]

	if len(parts.images) != 1 || !exists || descriptor.Type != "image" || image.contentType != "image/png" || !bytes.Equal(image.data, f.imageData) {
		return errors.New("multipart image or descriptor changed")
	}

	return nil
}

func assertOriginalReplayState(t *testing.T, requestJSON []byte) bool {
	t.Helper()

	var request struct {
		State struct{ Description, Revision string }
	}

	if err := json.Unmarshal(requestJSON, &request); err != nil {
		t.Error(err)

		return false
	}

	if request.State.Description != strings.Repeat("a", 2048) || request.State.Revision != "original" {
		t.Errorf("logical snapshot changed: %+v", request.State)
	}

	return true
}

func exerciseCompressedReplay(t *testing.T, mode typesafe.Compression, withImage bool) {
	t.Helper()

	changing := &changingJSON{}
	fixture := newCompressedReplayFixture(t, mode, withImage, changing)
	server := httptest.NewServer(fixture)

	defer server.Close()

	policy := instantPolicy(1)
	client := mustClient(t, typesafe.Config{APIKey: "key", BaseURL: server.URL, Model: "m", Compression: mode, ExperimentalImages: withImage, Retry: &policy})
	request := typesafe.SystemOneRequest{State: fixture.state, Questions: typesafe.Questions{"q": typesafe.NoulQuestion{Instructions: "check"}}}

	if withImage {
		request.Assets = typesafe.Assets{"photo": fixture.imageSource}
	}

	if _, err := client.SystemOne(context.Background(), request); err != nil {
		t.Fatal(err)
	}

	if fixture.attempts.Load() != 2 || changing.calls != 1 {
		t.Errorf("attempts=%d marshals=%d", fixture.attempts.Load(), changing.calls)
	}
}
