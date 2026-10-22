# Custom decoding and future API extensions

Call `response.DecodeAnswers(&destination)` to decode the original object keyed
by Question names. Call `response.DecodeResponse(&destination)` to decode the
entire original response. Full-response decoding is also available on model
listing responses and retained HTTP metadata. Both use ordinary `encoding/json`
semantics, can be repeated, and leave SDK accessors and metadata available.
Unknown Answer and response fields remain in the original bytes even when the
SDK does not model them.

```go
var app struct {
    Billing struct { Probability float64 `json:"noul"` }
}
if err := response.DecodeAnswers(&app); err != nil { return err }
```

There is no inheritance or automatic lifting of named Answers into a top-level
custom response. JSON strings do not become numbers automatically. Invalid
destinations and incompatible types return wrapped standard JSON errors,
including `*json.InvalidUnmarshalError` and `*json.UnmarshalTypeError`, accessible
with `errors.As`. Applications apply their own validation after decoding. SDK
required-field and requested-Answer validation still happens before a successful
response is returned. A decoding failure does not consume response data.

`RawQuestion` contains the complete raw JSON envelope for an unknown future
Question discriminator:

```go
question := typesafe.RawQuestion(`{"type":"future-ranking","alternatives":["a","b"]}`)
```

The envelope must be a JSON object with a nonblank string `type`. The SDK does
not invent schema rules for an unknown kind. Known `noul`, `choice`, `score` and
`box` discriminators must use their concrete SDK structs; using them in
RawQuestion is a local validation error. This preserves typed validation and
media opt-ins. A raw Question does not make an unsupported feature available on
the server: the provider must accept the future discriminator first.

A matching future Answer is retained as `UnknownAnswer` with `Type` and `Raw`.
Additional unknown Answers remain available too. Every requested name and type
must still match. A missing Answer, a different discriminator or an unknown
Answer in place of a known typed Question produces a `ProtocolError` retaining
the original HTTP response and field path.

`SystemOneRequest.ExtraBody` accepts additional top-level fields as
`map[string]any` using normal JSON values. Collisions with `model`, `state`,
`questions` and `assets` fail locally, even if the colliding value is nil. Set
core values through request fields and call options. ExtraBody never replaces
validated core data; unrelated fields are transmitted without losing nested
nulls. Unencodable values are local validation errors. Protected-header rules
also apply to extended requests. Do not mutate raw bytes, maps or their nested
values concurrently while preparing a call.

The [custom decoding example](../examples/custom-decoding/main.go) performs a
real evaluation only when explicitly run with an externally supplied API key.
The [extension example](../examples/extensions/main.go) is runnable without
credentials against a local HTTP fixture, illustrating a hypothetical future
provider feature. Fixture success does not establish real provider support.
