package typesafe_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"

	typesafe "github.com/aadidenko/typesafe-sdk-go"
)

func pngBytes(t *testing.T) []byte {
	t.Helper()

	var b bytes.Buffer

	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.White)

	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}

	return b.Bytes()
}

type assetRequestWire struct {
	Model string `json:"model"`
	State struct {
		Nested []string `json:"nested"`
	} `json:"state"`
	Questions map[string]struct {
		Instructions struct {
			Image string `json:"image"`
		} `json:"instructions"`
		Criteria struct {
			True []string `json:"true"`
		} `json:"criteria"`
	} `json:"questions"`
	Assets map[string]struct{ Type, Part string } `json:"assets"`
}

func mustImage(t *testing.T, data []byte) typesafe.Image {
	t.Helper()

	photo, err := typesafe.ImageFromBytes(data)
	if err != nil {
		t.Fatal(err)
	}

	return photo
}

func readAssetRequest(t *testing.T, r *http.Request, destination any) (*multipart.Reader, bool) {
	t.Helper()

	media, parameters, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "multipart/form-data" {
		t.Errorf("content type %q: %v", media, err)

		return nil, false
	}

	reader := multipart.NewReader(r.Body, parameters["boundary"])

	first, err := reader.NextPart()
	if err != nil {
		t.Error(err)

		return nil, false
	}
	defer fixtureClose(t, first)

	if first.FormName() != "request" || first.Header.Get("Content-Type") != "application/json" {
		t.Error("JSON must be first request part")

		return nil, false
	}

	if !fixtureDecode(t, first, destination) {
		return nil, false
	}

	return reader, true
}

func assertNamedReferences(t *testing.T, body assetRequestWire) {
	t.Helper()

	if body.Model != "chosen" || body.Assets["photo"].Type != "image" || body.Assets["photo"].Part == "" {
		t.Errorf("bad descriptor/model: %+v", body)
	}

	question := body.Questions["visible"]
	if question.Instructions.Image != "photo" || !reflect.DeepEqual(question.Criteria.True, []string{"photo"}) {
		t.Errorf("question references changed: %+v", question)
	}

	if !reflect.DeepEqual(body.State.Nested, []string{"photo", "compare photo again"}) {
		t.Errorf("references changed: %+v", body.State)
	}
}

func assertImageUpload(t *testing.T, reader *multipart.Reader, partName string, source []byte) bool {
	t.Helper()

	part, err := reader.NextPart()
	if err != nil {
		t.Error(err)

		return false
	}
	defer fixtureClose(t, part)

	raw, ok := fixtureReadAll(t, part)
	if !ok {
		return false
	}

	if part.FormName() != partName || part.Header.Get("Content-Type") != "image/png" || !bytes.Equal(raw, source) {
		t.Error("image part differs from descriptor/source")
	}

	return true
}

func TestNamedImageMultipartReferences(t *testing.T) {
	source := pngBytes(t)
	img := mustImage(t, source)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("TypeSafe-Danger-Zone-Images") != "true" {
			t.Error("missing image opt in")
		}

		var body assetRequestWire

		reader, ok := readAssetRequest(t, r, &body)

		if !ok {
			return
		}

		assertNamedReferences(t, body)

		if !assertImageUpload(t, reader, body.Assets["photo"].Part, source) {
			return
		}

		if _, err := reader.NextPart(); !errors.Is(err, io.EOF) {
			t.Error("repeated reference must upload once")
		}

		fixtureWriteString(t, w, `{"model":"chosen","usage":{"input_tokens":1,"output_tokens":1},"answers":{"visible":{"type":"noul","noul":0.7}}}`)
	}))

	defer server.Close()

	client := mustClient(t, typesafe.Config{APIKey: "key", BaseURL: server.URL, Model: "chosen", ExperimentalImages: true})

	_, err := client.SystemOne(context.Background(), typesafe.SystemOneRequest{State: map[string]any{"nested": []any{"photo", "compare photo again"}}, Questions: typesafe.Questions{"visible": typesafe.NoulQuestion{Instructions: map[string]any{"image": "photo"}, Criteria: &typesafe.NoulCriteria{True: []any{"photo"}}}}, Assets: typesafe.Assets{"photo": img}})
	if err != nil {
		t.Fatal(err)
	}
}

func assertImageFormatCounts(t *testing.T, reader *multipart.Reader) {
	t.Helper()

	counts := make(map[string]int)

	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			t.Error(err)

			return
		}

		counts[part.Header.Get("Content-Type")]++
		fixtureClose(t, part)
	}

	if counts["image/png"] != 2 || counts["image/jpeg"] != 1 || len(counts) != 2 {
		t.Errorf("image part content types: %v", counts)
	}
}

func TestImageSourcesAndGeneratedReferences(t *testing.T) {
	fromFile, fromBase64, fromImage := imageSources(t)

	var assets typesafe.Assets

	refs := []string{assets.Add(fromFile), assets.Add(fromBase64), assets.Add(fromImage)}

	if refs[0] == "" || refs[0] == refs[1] || refs[1] == refs[2] {
		t.Fatal("generated references must be distinct")
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Assets map[string]any `json:"assets"`
		}

		reader, ok := readAssetRequest(t, r, &body)

		if !ok {
			return
		}

		if len(body.Assets) != 3 {
			t.Errorf("got %d image descriptors", len(body.Assets))
		}

		assertImageFormatCounts(t, reader)
		fixtureWriteString(t, w, `{"model":"m","usage":{"input_tokens":0,"output_tokens":0},"answers":{"q":{"type":"noul","noul":0.5}}}`)
	}))

	defer server.Close()

	client := mustClient(t, typesafe.Config{APIKey: "key", BaseURL: server.URL, Compression: typesafe.CompressionNone})

	_, err := client.SystemOne(context.Background(), typesafe.SystemOneRequest{State: refs, Assets: assets, Questions: typesafe.Questions{"q": typesafe.NoulQuestion{Instructions: "compare"}}}, typesafe.WithExperimentalImages(true))
	if err != nil {
		t.Fatal(err)
	}
}

func TestInvalidImageSourcesAndOptInFailLocally(t *testing.T) {
	for name, load := range map[string]func() (typesafe.Image, error){
		"missing file":   func() (typesafe.Image, error) { return typesafe.ImageFromFile(filepath.Join(t.TempDir(), "missing")) },
		"invalid base64": func() (typesafe.Image, error) { return typesafe.ImageFromBase64("!!!") },
		"invalid bytes":  func() (typesafe.Image, error) { return typesafe.ImageFromBytes([]byte("not an image")) },
		"empty bytes":    func() (typesafe.Image, error) { return typesafe.ImageFromBytes(nil) },
		"nil image":      func() (typesafe.Image, error) { return typesafe.ImageFromGoImage(nil, "png") },
		"empty image": func() (typesafe.Image, error) {
			return typesafe.ImageFromGoImage(image.NewRGBA(image.Rect(0, 0, 0, 0)), "png")
		},
		"unknown format": func() (typesafe.Image, error) {
			return typesafe.ImageFromGoImage(image.NewRGBA(image.Rect(0, 0, 1, 1)), "gif")
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := load()
			if !errors.Is(err, typesafe.ErrValidation) {
				t.Fatalf("expected local validation error, got %v", err)
			}
		})
	}

	source := mustImage(t, pngBytes(t))

	var traffic atomic.Int64

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		traffic.Add(1)

		w.WriteHeader(500)
	}))

	defer server.Close()

	client := mustClient(t, typesafe.Config{APIKey: "key", BaseURL: server.URL})
	for _, request := range []typesafe.SystemOneRequest{
		{State: "photo", Assets: typesafe.Assets{"photo": source}, Questions: typesafe.Questions{"q": typesafe.NoulQuestion{Instructions: "visible?"}}},
		{State: "photo", Questions: typesafe.Questions{"q": typesafe.BoxQuestion{Instructions: "locate"}}},
	} {
		_, err := client.SystemOne(context.Background(), request)
		if !errors.Is(err, typesafe.ErrValidation) {
			t.Fatalf("expected missing opt-in validation, got %v", err)
		}
	}

	for _, assets := range []typesafe.Assets{{"": source}, {"photo": {}}} {
		_, err := client.SystemOne(context.Background(), typesafe.SystemOneRequest{State: "photo", Assets: assets, Questions: typesafe.Questions{"q": typesafe.NoulQuestion{Instructions: "visible?"}}}, typesafe.WithExperimentalImages(true))
		if !errors.Is(err, typesafe.ErrValidation) {
			t.Fatalf("expected invalid asset validation, got %v", err)
		}
	}

	enabled := mustClient(t, typesafe.Config{APIKey: "key", BaseURL: server.URL, ExperimentalImages: true})
	_, err := enabled.SystemOne(context.Background(), typesafe.SystemOneRequest{State: "photo", Assets: typesafe.Assets{"photo": source}, Questions: typesafe.Questions{"q": typesafe.NoulQuestion{Instructions: "visible?"}}}, typesafe.WithExperimentalImages(false))

	if !errors.Is(err, typesafe.ErrValidation) {
		t.Fatalf("call must disable client opt in: %v", err)
	}

	if traffic.Load() != 0 {
		t.Fatalf("invalid media sent %d requests", traffic.Load())
	}
}

func TestImageSnapshotsAndDistinctNamedUploads(t *testing.T) {
	data := pngBytes(t)
	original := append([]byte(nil), data...)
	fromBytes := mustImage(t, data)
	path := filepath.Join(t.TempDir(), "image.png")

	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	fromFile, err := typesafe.ImageFromFile(path)
	if err != nil {
		t.Fatal(err)
	}

	data[0] = 0

	if err := os.WriteFile(path, []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Assets map[string]struct{ Part string } `json:"assets"`
		}

		reader, ok := readAssetRequest(t, r, &request)

		if !ok {
			return
		}

		if len(request.Assets) != 2 || request.Assets["named-a"].Part == request.Assets["named-b"].Part {
			t.Error("each named descriptor needs a distinct part")
		}

		if !assertSnapshotUploads(t, reader, original) {
			return
		}

		fixtureWriteString(t, w, `{"model":"m","usage":{"input_tokens":0,"output_tokens":0},"answers":{"q":{"type":"noul","noul":0.5}}}`)
	}))

	defer server.Close()

	client := mustClient(t, typesafe.Config{APIKey: "key", BaseURL: server.URL, ExperimentalImages: true})

	_, err = client.SystemOne(context.Background(), typesafe.SystemOneRequest{State: "named-a and named-b", Assets: typesafe.Assets{"named-a": fromBytes, "named-b": fromFile}, Questions: typesafe.Questions{"q": typesafe.NoulQuestion{Instructions: "compare"}}})
	if err != nil {
		t.Fatal(err)
	}
}

func assertSnapshotUploads(t *testing.T, reader *multipart.Reader, original []byte) bool {
	t.Helper()

	for range 2 {
		part, err := reader.NextPart()
		if err != nil {
			t.Error(err)

			return false
		}

		raw, ok := fixtureReadAll(t, part)
		fixtureClose(t, part)

		if !ok {
			return false
		}

		if !bytes.Equal(raw, original) {
			t.Error("constructor snapshot changed")
		}
	}

	return true
}

func imageSources(t *testing.T) (typesafe.Image, typesafe.Image, typesafe.Image) {
	t.Helper()

	data := pngBytes(t)
	path := filepath.Join(t.TempDir(), "image.png")

	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	fromFile, err := typesafe.ImageFromFile(path)
	if err != nil {
		t.Fatal(err)
	}

	fromBase64, err := typesafe.ImageFromBase64(" \n" + base64.StdEncoding.EncodeToString(data) + "\t")
	if err != nil {
		t.Fatal(err)
	}

	source := image.NewRGBA(image.Rect(0, 0, 2, 2))

	fromImage, err := typesafe.ImageFromGoImage(source, "jpeg")
	if err != nil {
		t.Fatal(err)
	}

	return fromFile, fromBase64, fromImage
}
