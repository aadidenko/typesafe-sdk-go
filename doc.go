// Package typesafe provides a concurrent Go client for TypeSafe System One.
// It targets Go 1.25 or newer without Python or cgo.
//
// Construct a reusable [Client] with [NewClient] and explicit [Config].
// [LoadEnv] is an optional, separate helper; construction never reads process
// environment variables. A supplied standard HTTP client and transport remain
// caller-owned. [Client.ListModels] discovers account models; [Client.SystemOne]
// evaluates named [Questions] against shared State using a context per call.
//
// [NoulQuestion], [ChoiceQuestion], [ScoreQuestion] and [BoxQuestion] can be mixed
// in one evaluation. State, instructions and criteria use ordinary JSON-compatible
// Go strings, structs, maps and slices. Nested nulls survive serialization;
// invalid shapes and unencodable or nonfinite values fail locally. Images are
// immutable [Image] sources registered through request-scoped [Assets] and plain
// reference strings. Images and Box require explicit experimental opt-in, and
// never cause an automatic model change.
//
// Responses retain actual model information, token [Usage], all named Answers,
// HTTP status, cloned headers, request ID and original body bytes after closing
// the network body. Callers do not close SDK responses. Typed accessors return
// values and errors for missing names or mismatched kinds. DecodeAnswers and
// DecodeResponse use ordinary Go JSON semantics for caller-owned application
// structures. Required successful-response fields, numerical ranges, requested
// names and discriminators are checked; future types remain [UnknownAnswer].
//
// Calls have a hard overall budget and a per-attempt timeout. Context cancellation
// interrupts active HTTP work, body reads and retry waits. [RetryPolicy] controls
// transient failures; an ambiguous network failure can repeat an evaluation, so
// exactly-once execution is not promised. JSON and multipart bodies are prepared
// and compressed once, then replayed consistently. Default zstd applies at 1 KiB;
// gzip and no-compression modes are available through [Compression].
//
// Errors support errors.Is and errors.As through [ValidationError], [HTTPError],
// [TransportError], [ProtocolError], status sentinels and context error chains.
// [RawQuestion] and ExtraBody allow explicit API extensions without replacing
// SDK-managed request fields or headers. Diagnostics require an explicitly
// supplied slog logger. Bodies need a separate opt-in and may contain sensitive
// application content; secret headers are always redacted.
package typesafe
