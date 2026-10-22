# Request compression

`Config.Compression` selects the client's request compression. Its zero value,
`CompressionDefault`, uses zstd. `CompressionZstd`, `CompressionGzip`, and
`CompressionNone` explicitly select zstd, gzip, or no compression. Unsupported
values fail locally. `WithCompression(mode)` overrides one call and does not
change the client. Passing `CompressionDefault` restores zstd for that call,
even when the client selects another mode.

The threshold is the complete prepared request body: bodies smaller than
1,024 bytes stay uncompressed; bodies of 1,024 bytes or larger use the selected
algorithm unless compression is disabled. This applies equally to JSON and
multipart requests, including all JSON descriptors, boundaries, and binary
parts. The SDK preserves the logical `Content-Type` and supplies one
`Content-Encoding` layer. Custom compression headers are rejected; select a
mode through configuration instead.

The body is serialized, assembled, and compressed once before the first HTTP
attempt. Retries replay identical compressed bytes with the original content
type and multipart boundary, without rereading image files or rerunning custom
JSON marshalers. Compression time counts against the transport's overall call
budget, with cancellation checked before compression and before an attempt.
CPU encoding runs synchronously and cannot be interrupted midway; an expired
budget prevents the subsequent request.

[TypeSafe's request rules](https://docs.typesafe.ai/api/errors) cap both logical
and compressed bodies at 32 MiB and allow a single compression layer. The SDK
rejects either oversize representation locally. Compression does not increase
the allowed logical request size. Zstd uses an explicit window no larger than
8 MiB and disables single-segment frames so larger bodies still advertise a
bounded window. These limits were verified against provider documentation on
2026-10-22; local contract tests establish encoding compatibility without
claiming live-provider acceptance.

Zstd uses the targeted pure-Go
[`github.com/klauspost/compress/zstd`](https://pkg.go.dev/github.com/klauspost/compress/zstd)
dependency; gzip uses Go's standard library. Neither requires cgo. Compared with
the pinned Python SDK, zstd is always available and gzip is an intentional
addition. See the [compiling compression example](../examples/compression/main.go).
