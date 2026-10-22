package typesafe

import (
	"errors"
	"fmt"
	"net/http"
)

var (
	// ErrValidation classifies invalid local input.
	ErrValidation = errors.New("typesafe: invalid input")
	// ErrProtocol classifies malformed successful HTTP responses.
	ErrProtocol = errors.New("typesafe: invalid response")
	// ErrTransport classifies connection, cancellation, deadline and body-read failures.
	ErrTransport = errors.New("typesafe: transport failure")
	// ErrHTTP classifies unsuccessful HTTP responses.
	ErrHTTP = errors.New("typesafe: HTTP failure")
	// ErrBadRequest classifies HTTP 400 responses.
	ErrBadRequest = errors.New("typesafe: bad request")
	// ErrAuthentication classifies HTTP 401 responses.
	ErrAuthentication = errors.New("typesafe: authentication failed")
	// ErrPermission classifies HTTP 403 responses.
	ErrPermission = errors.New("typesafe: permission denied")
	// ErrNotFound classifies HTTP 404 responses.
	ErrNotFound = errors.New("typesafe: not found")
	// ErrServerValidation classifies HTTP 422 responses.
	ErrServerValidation = errors.New("typesafe: server validation failed")
	// ErrRateLimit classifies HTTP 429 responses.
	ErrRateLimit = errors.New("typesafe: rate limited")
	// ErrServer classifies HTTP server failures.
	ErrServer = errors.New("typesafe: server failure")
	// ErrAnswerMissing classifies access to an absent named answer.
	ErrAnswerMissing = errors.New("typesafe: answer missing")
	// ErrAnswerKind classifies an answer accessor kind mismatch.
	ErrAnswerKind = errors.New("typesafe: wrong answer kind")
)

// ValidationError identifies invalid local input, without including its value.
type ValidationError struct{ Field, Message string }

func (e *ValidationError) Error() string {
	return fmt.Sprintf("typesafe: invalid %s: %s", e.Field, e.Message)
}
func (e *ValidationError) Unwrap() error { return ErrValidation }

// HTTPMetadata remains available after the response body is closed.
type HTTPMetadata struct {
	StatusCode int
	Headers    http.Header
	RequestID  string
	RawBody    []byte
}

// ProtocolError describes a malformed successful response.
type ProtocolError struct {
	HTTPMetadata
	Field, Message string
}

func (e *ProtocolError) Error() string {
	return fmt.Sprintf("typesafe: invalid response at %s: %s", e.Field, e.Message)
}
func (e *ProtocolError) Unwrap() error { return ErrProtocol }

// HTTPError retains unsuccessful HTTP responses, including non-JSON bodies.
type HTTPError struct{ HTTPMetadata }

func (e *HTTPError) Error() string { return fmt.Sprintf("typesafe: HTTP status %d", e.StatusCode) }

// Is matches the generic HTTP category and the category for this status.
func (e *HTTPError) Is(target error) bool {
	if target == ErrHTTP {
		return true
	}

	switch e.StatusCode {
	case 400:
		return target == ErrBadRequest
	case 401:
		return target == ErrAuthentication
	case 403:
		return target == ErrPermission
	case 404:
		return target == ErrNotFound
	case 422:
		return target == ErrServerValidation
	case 429:
		return target == ErrRateLimit
	default:
		return e.StatusCode >= 500 && target == ErrServer
	}
}

// TransportError wraps connection, cancellation, deadline and body-read failures.
// Its message intentionally excludes URLs, headers and application content.
type TransportError struct {
	HTTPMetadata
	Cause error
}

func (e *TransportError) Error() string { return "typesafe: transport failure" }
func (e *TransportError) Unwrap() error { return e.Cause }

// Is matches ErrTransport while Unwrap retains the original cause.
func (e *TransportError) Is(target error) bool { return target == ErrTransport }
