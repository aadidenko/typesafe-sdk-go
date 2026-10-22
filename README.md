# TypeSafe Go SDK

A Go 1.25+ client for evaluating named Questions against shared State. Module:
`github.com/aadidenko/typesafe-sdk-go`; public package: `typesafe`.

```sh
go get github.com/aadidenko/typesafe-sdk-go
```

```go
client, err := typesafe.NewClient(typesafe.Config{APIKey: "your-api-key"})
if err != nil { return err }
response, err := client.SystemOne(ctx, typesafe.SystemOneRequest{
    State: "I was charged twice for my subscription.",
    Questions: typesafe.Questions{
        "billing": typesafe.NoulQuestion{Instructions: "Is this about billing?"},
    },
})
if err != nil { return err }
billing, err := response.Noul("billing")
```

`billing.Probability` is the probability of yes or true. It is distinct from
Confidence. The [runnable quickstart](examples/quickstart/main.go) explicitly
reads `TYPESAFE_API_KEY` and makes one live evaluation when invoked:

```sh
go run ./examples/quickstart
```

State, instructions, and true/false criteria accept JSON-compatible strings,
structs, maps, and slices. Nested nulls are preserved. Scalar top-level content,
unencodable values, and nonfinite numbers produce a local `ValidationError`
before HTTP traffic.

Model discovery is available through `client.ListModels(ctx, options...)`. It
returns names, descriptions, release dates and retained HTTP metadata. The
[configuration guide](docs/configuration.md) and [runnable model example](examples/models/main.go)
cover opt-in `LoadEnv`, gateways and caller-owned HTTP clients.

Construction reads no environment variables. `Config` accepts an API key, base
URL, default model, custom headers, attempt timeout, call timeout, and standard
`*http.Client`. Defaults are `https://api.typesafe.ai`, `jev-latest`, 10 seconds
per attempt, and a hard 30-second call budget. Shorter context deadlines win;
cancellation also interrupts response-body reads. Gateway base-URL path prefixes
are preserved. The caller retains ownership of supplied HTTP clients/transports.
A client can be shared by goroutines; each call may use `WithModel`,
`WithHeaders`, or `WithTimeouts` without changing its defaults. Caller inputs
must not be mutated concurrently with a call.

Responses retain actual `Model`, token `Usage`, all named `Answers`, HTTP
`StatusCode`, cloned `Headers`, `RequestID`, and original `RawBody`. No response
close is needed. Typed accessors return errors for missing names or wrong kinds.
Unknown Answer kinds remain accessible as `UnknownAnswer` with their raw JSON.
`DecodeAnswers` and `DecodeResponse` decode original data into application structs
without consuming metadata. [Custom decoding and extensions](docs/extensions-and-decoding.md)
cover future `RawQuestion` kinds and protected `ExtraBody` fields; the
[capability matrix](docs/capabilities.md) maps all 56 spec stories and the complete pinned Python public surface.
Malformed successful responses are `ProtocolError` values with a field path and
original HTTP metadata. They cannot silently default required fields to zero.

Errors support `errors.Is` and `errors.As`: `ValidationError`, `ProtocolError`,
`HTTPError`, and `TransportError` distinguish input, protocol, HTTP, and network
failures. HTTP errors retain metadata for JSON, non-JSON, and empty bodies;
status-category sentinels include `ErrAuthentication`, `ErrPermission`,
`ErrRateLimit`, and `ErrServer`. Context cancellation and deadline errors remain
available through the error chain. Routine error formatting excludes API keys,
headers and application bodies. The SDK emits no logs by default. Supply an optional
`slog.Logger` through `Config.Logger`; `LogBodies` separately opts into bounded wire
body output. Secret headers are always redacted. See [logging](docs/logging.md) and
the [local logging/error example](examples/logging/main.go).

The SDK supports Noul, Choice, Score, and Box evaluations, explicit image Assets,
model discovery, application decoding, protected API extensions, bounded retries,
request compression, and optional diagnostics. See [Questions and typed
Answers](docs/questions.md), the [capability matrix](docs/capabilities.md), and the
[acceptance record](docs/acceptance.md).

Deterministic local HTTP tests need no credentials or paid requests:

```sh
go test -race ./...
go vet ./...
CGO_ENABLED=0 go build ./...
```

CI runs those checks and formatting on minimum Go 1.25.0. The checked-in
[OpenAPI snapshot](docs/reference/typesafe-openapi.json) defines the HTTP
contract. Local tests establish SDK behavior against that contract; they do not
establish live provider acceptance or model quality.

### Experimental images and Box

Images and Box questions require `Config.ExperimentalImages: true` or
`WithExperimentalImages(true)` on the call. This sends
`TypeSafe-Danger-Zone-Images: true` and preserves your selected model. A per-call
`WithExperimentalImages(false)` disables a client's opt-in. Calls using images
or Box without the flag fail before sending a request.

Use `ImageFromBytes`, `ImageFromFile`, `ImageFromBase64`, or
`ImageFromGoImage(img, "png")` (`"jpeg"` and `"jpg"` are also accepted) to create
an immutable PNG/JPEG source snapshot. File data is read at construction;
changing the original file or byte slice does not change an Image. Image
preparation does not resize images or correct their orientation.

```go
photo, err := typesafe.ImageFromFile("photo.png")
if err != nil { /* handle local image error */ }
var assets typesafe.Assets
ref := assets.Add(photo)
response, err := client.SystemOne(ctx, typesafe.SystemOneRequest{
    State: map[string]any{"image": ref},
    Assets: assets,
    Questions: typesafe.Questions{
        "location": typesafe.BoxQuestion{Instructions: "Locate the subject in " + ref},
    },
}, typesafe.WithExperimentalImages(true))
```

For a caller-chosen reference, use `Assets{"photo": photo}` and place `"photo"`
in your content. References remain ordinary string values, including within
longer strings or nested State, instructions, and criteria. There is no automatic
image discovery. Each registered reference uploads once, even if used many
times. Distinct registered names receive distinct image parts. Assets belong to
one request; the client does not retain an attachment context.

Image requests send a multipart body with a JSON `request` part followed by raw
PNG/JPEG parts. Calls without image Assets remain JSON requests, including Box
calls. The complete body is prepared before transport and can be replayed without
reading image sources again. `response.Box(name)` returns normalized edge
coordinates and a `*float64` Confidence: nil represents the model's explicit
null confidence; a missing confidence is a protocol error. See
[the compiling image example](examples/images/main.go).

### Retries and deadlines

Calls retry transient failures by default within a hard overall deadline. Use
`Config.Retry` or `WithRetryPolicy`; `RetryPolicy{}` performs one attempt. See
[retry policies, cancellation, and prepared-body replay](docs/retries.md).
Ambiguous network failures may repeat evaluations; the SDK cannot guarantee
exactly-once execution.

### Complete workflow and example index

The [complete example](examples/complete/main.go) discovers available models,
retains your explicit selection, creates or loads an image, evaluates all four
Question types, reads typed Answers, decodes application structures, reports
model/Usage/HTTP metadata, and handles errors. The application owns its standard
HTTP transport. It generates a red rectangle when `-image` is omitted.

Select an image-capable model supported by your account; the SDK does not infer
media capability from model names or silently change your selection. Set the API
key outside the program, then invoke explicitly:

```sh
TYPESAFE_DEFAULT_MODEL=your-image-capable-model go run ./examples/complete
# Optional existing image, gateway environment configuration, and gzip:
go run ./examples/complete -image photo.png -compression gzip
```

The second command uses your configured/default model. `-log` enables metadata
logging; `-log -log-bodies` separately enables potentially sensitive wire body
prefixes. The default workflow deadline is 30 seconds and `-deadline` changes it.

Every example compiles with normal tests. Running a provider example makes live
requests and requires an externally supplied `TYPESAFE_API_KEY`.

| Command | Purpose | Network when invoked |
|---|---|---|
| `go run ./examples/quickstart` | Noul and token Usage | Provider |
| `go run ./examples/classification` | Choice and class probabilities | Provider |
| `go run ./examples/scoring` | Fractional Score and integer rubric levels | Provider |
| `go run ./examples/images` | Image references and nullable Box Confidence | Provider |
| `go run ./examples/models` | Environment, model discovery, gateway/custom transport | Provider |
| `go run ./examples/custom-decoding` | Explicit application Answer/response structures | Provider |
| `go run ./examples/retries` | Retry policy and shorter caller deadlines | Provider |
| `go run ./examples/compression` | Client/call compression selection | Provider |
| `go run ./examples/complete` | Combined model/image/all-Question workflow | Provider |
| `go run ./examples/extensions` | Future RawQuestion/UnknownAnswer/ExtraBody | Local fixture only |
| `go run ./examples/logging` | Diagnostics, HTTP errors and request IDs | Local fixture only |

### Manual provider verification

Real-provider tests are excluded by the `typesafe_live` build tag and require a
second explicit environment opt-in. They have not been run as part of local
fixture acceptance. They check contract compatibility without asserting semantic
probability thresholds or model quality. With credentials supplied externally:

```sh
TYPESAFE_RUN_LIVE=1 go test -tags=typesafe_live -run '^TestLive' -count=1 .
```

Optional `TYPESAFE_BASE_URL` and `TYPESAFE_DEFAULT_MODEL` select your gateway and
model. Ordinary `go test ./...` and CI do not execute these tests. To verify
compilation without any provider call or credentials:

```sh
go test -tags=typesafe_live -run '^$' ./...
```
