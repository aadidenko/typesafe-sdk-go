# Use concrete Go types and explicit answer decoding

Questions will use concrete Go structs in a named collection. Responses will offer typed answer accessors that return a value and an error, plus explicit decoding into user-defined structs. Question names and answer-kind mismatches are checked at runtime. Typed question handles and Pydantic-style inheritance or automatic field lifting would complicate the public API and are not the chosen approach.

The module path is `github.com/aadidenko/typesafe-sdk-go`, with package name `typesafe`. The client exposes `SystemOne` and `ListModels`; System One accepts a `SystemOneRequest` and optional per-call settings. Responses expose `Noul`, `Choice`, `Score`, and `Box` accessors, `DecodeAnswers`, and `DecodeResponse`. Numeric judgments use `float64`; nullable Box confidence uses `*float64`.

One client will support concurrent calls through ordinary methods accepting `context.Context`, with errors compatible with `errors.Is` and `errors.As`. The SDK targets Go 1.25 or newer, uses the standard library where practical, and permits a targeted pure-Go dependency for zstd without requiring cgo.

Construction is explicit through `NewClient(Config)`. Reading `TYPESAFE_*` environment variables is an opt-in helper, with explicit configuration taking precedence. Caller-supplied HTTP clients remain caller-owned. Logging uses an optional `slog.Logger`, is disabled by default, and requires a separate option to include bodies; secret headers are always redacted. Responses retain body bytes and HTTP metadata after closing the network response body.
