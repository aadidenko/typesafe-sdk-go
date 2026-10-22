package typesafe

import (
	"bytes"
	"compress/gzip"

	"github.com/klauspost/compress/zstd"
)

// Compression selects request body encoding. Empty selects default zstd.
// All modes retain uncompressed bodies below 1 KiB.
type Compression string

const (
	CompressionDefault Compression = ""
	CompressionZstd    Compression = "zstd"
	CompressionGzip    Compression = "gzip"
	CompressionNone    Compression = "none"
)

func validateCompression(mode Compression) error {
	switch mode {
	case CompressionDefault, CompressionZstd, CompressionGzip, CompressionNone:
		return nil
	}

	return &ValidationError{"compression", "must be default, zstd, gzip or none"}
}

// WithCompression overrides compression for one call without changing defaults.
func WithCompression(mode Compression) CallOption {
	return func(s *callSettings) error {
		if err := validateCompression(mode); err != nil {
			return err
		}

		s.compression = mode

		return nil
	}
}

const compressionThreshold = 1024

// TypeSafe limits both the logical body and its encoded representation.
const maximumRequestBody = 32 << 20

func compressRequest(body []byte, mode Compression) ([]byte, string, error) {
	if len(body) > maximumRequestBody {
		return nil, "", &ValidationError{"request", "body must not exceed 32 MiB before compression"}
	}

	if len(body) < compressionThreshold || mode == CompressionNone {
		return body, "", nil
	}

	if mode == CompressionGzip {
		var buffer bytes.Buffer

		encoder := gzip.NewWriter(&buffer)

		if _, err := encoder.Write(body); err != nil {
			return nil, "", &ValidationError{"compression", "cannot encode gzip body"}
		}

		if err := encoder.Close(); err != nil {
			return nil, "", &ValidationError{"compression", "cannot finish gzip body"}
		}

		return checkedCompressedBody(buffer.Bytes(), "gzip")
	}

	encoder, err := zstd.NewWriter(nil, zstd.WithWindowSize(8<<20), zstd.WithSingleSegment(false), zstd.WithEncoderConcurrency(1))
	if err != nil {
		return nil, "", &ValidationError{"compression", "cannot initialize zstd encoder"}
	}
	defer encoder.Close()

	return checkedCompressedBody(encoder.EncodeAll(body, nil), "zstd")
}

func checkedCompressedBody(body []byte, encoding string) ([]byte, string, error) {
	if len(body) > maximumRequestBody {
		return nil, "", &ValidationError{"request", "compressed body must not exceed 32 MiB"}
	}

	return body, encoding, nil
}
