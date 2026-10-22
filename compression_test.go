package typesafe_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	typesafe "github.com/aadidenko/typesafe-sdk-go"
	"github.com/klauspost/compress/zstd"
)

func decodeCompressedRequest(t *testing.T, r *http.Request) ([]byte, []byte) {
	t.Helper()
	wire, err := io.ReadAll(r.Body)
	if err != nil {
		t.Error(err)
		return nil, nil
	}
	switch r.Header.Get("Content-Encoding") {
	case "":
		return wire, wire
	case "zstd":
		decoder, err := zstd.NewReader(bytes.NewReader(wire), zstd.WithDecoderMaxWindow(8<<20), zstd.WithDecoderConcurrency(1))
		if err != nil {
			t.Error(err)
			return nil, wire
		}
		defer decoder.Close()
		raw, err := io.ReadAll(decoder)
		if err != nil {
			t.Error(err)
		}
		return raw, wire
	case "gzip":
		decoder, err := gzip.NewReader(bytes.NewReader(wire))
		if err != nil {
			t.Error(err)
			return nil, wire
		}
		defer decoder.Close()
		raw, err := io.ReadAll(decoder)
		if err != nil {
			t.Error(err)
		}
		return raw, wire
	default:
		t.Errorf("unsupported layered encoding %q", r.Header.Get("Content-Encoding"))
		return nil, wire
	}
}

func writeCompressionResponse(w http.ResponseWriter) {
	io.WriteString(w, `{"model":"m","usage":{"input_tokens":0,"output_tokens":0},"answers":{"q":{"type":"noul","noul":0.5}}}`)
}

func TestDefaultCompressionThreshold(t *testing.T) {
	// Fixed wire-contract fixture accounts for the complete JSON envelope.
	const envelope = `{"model":"m","questions":{"q":{"instructions":"check","type":"noul"}},"state":""}`
	for _, size := range []int{1023, 1024} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			state := strings.Repeat("a", size-len(envelope))
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				want := ""
				if size == 1024 {
					want = "zstd"
				}
				if got := r.Header.Get("Content-Encoding"); got != want {
					t.Errorf("size=%d encoding=%q want %q", size, got, want)
				}
				if r.Header.Get("Content-Type") != "application/json" {
					t.Error("logical JSON content type changed")
				}
				raw, _ := decodeCompressedRequest(t, r)
				if len(raw) != size {
					t.Errorf("logical size=%d want %d", len(raw), size)
				}
				var request struct{ State string }
				if err := json.Unmarshal(raw, &request); err != nil || request.State != state {
					t.Errorf("logical JSON changed: %v", err)
				}
				writeCompressionResponse(w)
			}))
			defer server.Close()
			client, err := typesafe.NewClient(typesafe.Config{APIKey: "key", BaseURL: server.URL, Model: "m"})
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.SystemOne(context.Background(), typesafe.SystemOneRequest{State: state, Questions: typesafe.Questions{"q": typesafe.NoulQuestion{Instructions: "check"}}})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCompressionModesAndCallOverrides(t *testing.T) {
	state := strings.Repeat("payload", 300)
	for _, test := range []struct {
		name       string
		configured typesafe.Compression
		option     []typesafe.CallOption
		want       string
	}{
		{"gzip", typesafe.CompressionGzip, nil, "gzip"},
		{"none", typesafe.CompressionNone, nil, ""},
		{"zstd", typesafe.CompressionZstd, nil, "zstd"},
		{"call gzip", typesafe.CompressionNone, []typesafe.CallOption{typesafe.WithCompression(typesafe.CompressionGzip)}, "gzip"},
		{"call none", typesafe.CompressionZstd, []typesafe.CallOption{typesafe.WithCompression(typesafe.CompressionNone)}, ""},
		{"call default", typesafe.CompressionNone, []typesafe.CallOption{typesafe.WithCompression(typesafe.CompressionDefault)}, "zstd"},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if got := r.Header.Get("Content-Encoding"); got != test.want {
					t.Errorf("encoding=%q want %q", got, test.want)
				}
				raw, _ := decodeCompressedRequest(t, r)
				var request struct{ State string }
				if err := json.Unmarshal(raw, &request); err != nil || request.State != state {
					t.Errorf("logical JSON changed: %v", err)
				}
				writeCompressionResponse(w)
			}))
			defer server.Close()
			client, err := typesafe.NewClient(typesafe.Config{APIKey: "key", BaseURL: server.URL, Compression: test.configured})
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.SystemOne(context.Background(), typesafe.SystemOneRequest{State: state, Questions: typesafe.Questions{"q": typesafe.NoulQuestion{Instructions: "check"}}}, test.option...)
			if err != nil {
				t.Fatal(err)
			}
		})
	}
	for _, mode := range []typesafe.Compression{"brotli", "gzip,zstd", "GZIP", " "} {
		if _, err := typesafe.NewClient(typesafe.Config{APIKey: "key", Compression: mode}); !errors.Is(err, typesafe.ErrValidation) {
			t.Errorf("invalid mode %q: %v", mode, err)
		}
	}
	var traffic atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { traffic.Add(1); writeCompressionResponse(w) }))
	defer server.Close()
	client, _ := typesafe.NewClient(typesafe.Config{APIKey: "key", BaseURL: server.URL})
	request := typesafe.SystemOneRequest{State: state, Questions: typesafe.Questions{"q": typesafe.NoulQuestion{Instructions: "check"}}}
	for _, option := range []typesafe.CallOption{typesafe.WithCompression("invalid"), typesafe.WithHeaders(http.Header{"content-encoding": {"gzip"}})} {
		_, err := client.SystemOne(context.Background(), request, option)
		if !errors.Is(err, typesafe.ErrValidation) {
			t.Errorf("invalid override must fail locally: %v", err)
		}
	}
	if traffic.Load() != 0 {
		t.Error("invalid compression overrides sent HTTP traffic")
	}
}

func TestLogicalBodyLimitIsNotExpandedByCompression(t *testing.T) {
	var traffic atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { traffic.Add(1); writeCompressionResponse(w) }))
	defer server.Close()
	for _, mode := range []typesafe.Compression{typesafe.CompressionDefault, typesafe.CompressionGzip, typesafe.CompressionNone} {
		client, _ := typesafe.NewClient(typesafe.Config{APIKey: "key", BaseURL: server.URL, Compression: mode})
		_, err := client.SystemOne(context.Background(), typesafe.SystemOneRequest{State: strings.Repeat("a", 32<<20), Questions: typesafe.Questions{"q": typesafe.NoulQuestion{Instructions: "check"}}})
		if !errors.Is(err, typesafe.ErrValidation) {
			t.Errorf("logical oversize must fail locally with mode %q: %v", mode, err)
		}
	}
	if traffic.Load() != 0 {
		t.Errorf("oversize logical bodies sent %d requests", traffic.Load())
	}
}

func TestCompressedBodyAlsoRespectsProviderLimit(t *testing.T) {
	// PNG decoders permit trailing data. Incompressible trailing bytes model a
	// near-limit binary upload without a costly image encoder fixture.
	source := make([]byte, (32<<20)-2048)
	random := rand.New(rand.NewSource(1))
	if _, err := random.Read(source); err != nil {
		t.Fatal(err)
	}
	copy(source, pngBytes(t))
	photo, err := typesafe.ImageFromBytes(source)
	if err != nil {
		t.Fatal(err)
	}
	var traffic atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		traffic.Add(1)
		raw, wire := decodeCompressedRequest(t, r)
		t.Errorf("oversize compressed request sent: logical=%d encoded=%d", len(raw), len(wire))
		writeCompressionResponse(w)
	}))
	defer server.Close()
	client, _ := typesafe.NewClient(typesafe.Config{APIKey: "key", BaseURL: server.URL, ExperimentalImages: true, Compression: typesafe.CompressionGzip})
	_, err = client.SystemOne(context.Background(), typesafe.SystemOneRequest{State: "photo", Assets: typesafe.Assets{"photo": photo}, Questions: typesafe.Questions{"q": typesafe.NoulQuestion{Instructions: "check"}}})
	if !errors.Is(err, typesafe.ErrValidation) || traffic.Load() != 0 {
		t.Fatalf("encoded oversize must fail before traffic: %v", err)
	}
}

func TestCompressedJSONAndMultipartRetriesReplayExactBytes(t *testing.T) {
	for _, mode := range []typesafe.Compression{typesafe.CompressionZstd, typesafe.CompressionGzip} {
		for _, withImage := range []bool{false, true} {
			t.Run(string(mode)+fmt.Sprint(withImage), func(t *testing.T) {
				var attempts atomic.Int32
				var mu sync.Mutex
				var original []byte
				var originalType string
				path := filepath.Join(t.TempDir(), "photo.png")
				imageData := pngBytes(t)
				if err := os.WriteFile(path, imageData, 0600); err != nil {
					t.Fatal(err)
				}
				imageSource, err := typesafe.ImageFromFile(path)
				if err != nil {
					t.Fatal(err)
				}
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					mu.Lock()
					defer mu.Unlock()
					count := attempts.Add(1)
					if r.Header.Get("Content-Encoding") != string(mode) {
						t.Error("selected encoding changed")
					}
					raw, wire := decodeCompressedRequest(t, r)
					if count == 1 {
						original = append([]byte(nil), wire...)
						originalType = r.Header.Get("Content-Type")
					} else if !bytes.Equal(original, wire) || r.Header.Get("Content-Type") != originalType {
						t.Error("compressed representation changed on retry")
					}
					requestJSON := raw
					if withImage {
						media, p, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
						if err != nil || media != "multipart/form-data" {
							t.Errorf("lost multipart type: %v", err)
							return
						}
						reader := multipart.NewReader(bytes.NewReader(raw), p["boundary"])
						first, err := reader.NextPart()
						if err != nil {
							t.Error(err)
							return
						}
						if first.FormName() != "request" || first.Header.Get("Content-Type") != "application/json" {
							t.Error("JSON part must be first")
						}
						requestJSON, _ = io.ReadAll(first)
						var request struct {
							Assets map[string]struct{ Type, Part string } `json:"assets"`
						}
						if err := json.Unmarshal(requestJSON, &request); err != nil {
							t.Error(err)
						}
						part, err := reader.NextPart()
						if err != nil {
							t.Error(err)
							return
						}
						partData, _ := io.ReadAll(part)
						if !bytes.Equal(partData, imageData) || part.FormName() != request.Assets["photo"].Part || request.Assets["photo"].Type != "image" || part.Header.Get("Content-Type") != "image/png" {
							t.Error("multipart image or descriptor changed")
						}
						if _, err := reader.NextPart(); err != io.EOF {
							t.Error("unexpected extra image part")
						}
					} else if r.Header.Get("Content-Type") != "application/json" {
						t.Error("lost JSON content type")
					}
					var request struct {
						State struct {
							Description string
							Revision    string
						}
					}
					if err := json.Unmarshal(requestJSON, &request); err != nil || request.State.Description != strings.Repeat("a", 2048) || request.State.Revision != "original" {
						t.Errorf("logical snapshot changed: %v", err)
					}
					if count == 1 {
						if err := os.WriteFile(path, []byte("changed after first attempt"), 0600); err != nil {
							t.Error(err)
						}
						w.WriteHeader(503)
						return
					}
					writeCompressionResponse(w)
				}))
				defer server.Close()
				policy := instantPolicy(1)
				client, err := typesafe.NewClient(typesafe.Config{APIKey: "key", BaseURL: server.URL, Model: "m", Compression: mode, ExperimentalImages: withImage, Retry: &policy})
				if err != nil {
					t.Fatal(err)
				}
				changing := &changingJSON{}
				request := typesafe.SystemOneRequest{State: map[string]any{"description": strings.Repeat("a", 2048), "revision": changing}, Questions: typesafe.Questions{"q": typesafe.NoulQuestion{Instructions: "check"}}}
				if withImage {
					request.Assets = typesafe.Assets{"photo": imageSource}
				}
				_, err = client.SystemOne(context.Background(), request)
				if err != nil {
					t.Fatal(err)
				}
				if attempts.Load() != 2 || changing.calls != 1 {
					t.Errorf("attempts=%d marshals=%d", attempts.Load(), changing.calls)
				}
			})
		}
	}
}

func TestMultipartCompressionThresholdUsesCompleteBody(t *testing.T) {
	source, _ := typesafe.ImageFromBytes(pngBytes(t))
	var target atomic.Int64
	var baseline atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := decodeCompressedRequest(t, r)
		want := target.Load()
		if want == 0 {
			baseline.Store(int64(len(raw)))
		} else {
			encoding := ""
			if want == 1024 {
				encoding = "zstd"
			}
			if r.Header.Get("Content-Encoding") != encoding || int64(len(raw)) != want {
				t.Errorf("complete body size=%d encoding=%q want %d/%q", len(raw), r.Header.Get("Content-Encoding"), want, encoding)
			}
		}
		writeCompressionResponse(w)
	}))
	defer server.Close()
	client, _ := typesafe.NewClient(typesafe.Config{APIKey: "key", BaseURL: server.URL, Model: "m", ExperimentalImages: true})
	request := typesafe.SystemOneRequest{State: "photo", Assets: typesafe.Assets{"photo": source}, Questions: typesafe.Questions{"q": typesafe.NoulQuestion{Instructions: "check"}}}
	if _, err := client.SystemOne(context.Background(), request, typesafe.WithCompression(typesafe.CompressionNone)); err != nil {
		t.Fatal(err)
	}
	if baseline.Load() >= 1023 {
		t.Fatalf("small multipart fixture unexpectedly large: %d", baseline.Load())
	}
	for _, size := range []int64{1023, 1024} {
		target.Store(size)
		request.State = "photo" + strings.Repeat("a", int(size-baseline.Load()))
		if _, err := client.SystemOne(context.Background(), request); err != nil {
			t.Fatal(err)
		}
	}
}

func TestZstdWindowSupportsLogicalBodiesLargerThanEightMiB(t *testing.T) {
	state := strings.Repeat("a", 9<<20)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Encoding") != "zstd" {
			t.Error("default must use zstd")
		}
		raw, wire := decodeCompressedRequest(t, r)
		var header zstd.Header
		if err := header.Decode(wire); err != nil || header.SingleSegment || header.WindowSize > 8<<20 {
			t.Errorf("unsupported provider window: %+v %v", header, err)
		}
		var request struct{ State string }
		if err := json.Unmarshal(raw, &request); err != nil || request.State != state {
			t.Errorf("large JSON changed: %v", err)
		}
		writeCompressionResponse(w)
	}))
	defer server.Close()
	client, _ := typesafe.NewClient(typesafe.Config{APIKey: "key", BaseURL: server.URL})
	if _, err := client.SystemOne(context.Background(), typesafe.SystemOneRequest{State: state, Questions: typesafe.Questions{"q": typesafe.NoulQuestion{Instructions: "check"}}}); err != nil {
		t.Fatal(err)
	}
}

func TestExactlyMaximumLogicalBodyIsAllowed(t *testing.T) {
	const envelope = `{"model":"m","questions":{"q":{"instructions":"check","type":"noul"}},"state":""}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		size, err := io.Copy(io.Discard, r.Body)
		if err != nil || size != 32<<20 {
			t.Errorf("logical maximum request size=%d: %v", size, err)
		}
		writeCompressionResponse(w)
	}))
	defer server.Close()
	client, _ := typesafe.NewClient(typesafe.Config{APIKey: "key", BaseURL: server.URL, Model: "m", Compression: typesafe.CompressionNone})
	if _, err := client.SystemOne(context.Background(), typesafe.SystemOneRequest{State: strings.Repeat("a", (32<<20)-len(envelope)), Questions: typesafe.Questions{"q": typesafe.NoulQuestion{Instructions: "check"}}}); err != nil {
		t.Fatal(err)
	}
}
