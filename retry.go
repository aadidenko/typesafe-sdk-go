package typesafe

import (
	"context"
	"errors"
	"io"
	"math"
	"math/rand/v2"
	"net"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
)

// RetryPolicy configures retries after the initial attempt. A supplied policy is
// complete: zero/false values are explicit. Use DefaultRetryPolicy as a starting
// point, or RetryPolicy{} for a single attempt.
// Predicate adds eligibility to built-in rules, but never retries cancellation,
// expired call contexts, local validation, or protocol errors. It may run from
// concurrent calls and must be safe for concurrent use.
type RetryPolicy struct {
	MaxRetries                 int
	HTTPStatuses               []int
	RetryConnectionErrors      bool
	RetryTimeouts              bool
	BackoffInitial, BackoffMax time.Duration
	Jitter                     float64
	RespectRetryAfter          bool
	Predicate                  func(error) bool
}

// DefaultRetryPolicy returns independent defaults: two retries, transient HTTP
// statuses and network failures, 500ms–5s backoff, and 25% subtractive jitter.
func DefaultRetryPolicy() RetryPolicy {
	statuses := []int{408, 429}
	for status := 500; status <= 599; status++ {
		statuses = append(statuses, status)
	}

	return RetryPolicy{MaxRetries: 2, HTTPStatuses: statuses, RetryConnectionErrors: true, RetryTimeouts: true, BackoffInitial: 500 * time.Millisecond, BackoffMax: 5 * time.Second, Jitter: .25, RespectRetryAfter: true}
}

func copyRetryPolicy(policy RetryPolicy) (RetryPolicy, error) {
	if policy.MaxRetries < 0 {
		return RetryPolicy{}, &ValidationError{Field: "retry.max_retries", Message: "must not be negative"}
	}

	if policy.BackoffInitial < 0 || policy.BackoffMax < 0 {
		return RetryPolicy{}, &ValidationError{Field: "retry.backoff", Message: "must not be negative"}
	}

	if invalidJitter(policy.Jitter) {
		return RetryPolicy{}, &ValidationError{Field: "retry.jitter", Message: "must be finite and between zero and one"}
	}

	for _, status := range policy.HTTPStatuses {
		if status < 100 || status > 599 {
			return RetryPolicy{}, &ValidationError{Field: "retry.http_statuses", Message: "must contain HTTP status codes from 100 to 599"}
		}
	}

	policy.HTTPStatuses = append([]int(nil), policy.HTTPStatuses...)

	return policy, nil
}

// WithRetryPolicy uses an independent policy snapshot for one call.
func WithRetryPolicy(policy RetryPolicy) CallOption {
	snapshot, err := copyRetryPolicy(policy)

	return func(s *callSettings) error {
		if err != nil {
			return err
		}

		s.retry = snapshot

		return nil
	}
}

func (p RetryPolicy) retryable(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, ErrValidation) || errors.Is(err, ErrProtocol) {
		return false
	}

	eligible := false

	var (
		httpError      *HTTPError
		transportError *TransportError
	)

	if errors.As(err, &httpError) {
		eligible = slices.Contains(p.HTTPStatuses, httpError.StatusCode)
	} else if errors.As(err, &transportError) {
		eligible = p.retryableTransport(err)
	}

	return eligible || (p.Predicate != nil && p.Predicate(err))
}

func (p RetryPolicy) delay(retryNumber int, headers http.Header) time.Duration {
	if p.RespectRetryAfter {
		if delay, ok := serverRetryDelay(headers, time.Now()); ok {
			return delay
		}
	}

	delay := p.BackoffInitial
	if delay > p.BackoffMax {
		delay = p.BackoffMax
	}

	for i := 1; i < retryNumber && delay < p.BackoffMax; i++ {
		if delay >= p.BackoffMax-delay {
			delay = p.BackoffMax
		} else {
			delay *= 2
		}

		if delay == 0 {
			break
		}
	}

	//nolint:gosec // G404: jitter spreads retry scheduling; it is not a security random value.
	return delay - time.Duration(float64(delay)*p.Jitter*rand.Float64())
}

func serverRetryDelay(headers http.Header, now time.Time) (time.Duration, bool) {
	if delay, ok := numericRetryDelay(headers.Get("retry-after-ms"), time.Millisecond); ok {
		return delay, true
	}

	value := strings.TrimSpace(headers.Get("Retry-After"))
	if delay, ok := numericRetryDelay(value, time.Second); ok {
		return delay, true
	}

	if date, err := http.ParseTime(value); err == nil {
		delay := date.Sub(now)
		if delay < 0 {
			delay = 0
		}

		return delay, true
	}

	return 0, false
}

func numericRetryDelay(raw string, unit time.Duration) (time.Duration, bool) {
	value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return 0, false
	}

	nanos := value * float64(unit)
	// Saturating valid long hints avoids overflow and remains cancelable under
	// the overall call deadline. Nonfinite header inputs are invalid above.
	if nanos >= float64(math.MaxInt64) {
		return time.Duration(math.MaxInt64), true
	}

	return time.Duration(nanos), true
}

func retryContextError(ctx context.Context, meta HTTPMetadata, last error) error {
	cause := ctx.Err()
	if last != nil {
		cause = errors.Join(cause, last)
	}

	return &TransportError{HTTPMetadata: meta, Cause: cause}
}

func (c *Client) do(ctx context.Context, method, path string, body []byte, s callSettings) (meta HTTPMetadata, resultErr error) {
	started := time.Now()
	attempts := 0

	defer func() { c.logCallFailure(ctx, method, path, attempts, started, meta, resultErr) }()

	total, cancel := context.WithTimeout(ctx, s.callTimeout)

	defer cancel()

	var last error

	if total.Err() != nil {
		return meta, retryContextError(total, meta, nil)
	}

	var err error

	body, s.contentEncoding, err = compressRequest(body, s.compression)
	if err != nil {
		return meta, err
	}

	for retryNumber := 0; ; retryNumber++ {
		if total.Err() != nil {
			return meta, retryContextError(total, meta, last)
		}

		attempt, stop := context.WithTimeout(total, s.attemptTimeout)
		attempts++

		meta, last = c.doAttempt(attempt, method, path, body, s, retryNumber)

		stop()

		if last == nil {
			return meta, nil
		}

		if total.Err() != nil {
			return meta, retryContextError(total, meta, last)
		}

		if retryNumber == s.retry.MaxRetries || !s.retry.retryable(last) {
			return meta, last
		}

		delay := s.retry.delay(retryNumber+1, meta.Headers)
		c.logRetry(ctx, method, path, retryNumber+1, delay, meta, last)

		if err := waitRetry(total, delay); err != nil {
			return meta, retryContextError(total, meta, last)
		}
	}
}

func invalidJitter(jitter float64) bool {
	return math.IsNaN(jitter) || math.IsInf(jitter, 0) || jitter < 0 || jitter > 1
}

func (p RetryPolicy) retryableTransport(err error) bool {
	var network net.Error

	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &network) && network.Timeout()) {
		return p.RetryTimeouts
	}

	var operation *net.OpError

	//nolint:staticcheck // SA1019: temporary net.Error remains part of the SDK's accepted retry contract.
	return p.RetryConnectionErrors && (errors.As(err, &operation) || (network != nil && network.Temporary()) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF))
}

func waitRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
