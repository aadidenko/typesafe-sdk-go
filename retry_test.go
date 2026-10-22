package typesafe_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"math"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	typesafe "github.com/aadidenko/typesafe-sdk-go"
)

func TestDefaultRetriesTransientHTTPAndReplaysJSON(t *testing.T) {
	var (
		attempts atomic.Int32
		original []byte
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := int(attempts.Add(1)) - 1
		if r.Header.Get("X-TypeSafe-Retry-Count") != strconv.Itoa(count) {
			t.Errorf("retry count header=%s attempt=%d", r.Header.Get("X-TypeSafe-Retry-Count"), count)
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)

			return
		}

		if count == 0 {
			original = body
		} else if !bytes.Equal(original, body) {
			t.Error("JSON changed across attempts")
		}

		if count < 2 {
			w.Header().Set("retry-after-ms", "0")
			w.WriteHeader(503)
			fixtureWrite(t, w, []byte("temporary"))

			return
		}

		fixtureWrite(t, w, []byte(validResponse))
	}))

	defer server.Close()

	client := mustClient(t, typesafe.Config{APIKey: "test", BaseURL: server.URL})

	response, err := client.SystemOne(context.Background(), noulRequest())
	if err != nil {
		t.Fatal(err)
	}

	if attempts.Load() != 3 || response.Model != "jev-actual" {
		t.Fatalf("attempts=%d response=%+v", attempts.Load(), response)
	}
}

func instantPolicy(retries int) typesafe.RetryPolicy {
	policy := typesafe.DefaultRetryPolicy()

	policy.MaxRetries = retries
	policy.BackoffInitial = 0
	policy.BackoffMax = 0
	policy.Jitter = 0

	return policy
}

func TestRetryEligibilityAndFinalHTTPMetadata(t *testing.T) {
	for _, status := range []int{400, 401, 403, 404, 408, 422, 429, 500, 503, 599} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			var attempts atomic.Int32

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				count := attempts.Add(1)
				w.Header().Set("x-typesafe-request-id", "attempt-"+strconv.Itoa(int(count)))
				w.Header().Set("X-Final", "kept")
				w.WriteHeader(status)
				fixtureWrite(t, w, []byte("non-JSON failure"))
			}))

			defer server.Close()

			policy := instantPolicy(2)
			client := mustClient(t, typesafe.Config{APIKey: "test", BaseURL: server.URL, Retry: &policy})
			_, err := client.SystemOne(context.Background(), noulRequest())

			var failure *typesafe.HTTPError

			expected := int32(1)

			if status == 408 || status == 429 || status >= 500 {
				expected = 3
			}

			if attempts.Load() != expected || !errors.As(err, &failure) {
				t.Fatalf("attempts=%d error=%v", attempts.Load(), err)
			}

			assertRetryHTTPMetadata(t, failure, status, expected)
		})
	}
}

func TestCustomAndDisabledRetryPolicies(t *testing.T) {
	cases := []struct {
		name     string
		policy   typesafe.RetryPolicy
		status   int
		expected int32
	}{
		{name: "disabled", policy: typesafe.RetryPolicy{}, status: 503, expected: 1},
		{name: "custom status", policy: typesafe.RetryPolicy{MaxRetries: 1, HTTPStatuses: []int{409}}, status: 409, expected: 2},
		{name: "custom excludes server error", policy: typesafe.RetryPolicy{MaxRetries: 1, HTTPStatuses: []int{409}}, status: 503, expected: 1},
		{name: "predicate additive", policy: typesafe.RetryPolicy{MaxRetries: 1, Predicate: func(err error) bool {
			var failure *typesafe.HTTPError

			return errors.As(err, &failure) && failure.StatusCode == 409
		}}, status: 409, expected: 2},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			var attempts atomic.Int32

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { attempts.Add(1); w.WriteHeader(test.status) }))

			defer server.Close()

			client := mustClient(t, typesafe.Config{APIKey: "test", BaseURL: server.URL})
			_, err := client.SystemOne(context.Background(), noulRequest(), typesafe.WithRetryPolicy(test.policy))

			if !errors.Is(err, typesafe.ErrHTTP) || attempts.Load() != test.expected {
				t.Fatalf("attempts=%d error=%v", attempts.Load(), err)
			}
		})
	}
}

func TestProtocolAndValidationFailuresNeverRetry(t *testing.T) {
	var (
		attempts   atomic.Int32
		predicates atomic.Int32
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { attempts.Add(1); fixtureWrite(t, w, []byte(`{}`)) }))

	defer server.Close()

	policy := instantPolicy(2)

	policy.Predicate = func(error) bool {
		predicates.Add(1)

		return true
	}

	client := mustClient(t, typesafe.Config{APIKey: "test", BaseURL: server.URL, Retry: &policy})

	if _, err := client.SystemOne(context.Background(), noulRequest()); !errors.Is(err, typesafe.ErrProtocol) {
		t.Fatal(err)
	}

	if _, err := client.SystemOne(context.Background(), typesafe.SystemOneRequest{}); !errors.Is(err, typesafe.ErrValidation) {
		t.Fatal(err)
	}

	if attempts.Load() != 1 || predicates.Load() != 0 {
		t.Fatalf("attempts=%d predicates=%d", attempts.Load(), predicates.Load())
	}
}

func TestRetryDelayHints(t *testing.T) {
	cases := []struct {
		name, ms, seconds string
		blocked           bool
	}{
		{name: "millisecond precedence", ms: "0", seconds: "3600", blocked: false},
		{name: "fractional millisecond", ms: "0.1", seconds: "", blocked: false},
		{name: "fractional seconds", ms: "", seconds: "0.0001", blocked: false},
		{name: "past HTTP date", ms: "", seconds: time.Now().Add(-time.Hour).UTC().Format(http.TimeFormat), blocked: false},
		{name: "future HTTP date", ms: "", seconds: time.Now().Add(time.Hour).UTC().Format(http.TimeFormat), blocked: true},
		{name: "invalid ms valid seconds", ms: "invalid", seconds: "3600", blocked: true},
		{name: "invalid ms valid date", ms: "NaN", seconds: time.Now().Add(time.Hour).UTC().Format(http.TimeFormat), blocked: true},
		{name: "infinite ms falls back", ms: "Inf", seconds: "0", blocked: false},
		{name: "invalid all falls back", ms: "invalid", seconds: "invalid", blocked: false},
		{name: "negative falls back", ms: "-1", seconds: "-1", blocked: false},
		{name: "nonfinite falls back", ms: "NaN", seconds: "Inf", blocked: false},
		{name: "huge finite milliseconds", ms: "1e30", seconds: "0", blocked: true},
		{name: "huge finite seconds", ms: "", seconds: "1e30", blocked: true},
		{name: "unrepresentable numeric input", ms: "1e999", seconds: "1e999", blocked: false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			var attempts atomic.Int32

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if attempts.Add(1) == 1 {
					w.Header().Set("retry-after-ms", test.ms)
					w.Header().Set("Retry-After", test.seconds)
					w.Header().Set("x-typesafe-request-id", "hint-request")
					w.WriteHeader(429)
					fixtureWrite(t, w, []byte("rate limited"))

					return
				}

				fixtureWrite(t, w, []byte(validResponse))
			}))

			defer server.Close()

			policy := instantPolicy(1)
			client := mustClient(t, typesafe.Config{APIKey: "test", BaseURL: server.URL, Retry: &policy, CallTimeout: 40 * time.Millisecond})
			_, err := client.SystemOne(context.Background(), noulRequest())

			if test.blocked {
				var failure *typesafe.HTTPError

				if attempts.Load() != 1 || !errors.Is(err, context.DeadlineExceeded) || !errors.As(err, &failure) || failure.RequestID != "hint-request" {
					t.Fatalf("attempts=%d error=%v HTTP=%+v", attempts.Load(), err, failure)
				}
			} else if err != nil || attempts.Load() != 2 {
				t.Fatalf("attempts=%d error=%v", attempts.Load(), err)
			}
		})
	}
}

func TestCancellationDuringRetryWaitPreventsMoreAttempts(t *testing.T) {
	var attempts atomic.Int32

	waiting := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.Header().Set("Retry-After", "3600")
		w.WriteHeader(409)
	}))

	defer server.Close()

	policy := instantPolicy(2)

	policy.HTTPStatuses = nil

	policy.Predicate = func(error) bool {
		close(waiting)

		return true
	}

	client := mustClient(t, typesafe.Config{APIKey: "test", BaseURL: server.URL, Retry: &policy})
	ctx, cancel := context.WithCancel(context.Background())

	defer cancel()

	done := make(chan error, 1)

	go func() { _, err := client.SystemOne(ctx, noulRequest()); done <- err }()

	awaitCallReady(t, waiting, done, cancel)
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) || attempts.Load() != 1 {
			t.Fatalf("attempts=%d error=%v", attempts.Load(), err)
		}
	case <-time.After(time.Second):
		t.Fatal("retry wait ignored cancellation")
	}
}

func TestAttemptTimeoutCanRetryWhileBudgetSurvives(t *testing.T) {
	for _, stallBody := range []bool{false, true} {
		t.Run(strconv.FormatBool(stallBody), func(t *testing.T) {
			var attempts atomic.Int32

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				serveAttemptRecovery(t, w, r, &attempts, stallBody)
			}))

			defer server.Close()

			policy := instantPolicy(1)
			client := mustClient(t, typesafe.Config{APIKey: "test", BaseURL: server.URL, Retry: &policy, AttemptTimeout: 25 * time.Millisecond, CallTimeout: time.Second})

			if _, err := client.SystemOne(context.Background(), noulRequest()); err != nil || attempts.Load() != 2 {
				t.Fatalf("attempts=%d error=%v", attempts.Load(), err)
			}
		})
	}
}

func TestAttemptTimeoutControlAndTotalBudget(t *testing.T) {
	for _, retryTimeouts := range []bool{false, true} {
		t.Run(strconv.FormatBool(retryTimeouts), func(t *testing.T) {
			var attempts atomic.Int32

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !fixtureDrain(t, r.Body) {
					return
				}

				attempts.Add(1)
				w.Header().Set("x-typesafe-request-id", "partial")
				w.WriteHeader(200)

				if !fixtureWrite(t, w, []byte(`{"model":`)) {
					return
				}

				if !fixtureFlush(t, w) {
					return
				}

				<-r.Context().Done()
			}))

			defer server.Close()

			policy := instantPolicy(100)

			policy.RetryTimeouts = retryTimeouts

			client := mustClient(t, typesafe.Config{APIKey: "test", BaseURL: server.URL, Retry: &policy, AttemptTimeout: 100 * time.Millisecond, CallTimeout: 250 * time.Millisecond})
			started := time.Now()
			_, err := client.SystemOne(context.Background(), noulRequest())

			assertTimeoutBudget(t, err, attempts.Load(), retryTimeouts, time.Since(started))
		})
	}
}

func TestUnavailableConnectionRetryControls(t *testing.T) {
	listener, err := new(net.ListenConfig).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	address := listener.Addr().String()
	fixtureClose(t, listener)

	for _, enabled := range []bool{false, true} {
		t.Run(strconv.FormatBool(enabled), func(t *testing.T) {
			var connects atomic.Int32

			policy := instantPolicy(2)

			policy.RetryConnectionErrors = enabled

			client := mustClient(t, typesafe.Config{APIKey: "test", BaseURL: "http://" + address, Retry: &policy})
			ctx := httptrace.WithClientTrace(context.Background(), &httptrace.ClientTrace{ConnectStart: func(_, _ string) { connects.Add(1) }})
			_, err := client.SystemOne(ctx, noulRequest())
			expected := int32(1)

			if enabled {
				expected = 3
			}

			if !errors.Is(err, typesafe.ErrTransport) || connects.Load() != expected {
				t.Fatalf("connects=%d error=%v", connects.Load(), err)
			}
		})
	}
}

func TestClosedConnectionCanRecover(t *testing.T) {
	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !fixtureDrain(t, r.Body) {
			return
		}

		if attempts.Add(1) == 1 {
			connection, _, err := http.NewResponseController(w).Hijack()
			if err != nil {
				t.Error(err)

				return
			}

			fixtureClose(t, connection)

			return
		}

		fixtureWrite(t, w, []byte(validResponse))
	}))

	defer server.Close()

	policy := instantPolicy(1)
	client := mustClient(t, typesafe.Config{APIKey: "test", BaseURL: server.URL, Retry: &policy})

	if _, err := client.SystemOne(context.Background(), noulRequest()); err != nil || attempts.Load() != 2 {
		t.Fatalf("attempts=%d error=%v", attempts.Load(), err)
	}
}

func TestMultipartRetriesReplayPreparedFileSnapshot(t *testing.T) {
	originalImage := pngBytes(t)
	path, asset := prepareReplayImage(t, originalImage)

	var (
		originalBody        []byte
		originalContentType string
		attempts            atomic.Int32
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)

			return
		}

		if attempts.Add(1) == 1 {
			originalBody = body
			originalContentType = r.Header.Get("Content-Type")

			w.WriteHeader(http.StatusServiceUnavailable)

			// Keep the failure buffered until mutation completes, before the retry can start.
			if err := os.WriteFile(path, []byte("modified and no longer an image"), 0o600); err != nil {
				t.Error(err)

				return
			}

			return
		}

		if !bytes.Equal(body, originalBody) || r.Header.Get("Content-Type") != originalContentType {
			t.Error("multipart representation changed across retries")
		}

		if !assertReplayedMultipartImage(t, body, originalContentType, originalImage) {
			return
		}

		fixtureWrite(t, w, []byte(validResponse))
	}))

	defer server.Close()

	policy := instantPolicy(1)
	client := mustClient(t, typesafe.Config{APIKey: "test", BaseURL: server.URL, Retry: &policy, ExperimentalImages: true})
	request := noulRequest()

	request.State = "Look at photo"
	request.Assets = typesafe.Assets{"photo": asset}

	if _, err := client.SystemOne(context.Background(), request); err != nil || attempts.Load() != 2 {
		t.Fatalf("attempts=%d error=%v", attempts.Load(), err)
	}
}

func TestRetryPolicyValidation(t *testing.T) {
	for _, policy := range []typesafe.RetryPolicy{
		{MaxRetries: -1}, {BackoffInitial: -1}, {BackoffMax: -1}, {Jitter: -.1}, {Jitter: 1.1}, {Jitter: math.NaN()}, {Jitter: math.Inf(1)}, {HTTPStatuses: []int{99}}, {HTTPStatuses: []int{600}},
	} {
		if _, err := typesafe.NewClient(typesafe.Config{APIKey: "test", Retry: &policy}); !errors.Is(err, typesafe.ErrValidation) {
			t.Fatalf("invalid policy accepted: %v", err)
		}

		client := mustClient(t, typesafe.Config{APIKey: "test"})
		if _, err := client.SystemOne(context.Background(), noulRequest(), typesafe.WithRetryPolicy(policy)); !errors.Is(err, typesafe.ErrValidation) {
			t.Fatalf("invalid per-call policy accepted: %v", err)
		}
	}
}

func TestHugeBackoffRemainsBoundedByCallDeadline(t *testing.T) {
	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { attempts.Add(1); w.WriteHeader(503) }))

	defer server.Close()

	policy := instantPolicy(1)

	policy.BackoffInitial = time.Duration(math.MaxInt64)
	policy.BackoffMax = time.Duration(math.MaxInt64)

	client := mustClient(t, typesafe.Config{APIKey: "test", BaseURL: server.URL, Retry: &policy, CallTimeout: 25 * time.Millisecond})

	if _, err := client.SystemOne(context.Background(), noulRequest()); !errors.Is(err, context.DeadlineExceeded) || attempts.Load() != 1 {
		t.Fatalf("backoff overflowed: attempts=%d error=%v", attempts.Load(), err)
	}
}

func TestPolicySnapshotsAndConcurrentOverrides(t *testing.T) {
	var mutex sync.Mutex

	attempts := make(map[string]int)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Call")

		mutex.Lock()

		attempts[id]++

		count := attempts[id]
		mutex.Unlock()

		if count == 1 {
			w.WriteHeader(409)

			return
		}

		fixtureWrite(t, w, []byte(validResponse))
	}))

	defer server.Close()

	policy := typesafe.RetryPolicy{MaxRetries: 1, HTTPStatuses: []int{409}}
	option := typesafe.WithRetryPolicy(policy)
	client := mustClient(t, typesafe.Config{APIKey: "test", BaseURL: server.URL, Retry: &policy})

	policy.HTTPStatuses[0] = 400
	policy.MaxRetries = 0

	if _, err := client.SystemOne(context.Background(), noulRequest(), typesafe.WithHeaders(http.Header{"X-Call": {"client-copy"}})); err != nil {
		t.Fatalf("client policy changed: %v", err)
	}

	if _, err := client.SystemOne(context.Background(), noulRequest(), typesafe.WithHeaders(http.Header{"X-Call": {"option-copy"}}), option); err != nil {
		t.Fatalf("option policy changed: %v", err)
	}

	var group sync.WaitGroup

	for i := range 12 {
		group.Add(1)

		go func() {
			defer group.Done()

			exerciseRetryOverride(t, client, i)
		}()
	}

	group.Wait()
	mutex.Lock()
	defer mutex.Unlock()

	for i := range 12 {
		expected := 1
		if i%2 == 0 {
			expected = 2
		}

		if attempts[strconv.Itoa(i)] != expected {
			t.Errorf("call %d attempts=%d", i, attempts[strconv.Itoa(i)])
		}
	}
}

func TestServerHintsCanBeDisabledAndBackoffIsCancelable(t *testing.T) {
	for _, respectHint := range []bool{false, true} {
		t.Run(strconv.FormatBool(respectHint), func(t *testing.T) {
			var attempts atomic.Int32

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if attempts.Add(1) == 1 {
					w.Header().Set("Retry-After", "3600")
					w.WriteHeader(503)

					return
				}

				fixtureWrite(t, w, []byte(validResponse))
			}))

			defer server.Close()

			policy := instantPolicy(1)

			policy.RespectRetryAfter = respectHint

			client := mustClient(t, typesafe.Config{APIKey: "test", BaseURL: server.URL, Retry: &policy, CallTimeout: 40 * time.Millisecond})
			_, err := client.SystemOne(context.Background(), noulRequest())

			assertHintOutcome(t, err, attempts.Load(), respectHint)
		})
	}

	t.Run("default backoff", func(t *testing.T) {
		var attempts atomic.Int32

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { attempts.Add(1); w.WriteHeader(503) }))

		defer server.Close()

		client := mustClient(t, typesafe.Config{APIKey: "test", BaseURL: server.URL, CallTimeout: 40 * time.Millisecond})
		if _, err := client.SystemOne(context.Background(), noulRequest()); !errors.Is(err, context.DeadlineExceeded) || attempts.Load() != 1 {
			t.Fatalf("default backoff ignored: attempts=%d error=%v", attempts.Load(), err)
		}
	})
}

func TestListModelsSharesRetryPolicy(t *testing.T) {
	var attempts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/v1/models" {
			t.Error("wrong model operation")
		}

		if attempts.Add(1) == 1 {
			w.WriteHeader(503)

			return
		}

		fixtureWrite(t, w, []byte(`{"models":[{"name":"jev-latest","description":"available","release_date":"2026-09-15"}]}`))
	}))

	defer server.Close()

	policy := instantPolicy(1)
	client := mustClient(t, typesafe.Config{APIKey: "test", BaseURL: server.URL, Retry: &policy})
	response, err := client.ListModels(context.Background())

	if err != nil || attempts.Load() != 2 || len(response.Models) != 1 {
		t.Fatalf("attempts=%d response=%+v error=%v", attempts.Load(), response, err)
	}
}

func assertRetryHTTPMetadata(t *testing.T, failure *typesafe.HTTPError, status int, expected int32) {
	t.Helper()

	if failure.StatusCode != status || failure.RequestID != "attempt-"+strconv.Itoa(int(expected)) || failure.Headers.Get("X-Final") != "kept" || string(failure.RawBody) != "non-JSON failure" {
		t.Fatalf("metadata=%+v", failure)
	}
}

func serveAttemptRecovery(t *testing.T, w http.ResponseWriter, r *http.Request, attempts *atomic.Int32, stallBody bool) {
	t.Helper()

	if !fixtureDrain(t, r.Body) {
		return
	}

	if attempts.Add(1) == 1 {
		if stallBody {
			w.Header().Set("x-typesafe-request-id", "stalled")
			w.WriteHeader(200)

			if !fixtureWrite(t, w, []byte(`{"model":`)) {
				return
			}

			if !fixtureFlush(t, w) {
				return
			}
		}

		<-r.Context().Done()

		return
	}

	fixtureWrite(t, w, []byte(validResponse))
}

func assertTimeoutBudget(t *testing.T, err error, attempts int32, retryTimeouts bool, elapsed time.Duration) {
	t.Helper()

	var transport *typesafe.TransportError

	if !errors.Is(err, context.DeadlineExceeded) || !errors.As(err, &transport) {
		t.Fatalf("error=%v metadata=%#v", err, transport)
	}

	assertTimeoutMetadata(t, transport.HTTPMetadata, retryTimeouts)

	if !retryTimeouts && attempts != 1 {
		t.Fatalf("timeout retried with disabled control: %d", attempts)
	}

	if retryTimeouts && (attempts < 2 || attempts > 5) {
		t.Fatalf("budget did not bound attempts: %d", attempts)
	}

	if elapsed > time.Second {
		t.Fatal("total call budget did not bound the stalled body")
	}
}

func assertReplayedMultipartImage(t *testing.T, body []byte, originalContentType string, originalImage []byte) bool {
	t.Helper()

	_, params, err := mime.ParseMediaType(originalContentType)
	if err != nil {
		t.Error(err)

		return false
	}

	reader := multipart.NewReader(bytes.NewReader(body), params["boundary"])
	requestPart, err := reader.NextPart()

	if err != nil || requestPart.FormName() != "request" {
		t.Error("first JSON part lost")

		return false
	}

	if err := requestPart.Close(); err != nil {
		t.Error(err)

		return false
	}

	imagePart, err := reader.NextPart()
	if err != nil {
		t.Error(err)

		return false
	}

	data, ok := fixtureReadAll(t, imagePart)
	if !ok {
		return false
	}

	if !bytes.Equal(data, originalImage) {
		t.Error("file mutation changed replayed image")
	}

	return true
}

func exerciseRetryOverride(t *testing.T, client *typesafe.Client, i int) {
	t.Helper()

	id := strconv.Itoa(i)
	perCall := typesafe.RetryPolicy{}

	if i%2 == 0 {
		perCall = typesafe.RetryPolicy{MaxRetries: 1, HTTPStatuses: []int{409}}
	}

	_, err := client.SystemOne(context.Background(), noulRequest(), typesafe.WithHeaders(http.Header{"X-Call": {id}}), typesafe.WithRetryPolicy(perCall))
	if (i%2 == 0 && err != nil) || (i%2 != 0 && !errors.Is(err, typesafe.ErrHTTP)) {
		t.Errorf("call %s error=%v", id, err)
	}
}

func assertHintOutcome(t *testing.T, err error, attempts int32, respectHint bool) {
	t.Helper()

	if respectHint {
		if !errors.Is(err, context.DeadlineExceeded) || attempts != 1 {
			t.Fatalf("attempts=%d error=%v", attempts, err)
		}
	} else if err != nil || attempts != 2 {
		t.Fatalf("attempts=%d error=%v", attempts, err)
	}
}

func prepareReplayImage(t *testing.T, originalImage []byte) (string, typesafe.Image) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "image.png")

	if err := os.WriteFile(path, originalImage, 0o600); err != nil {
		t.Fatal(err)
	}

	asset, err := typesafe.ImageFromFile(path)
	if err != nil {
		t.Fatal(err)
	}

	return path, asset
}

func assertTimeoutMetadata(t *testing.T, meta typesafe.HTTPMetadata, retryTimeouts bool) {
	t.Helper()

	// A late retry may exhaust the total budget before receiving any response.
	// Earlier attempt metadata must not be substituted for that final attempt.
	if retryTimeouts && meta.StatusCode == 0 {
		assertNoTimeoutResponse(t, meta)

		return
	}

	if meta.StatusCode != http.StatusOK || meta.RequestID != "partial" || string(meta.RawBody) != `{"model":` {
		t.Fatalf("invalid final partial response: %#v", meta)
	}
}

func assertNoTimeoutResponse(t *testing.T, meta typesafe.HTTPMetadata) {
	t.Helper()

	if meta.RequestID != "" || len(meta.Headers) != 0 || len(meta.RawBody) != 0 {
		t.Fatalf("response-less final attempt retained earlier response data: %#v", meta)
	}
}
