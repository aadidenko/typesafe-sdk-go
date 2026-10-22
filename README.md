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
[capability matrix](docs/capabilities.md) records this delivery slice.
Malformed successful responses are `ProtocolError` values with a field path and
original HTTP metadata. They cannot silently default required fields to zero.

Errors support `errors.Is` and `errors.As`: `ValidationError`, `ProtocolError`,
`HTTPError`, and `TransportError` distinguish input, protocol, HTTP, and network
failures. HTTP errors retain metadata for JSON, non-JSON, and empty bodies;
status-category sentinels include `ErrAuthentication`, `ErrPermission`,
`ErrRateLimit`, and `ErrServer`. Context cancellation and deadline errors remain
available through the error chain. Routine error formatting excludes API keys,
headers and application bodies. The SDK emits no logs by default.

This implementation slice delivers Noul evaluations and the transport/validation
foundation for [spec #1](https://github.com/aadidenko/typesafe-sdk-go/issues/1).
Choice, Score, Box, model discovery, images, retries,
compression, environment helpers and optional diagnostics
are assigned to dependent tickets and are not claimed as implemented here.

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
