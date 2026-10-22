package typesafe

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

const maxLoggedBodyBytes = 4096

func (c *Client) logEnabled(ctx context.Context, level slog.Level) bool {
	return c.logger != nil && (c.logLevel == nil || (*c.logLevel != LogLevelOff && level >= *c.logLevel)) && c.logger.Enabled(ctx, level)
}

func redactedHeaders(headers http.Header) map[string][]string {
	result := make(map[string][]string, len(headers))
	for name, values := range headers {
		normalized := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(name, "-", ""), "_", ""))
		if strings.Contains(normalized, "authorization") || strings.Contains(normalized, "cookie") || strings.Contains(normalized, "key") || strings.Contains(normalized, "token") || strings.Contains(normalized, "secret") || strings.Contains(normalized, "credential") {
			result[name] = []string{"[REDACTED]"}
		} else {
			result[name] = append([]string(nil), values...)
		}
	}
	return result
}

func bodyLogAttributes(body []byte) []slog.Attr {
	limit := min(len(body), maxLoggedBodyBytes)
	return []slog.Attr{slog.String("body", string(body[:limit])), slog.Int("body_bytes", len(body)), slog.Bool("body_truncated", limit < len(body))}
}

func (c *Client) logHTTPRequest(ctx context.Context, method, path string, attempt int, headers http.Header, body []byte) {
	if !c.logEnabled(ctx, slog.LevelInfo) {
		return
	}
	attrs := []slog.Attr{slog.String("method", method), slog.String("path", path), slog.Int("attempt", attempt), slog.Any("headers", redactedHeaders(headers))}
	if c.logBodies && body != nil {
		attrs = append(attrs, bodyLogAttributes(body)...)
	}
	c.logger.LogAttrs(ctx, slog.LevelInfo, "typesafe request", attrs...)
}

func errorCategory(err error) string {
	switch {
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline"
	case errors.Is(err, ErrProtocol):
		return "protocol"
	case errors.Is(err, ErrHTTP):
		return "http"
	case errors.Is(err, ErrTransport):
		return "transport"
	case errors.Is(err, ErrValidation):
		return "validation"
	default:
		return "failure"
	}
}

func (c *Client) logHTTPResponse(ctx context.Context, method, path string, attempt int, started time.Time, meta HTTPMetadata, err error) {
	level := slog.LevelInfo
	if err != nil {
		level = slog.LevelError
	}
	if !c.logEnabled(ctx, level) {
		return
	}
	attrs := []slog.Attr{slog.String("method", method), slog.String("path", path), slog.Int("attempt", attempt), slog.Int("status", meta.StatusCode), slog.String("request_id", meta.RequestID), slog.Duration("duration", time.Since(started)), slog.Any("headers", redactedHeaders(meta.Headers))}
	if err != nil {
		attrs = append(attrs, slog.String("failure", errorCategory(err)))
	}
	if c.logBodies && meta.RawBody != nil {
		attrs = append(attrs, bodyLogAttributes(meta.RawBody)...)
	}
	c.logger.LogAttrs(ctx, level, "typesafe response", attrs...)
}

func (c *Client) logRetry(ctx context.Context, method, path string, retry int, delay time.Duration, meta HTTPMetadata, err error) {
	if !c.logEnabled(ctx, slog.LevelWarn) {
		return
	}
	c.logger.LogAttrs(ctx, slog.LevelWarn, "typesafe retry", slog.String("method", method), slog.String("path", path), slog.Int("retry", retry), slog.Duration("delay", delay), slog.Int("status", meta.StatusCode), slog.String("request_id", meta.RequestID), slog.String("failure", errorCategory(err)))
}

func (c *Client) logCallFailure(ctx context.Context, method, path string, attempts int, started time.Time, meta HTTPMetadata, err error) {
	if err == nil || !c.logEnabled(ctx, slog.LevelError) {
		return
	}
	c.logger.LogAttrs(ctx, slog.LevelError, "typesafe call failed", slog.String("method", method), slog.String("path", path), slog.Int("attempts", attempts), slog.Duration("duration", time.Since(started)), slog.Int("status", meta.StatusCode), slog.String("request_id", meta.RequestID), slog.String("failure", errorCategory(err)))
}

func (c *Client) logProtocolFailure(ctx context.Context, method, path string, meta HTTPMetadata, err error) {
	if !errors.Is(err, ErrProtocol) || !c.logEnabled(ctx, slog.LevelError) {
		return
	}
	// Neither error messages nor field paths are logged: both can include data
	// supplied by an upstream service or application, including credentials.
	c.logger.LogAttrs(ctx, slog.LevelError, "typesafe protocol failure", slog.String("method", method), slog.String("path", path), slog.Int("status", meta.StatusCode), slog.String("request_id", meta.RequestID), slog.String("failure", "protocol"))
}
