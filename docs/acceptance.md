# Spec #1 acceptance record

This record covers the integrated Go SDK and the final consumer-workflow ticket
#10. The [capability matrix](capabilities.md) maps all **56** spec stories and
all **46** names in Python SDK v0.7.5's public export list, plus its version,
methods, constructor/call options and inherited response capabilities. Every
item has concrete Go behavior and evidence or an explicitly documented Go
redesign. The pinned OpenAPI snapshot and accepted ADRs remain normative.

## Checks actually run

Local validation used **`go version go1.25.0 darwin/arm64`**. These commands
completed successfully on the acceptance implementation:

| Command | Observed result |
|---|---|
| `GOTOOLCHAIN=go1.25.0 go test -race ./...` | Passed; library tests and all 11 example packages compiled |
| `GOTOOLCHAIN=go1.25.0 go vet ./...` | Passed |
| `CGO_ENABLED=0 GOTOOLCHAIN=go1.25.0 go build ./...` | Passed without cgo or a Python runtime |
| `GOTOOLCHAIN=go1.25.0 go test -tags=typesafe_live -run '^$' ./...` | Passed, with **no tests executed**; manual live suite compiled |
| `test -z "$(gofmt -l .)"` | Passed |
| `git diff --check` | Passed |

A source audit checked that the matrix contains exactly stories 1–56, mentions
every one of the 46 pinned Python `__all__` exports, and references 62 existing
named test functions. Python source was inspected as reference material; Python
is not an SDK or normal-test dependency.

These are local source-validation results. The exact integration commit and
remote CI outcome are recorded separately in the associated PR/issue evidence;
this file does not substitute a pre-commit SHA for the final merged identity.
The CI workflow independently exercises minimum Go 1.25.0 with race tests, vet,
format checks and cgo-disabled builds on Linux.

## Combined workflow evidence

`TestCompleteWorkflow` runs the real public SDK against a contract-derived local
HTTP fixture. It discovers a model through a gateway prefix, retains the explicit
selection for an image evaluation, and sends all four named Question types plus
a future RawQuestion. It verifies structured Go content and nested nulls, one
binary image part with a matching descriptor, default zstd, exact compressed
multipart replay after a transient response, retry metadata, typed accessors and
all-kind filtering. Application Answer and complete-response decoding retain
future data and original HTTP metadata. The same workflow exercises diagnostics,
authentication redaction, HTTP error categories, and continued use of the
caller-owned standard HTTP client.

`TestConcurrentCompleteRequestsRemainIsolated` exercises one shared client with
distinct per-call models, headers, question names, image references and image
bytes, using zstd, gzip and uncompressed multipart. Race detection passed.
Focused tests additionally cover all image sources, null Confidence, required
response fields, numeric ranges, local validation without traffic, supplied
resource ownership, cancellation, hanging/stalled bodies, retry controls and
delay hints, compression boundaries/body limits, and secret-safe diagnostics.

The [complete example](../examples/complete/main.go) combines model discovery,
explicit configuration and model selection, generated/file image Assets, all
four Questions, typed access, application decoding, model/Usage/HTTP metadata,
and errors. All examples were compiled; no provider example was invoked.

## Verification limits and manual live checks

**No live TypeSafe requests were made.** Local fixtures establish SDK behavior
against the pinned contract and controlled failures. They do not establish live
provider acceptance, image-model availability, semantic accuracy or model quality.

`TestLiveModels` and `TestLiveSystemOneNoul` are separately opt-in under the
`typesafe_live` build tag. Both also require `TYPESAFE_RUN_LIVE=1` and an API key
supplied outside the code. Ordinary tests and normal CI exclude them. Their
source was compiled with an empty test-selection expression; they were not run.
[Manual invocation instructions](../README.md#manual-provider-verification) are
available for a future explicitly requested provider check. Probabilistic outputs
have no deterministic semantic-threshold assertions.

No package release, production deployment, paid request or new server endpoint
is part of this acceptance work. Retrying an ambiguous transport failure can
repeat an evaluation, and the SDK cannot guarantee exactly-once execution.
