package typesafe

import (
	"net/http"
	"strings"
)

// copyHeaders validates user headers and normalizes map keys before applying
// per-call overrides. HTTP Header's Clone alone does not canonicalize map keys.
func copyHeaders(headers http.Header) (http.Header, error) {
	result := make(http.Header, len(headers))
	for name, values := range headers {
		if name == "" || strings.IndexFunc(name, func(r rune) bool { return r <= 32 || r >= 127 || strings.ContainsRune("()<>@,;:\\\"/[]?={}", r) }) >= 0 {
			return nil, &ValidationError{"headers", "invalid header name"}
		}

		canonical := http.CanonicalHeaderKey(name)
		if protectedHeader(canonical) {
			return nil, &ValidationError{"headers." + canonical, "is managed by the SDK; use its configuration option"}
		}

		if _, exists := result[canonical]; exists {
			return nil, &ValidationError{"headers." + canonical, "duplicate case-insensitive header name"}
		}

		for _, value := range values {
			if strings.IndexFunc(value, func(r rune) bool { return r == '\r' || r == '\n' || r == 127 || (r < 32 && r != '\t') }) >= 0 {
				return nil, &ValidationError{"headers." + canonical, "invalid header value"}
			}
		}

		result[canonical] = append([]string(nil), values...)
	}

	return result, nil
}

func protectedHeader(name string) bool {
	switch strings.ToLower(name) {
	case "authorization", "proxy-authorization", "api-key", "x-api-key", "accept", "accept-encoding", "user-agent", "x-typesafe-sdk", "x-typesafe-runtime", "x-typesafe-retry-count", "content-type", "content-encoding", "content-length", "host", "transfer-encoding", "connection", "keep-alive", "trailer", "te", "upgrade", "expect", "typesafe-danger-zone-images":
		return true
	}

	return false
}
