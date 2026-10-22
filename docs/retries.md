# Retries, deadlines, and replay

Both `SystemOne` and `ListModels` default to two retries after the initial
attempt. HTTP 408, 429, and 500–599 responses, connection failures, and timeouts
are eligible. Validation and protocol failures are never retried. Retrying an
ambiguous network failure can execute an evaluation again; the SDK cannot
guarantee exactly-once execution.

```go
policy := typesafe.DefaultRetryPolicy()
policy.MaxRetries = 1
policy.BackoffInitial = 100 * time.Millisecond
client, err := typesafe.NewClient(typesafe.Config{
    APIKey: key,
    Retry: &policy,
    AttemptTimeout: 3 * time.Second,
    CallTimeout: 8 * time.Second,
})
```

`Config.Retry == nil` selects defaults. A supplied `RetryPolicy` is a complete
policy: its zero and false values are explicit. `RetryPolicy{}` disables retries
and performs a single attempt. Start from `DefaultRetryPolicy()` to customize
only selected defaults. `WithRetryPolicy(policy)` changes one call without
mutating client defaults; policy status slices are copied. Predicates must be
safe for concurrent use.

The policy exposes `HTTPStatuses`, `RetryConnectionErrors`, `RetryTimeouts`,
`RespectRetryAfter`, `BackoffInitial`, `BackoffMax`, and `Jitter`. `Predicate`
adds retry eligibility to the built-in rules, so it can cover custom error
categories or statuses. It cannot override cancellation, expired contexts, or
validation/protocol failures. Negative retry counts/delays, invalid status codes,
and nonfinite or out-of-range jitter values produce local validation errors.

Default backoff begins at 500 ms and doubles up to 5 seconds. Each delay uses
up to 25% subtractive jitter. A valid `retry-after-ms` header takes precedence
over `Retry-After` seconds or HTTP dates. Invalid millisecond hints fall back to
a valid seconds/date hint, then to backoff. Past dates mean zero delay. Very
large finite hints saturate safely and remain bounded by the call deadline;
nonfinite values are invalid. Set `RespectRetryAfter` false to use backoff
regardless of server hints. Explicit zero backoff values allow immediate retries.

The hard call budget defaults to 30 seconds across attempts, retry waits, HTTP
work, and response-body reads. Each attempt defaults to at most 10 seconds. The
shortest caller context or configured deadline wins. Cancellation interrupts
active requests and waits, preventing further attempts. Attempt timeouts can
retry only while the caller context and total budget remain valid. Context
cancellation and deadline identity remain available through `errors.Is`.

The SDK prepares JSON or the full multipart representation once, then replays
its identical bytes on each attempt. Files are captured by `ImageFromFile`, so
changing their paths afterward cannot change a retry. The SDK-managed
`X-TypeSafe-Retry-Count` header is zero on the initial attempt and increases
with each retry. Exhausted HTTP errors retain the final status, headers, request
ID, and body, including non-JSON or empty bodies. Transport errors after headers
retain the available HTTP metadata and partial body; deadline errors during
waits also retain the preceding error in their chain.

See the [compiling retry/deadline example](../examples/retries/main.go). Running
it requires an API key and makes a live evaluation; ordinary tests remain local
and credential-free.
