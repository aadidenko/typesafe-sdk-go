# Optional logging

Set `Config.Logger` to an explicitly supplied `*slog.Logger` to enable SDK
diagnostics. Without one the SDK is silent, including when a global logger or
`TYPESAFE_LOG_LEVEL` is configured. Construction never changes a global logger,
handler or caller-owned logging level.

```go
logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
client, err := typesafe.NewClient(typesafe.Config{
    APIKey: os.Getenv("TYPESAFE_API_KEY"),
    Logger: logger,
})
```

Request and response records contain method, operation path, one-based attempt,
status, request ID, duration and redacted headers. Retry records include the
next retry number and selected delay. HTTP, transport, timeout and cancellation
failures have a safe category; final failures include the attempts performed.
Protocol failures after HTTP success are reported separately for both
SystemOne and ListModels. Error causes and protocol field paths are deliberately
excluded from logs because arbitrary upstream or caller-provided error text can
contain credentials. Inspect returned typed errors explicitly for additional
information.

Requests and successful responses log at Info, scheduled retries at Warn, and
failures at Error. The supplied logger's handler controls output.
`Config.LogLevel` optionally adds a minimum-level filter without modifying the
logger. A loaded environment level only filters a supplied logger, and cannot
enable levels its handler already suppresses. `LogLevelOff` disables all SDK
records. The optional `LoadEnv` helper accepts debug, info, warn/warning, error
and off, with explicit configuration taking precedence.

Bodies are excluded unless `Config.LogBodies` is independently set to true.
That flag does not create a logger. Body output shares its request/response
record's level and is limited to the first 4096 **bytes** of each wire body;
records include `body_bytes` and `body_truncated`. Request bodies may therefore
be multipart, compressed or binary. The recorded prefix can end within a UTF-8
character, and the logging handler determines escaping. Response bytes are the
bytes already read by the SDK's HTTP client; logging never consumes or truncates
the retained RawBody or subsequent custom decoding.

Secret header values are always replaced with `[REDACTED]`, including
Authorization, Proxy-Authorization, cookies, API keys and names containing key,
token, secret or credential, case-insensitively and across hyphen/underscore
variants. Redaction operates on a copy of the headers. Arbitrary application
body content is **not** automatically sanitized: enabling body logging can
expose sensitive State, instructions, Assets or response data. Body logging is
an explicit application decision.

The [runnable logging example](../examples/logging/main.go) uses a local HTTP
fixture to show structured diagnostics, status categories and retained request
IDs. It needs no credentials and makes no provider calls. Returned HTTP errors
can be inspected with `errors.As(err, &httpError)` and category sentinels such as
`errors.Is(err, typesafe.ErrPermission)`; response bodies and headers remain
available on the error. Routine error formatting omits header values and bodies.
