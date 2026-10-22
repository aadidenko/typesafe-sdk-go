package typesafe_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
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

func TestNamedImageMultipartReferences(t *testing.T) {
	source := pngBytes(t)
	img, err := typesafe.ImageFromBytes(source)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("TypeSafe-Danger-Zone-Images") != "true" {
			t.Error("missing image opt in")
		}
		media, p, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || media != "multipart/form-data" {
			t.Errorf("content type %q: %v", media, err)
			return
		}
		mr := multipart.NewReader(r.Body, p["boundary"])
		first, err := mr.NextPart()
		if err != nil {
			t.Error(err)
			return
		}
		if first.FormName() != "request" || first.Header.Get("Content-Type") != "application/json" {
			t.Error("JSON must be first request part")
		}
		var body struct {
			Model     string                                 `json:"model"`
			State     any                                    `json:"state"`
			Questions map[string]any                         `json:"questions"`
			Assets    map[string]struct{ Type, Part string } `json:"assets"`
		}
		if err := json.NewDecoder(first).Decode(&body); err != nil {
			t.Error(err)
		}
		if body.Model != "chosen" || body.Assets["photo"].Type != "image" || body.Assets["photo"].Part == "" {
			t.Errorf("bad descriptor/model: %+v", body)
		}
		question := body.Questions["visible"].(map[string]any)
		if question["instructions"].(map[string]any)["image"] != "photo" || question["criteria"].(map[string]any)["true"].([]any)[0] != "photo" {
			t.Errorf("question references changed: %v", question)
		}
		nested := body.State.(map[string]any)["nested"].([]any)
		if nested[0] != "photo" || nested[1] != "compare photo again" {
			t.Errorf("references changed: %v", nested)
		}
		part, err := mr.NextPart()
		if err != nil {
			t.Error(err)
			return
		}
		raw, _ := io.ReadAll(part)
		if part.FormName() != body.Assets["photo"].Part || part.Header.Get("Content-Type") != "image/png" || !bytes.Equal(raw, source) {
			t.Error("image part differs from descriptor/source")
		}
		if _, err := mr.NextPart(); err != io.EOF {
			t.Error("repeated reference must upload once")
		}
		io.WriteString(w, `{"model":"chosen","usage":{"input_tokens":1,"output_tokens":1},"answers":{"visible":{"type":"noul","noul":0.7}}}`)
	}))
	defer server.Close()
	client, err := typesafe.NewClient(typesafe.Config{APIKey: "key", BaseURL: server.URL, Model: "chosen", ExperimentalImages: true})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.SystemOne(context.Background(), typesafe.SystemOneRequest{State: map[string]any{"nested": []any{"photo", "compare photo again"}}, Questions: typesafe.Questions{"visible": typesafe.NoulQuestion{Instructions: map[string]any{"image": "photo"}, Criteria: &typesafe.NoulCriteria{True: []any{"photo"}}}}, Assets: typesafe.Assets{"photo": img}})
	if err != nil {
		t.Fatal(err)
	}
}

func TestImageSourcesAndGeneratedReferences(t *testing.T) {
	data := pngBytes(t)
	path := filepath.Join(t.TempDir(), "image.png")
	if err := os.WriteFile(path, data, 0600); err != nil {
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
	var assets typesafe.Assets
	refs := []string{assets.Add(fromFile), assets.Add(fromBase64), assets.Add(fromImage)}
	if refs[0] == "" || refs[0] == refs[1] || refs[1] == refs[2] {
		t.Fatal("generated references must be distinct")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Error(err)
			return
		}
		if len(r.MultipartForm.File) != 3 {
			t.Errorf("got %d image parts", len(r.MultipartForm.File))
		}
		seenPNG, seenJPEG := 0, 0
		for _, files := range r.MultipartForm.File {
			for _, f := range files {
				switch f.Header.Get("Content-Type") {
				case "image/png":
					seenPNG++
				case "image/jpeg":
					seenJPEG++
				}
			}
		}
		if seenPNG != 2 || seenJPEG != 1 {
			t.Errorf("content types PNG=%d JPEG=%d", seenPNG, seenJPEG)
		}
		io.WriteString(w, `{"model":"m","usage":{"input_tokens":0,"output_tokens":0},"answers":{"q":{"type":"noul","noul":0.5}}}`)
	}))
	defer server.Close()
	client, _ := typesafe.NewClient(typesafe.Config{APIKey: "key", BaseURL: server.URL, Compression: typesafe.CompressionNone})
	_, err = client.SystemOne(context.Background(), typesafe.SystemOneRequest{State: refs, Assets: assets, Questions: typesafe.Questions{"q": typesafe.NoulQuestion{Instructions: "compare"}}}, typesafe.WithExperimentalImages(true))
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
	source, _ := typesafe.ImageFromBytes(pngBytes(t))
	var traffic atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { traffic.Add(1); w.WriteHeader(500) }))
	defer server.Close()
	client, _ := typesafe.NewClient(typesafe.Config{APIKey: "key", BaseURL: server.URL})
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
	enabled, _ := typesafe.NewClient(typesafe.Config{APIKey: "key", BaseURL: server.URL, ExperimentalImages: true})
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
	fromBytes, _ := typesafe.ImageFromBytes(data)
	path := filepath.Join(t.TempDir(), "image.png")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	fromFile, err := typesafe.ImageFromFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data[0] = 0
	if err := os.WriteFile(path, []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		media, p, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if media != "multipart/form-data" {
			t.Error("expected multipart")
			return
		}
		mr := multipart.NewReader(r.Body, p["boundary"])
		first, _ := mr.NextPart()
		var request struct {
			Assets map[string]struct{ Part string } `json:"assets"`
		}
		if err := json.NewDecoder(first).Decode(&request); err != nil {
			t.Error(err)
		}
		if len(request.Assets) != 2 || request.Assets["named-a"].Part == request.Assets["named-b"].Part {
			t.Error("each named descriptor needs a distinct part")
		}
		for i := 0; i < 2; i++ {
			part, err := mr.NextPart()
			if err != nil {
				t.Error(err)
				return
			}
			raw, _ := io.ReadAll(part)
			if !bytes.Equal(raw, original) {
				t.Error("constructor snapshot changed")
			}
		}
		io.WriteString(w, `{"model":"m","usage":{"input_tokens":0,"output_tokens":0},"answers":{"q":{"type":"noul","noul":0.5}}}`)
	}))
	defer server.Close()
	client, _ := typesafe.NewClient(typesafe.Config{APIKey: "key", BaseURL: server.URL, ExperimentalImages: true})
	_, err = client.SystemOne(context.Background(), typesafe.SystemOneRequest{State: "named-a and named-b", Assets: typesafe.Assets{"named-a": fromBytes, "named-b": fromFile}, Questions: typesafe.Questions{"q": typesafe.NoulQuestion{Instructions: "compare"}}})
	if err != nil {
		t.Fatal(err)
	}
}
