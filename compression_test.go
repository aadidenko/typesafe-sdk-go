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
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/klauspost/compress/zstd"

	typesafe "github.com/aadidenko/typesafe-sdk-go"
)

func decodeCompressedRequest(r *http.Request) ([]byte, []byte, error) {
	wire, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, nil, err
	}

	raw, err := decompressFixture(wire, r.Header.Get("Content-Encoding"))

	return raw, wire, err
}

func decompressFixture(wire []byte, encoding string) ([]byte, error) {
	switch encoding {
	case "":
		return wire, nil
	case "zstd":
		decoder, err := zstd.NewReader(bytes.NewReader(wire), zstd.WithDecoderMaxWindow(8<<20), zstd.WithDecoderConcurrency(1))
		if err != nil {
			return nil, err
		}
		defer decoder.Close()

		return io.ReadAll(decoder)
	case "gzip":
		return decompressGzipFixture(wire)
	default:
		return nil, fmt.Errorf("unsupported layered encoding %q", encoding)
	}
}

func decompressGzipFixture(wire []byte) ([]byte, error) {
	decoder, err := gzip.NewReader(bytes.NewReader(wire))
	if err != nil {
		return nil, err
	}

	raw, readErr := io.ReadAll(decoder)

	return raw, errors.Join(readErr, decoder.Close())
}

func writeCompressionResponse(t *testing.T, w http.ResponseWriter) {
	t.Helper()
	fixtureWriteString(t, w, `{"model":"m","usage":{"input_tokens":0,"output_tokens":0},"answers":{"q":{"type":"noul","noul":0.5}}}`)
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

				if !assertEncodedState(t, r, state, want, size) {
					return
				}

				writeCompressionResponse(t, w)
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
		{name: "gzip", configured: typesafe.CompressionGzip, option: nil, want: "gzip"},
		{name: "none", configured: typesafe.CompressionNone, option: nil, want: ""},
		{name: "zstd", configured: typesafe.CompressionZstd, option: nil, want: "zstd"},
		{name: "call gzip", configured: typesafe.CompressionNone, option: []typesafe.CallOption{typesafe.WithCompression(typesafe.CompressionGzip)}, want: "gzip"},
		{name: "call none", configured: typesafe.CompressionZstd, option: []typesafe.CallOption{typesafe.WithCompression(typesafe.CompressionNone)}, want: ""},
		{name: "call default", configured: typesafe.CompressionNone, option: []typesafe.CallOption{typesafe.WithCompression(typesafe.CompressionDefault)}, want: "zstd"},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !assertEncodedState(t, r, state, test.want, 0) {
					return
				}

				writeCompressionResponse(t, w)
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

	assertInvalidCompressionOverrides(t, state)
}

func TestLogicalBodyLimitIsNotExpandedByCompression(t *testing.T) {
	var traffic atomic.Int64

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { traffic.Add(1); writeCompressionResponse(t, w) }))

	defer server.Close()

	for _, mode := range []typesafe.Compression{typesafe.CompressionDefault, typesafe.CompressionGzip, typesafe.CompressionNone} {
		client := mustClient(t, typesafe.Config{APIKey: "key", BaseURL: server.URL, Compression: mode})
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
	//nolint:gosec // G404: deterministic incompressible fixture bytes contain no secrets.
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

		raw, wire, err := decodeCompressedRequest(r)
		if err != nil {
			t.Error(err)

			return
		}

		t.Errorf("oversize compressed request sent: logical=%d encoded=%d", len(raw), len(wire))
		writeCompressionResponse(t, w)
	}))

	defer server.Close()

	client := mustClient(t, typesafe.Config{APIKey: "key", BaseURL: server.URL, ExperimentalImages: true, Compression: typesafe.CompressionGzip})

	_, err = client.SystemOne(context.Background(), typesafe.SystemOneRequest{State: "photo", Assets: typesafe.Assets{"photo": photo}, Questions: typesafe.Questions{"q": typesafe.NoulQuestion{Instructions: "check"}}})

	if !errors.Is(err, typesafe.ErrValidation) || traffic.Load() != 0 {
		t.Fatalf("encoded oversize must fail before traffic: %v", err)
	}
}

func TestCompressedJSONAndMultipartRetriesReplayExactBytes(t *testing.T) {
	for _, mode := range []typesafe.Compression{typesafe.CompressionZstd, typesafe.CompressionGzip} {
		for _, withImage := range []bool{false, true} {
			t.Run(string(mode)+fmt.Sprint(withImage), func(t *testing.T) { exerciseCompressedReplay(t, mode, withImage) })
		}
	}
}

func TestMultipartCompressionThresholdUsesCompleteBody(t *testing.T) {
	source := mustImage(t, pngBytes(t))

	var (
		target   atomic.Int64
		baseline atomic.Int64
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !observeMultipartCompressionSize(t, r, &target, &baseline) {
			return
		}

		writeCompressionResponse(t, w)
	}))

	defer server.Close()

	client := mustClient(t, typesafe.Config{APIKey: "key", BaseURL: server.URL, Model: "m", ExperimentalImages: true})
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

		raw, wire, err := decodeCompressedRequest(r)
		if err != nil {
			t.Error(err)

			return
		}

		var header zstd.Header

		if err := header.Decode(wire); err != nil || header.SingleSegment || header.WindowSize > 8<<20 {
			t.Errorf("unsupported provider window: %+v %v", header, err)
		}

		var request struct{ State string }

		if err := json.Unmarshal(raw, &request); err != nil || request.State != state {
			t.Errorf("large JSON changed: %v", err)
		}

		writeCompressionResponse(t, w)
	}))

	defer server.Close()

	client := mustClient(t, typesafe.Config{APIKey: "key", BaseURL: server.URL})
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

		writeCompressionResponse(t, w)
	}))

	defer server.Close()

	client := mustClient(t, typesafe.Config{APIKey: "key", BaseURL: server.URL, Model: "m", Compression: typesafe.CompressionNone})
	if _, err := client.SystemOne(context.Background(), typesafe.SystemOneRequest{State: strings.Repeat("a", (32<<20)-len(envelope)), Questions: typesafe.Questions{"q": typesafe.NoulQuestion{Instructions: "check"}}}); err != nil {
		t.Fatal(err)
	}
}

func assertInvalidCompressionOverrides(t *testing.T, state string) {
	t.Helper()

	var traffic atomic.Int64

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { traffic.Add(1); writeCompressionResponse(t, w) }))

	defer server.Close()

	client := mustClient(t, typesafe.Config{APIKey: "key", BaseURL: server.URL})
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

func assertEncodedState(t *testing.T, r *http.Request, state, encoding string, size int) bool {
	t.Helper()

	if r.Header.Get("Content-Encoding") != encoding {
		t.Errorf("encoding=%q want %q", r.Header.Get("Content-Encoding"), encoding)
	}

	if r.Header.Get("Content-Type") != "application/json" {
		t.Error("logical JSON content type changed")
	}

	raw, _, err := decodeCompressedRequest(r)
	if err != nil {
		t.Error(err)

		return false
	}

	if size > 0 && len(raw) != size {
		t.Errorf("logical size=%d want %d", len(raw), size)
	}

	var request struct{ State string }

	if err := json.Unmarshal(raw, &request); err != nil {
		t.Error(err)

		return false
	}

	if request.State != state {
		t.Error("logical JSON state changed")
	}

	return true
}

func observeMultipartCompressionSize(t *testing.T, r *http.Request, target, baseline *atomic.Int64) bool {
	t.Helper()

	raw, _, err := decodeCompressedRequest(r)
	if err != nil {
		t.Error(err)

		return false
	}

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

	return true
}
