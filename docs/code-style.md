# Go code style and lint policy

Status: local tooling implemented; full-repository remediation and CI acceptance
are tracked by specification #12.

Specification: [strict Go quality gates and full Google/Uber style audit, #12](https://github.com/aadidenko/typesafe-sdk-go/issues/12).

## Agreed direction

- Use the [Google Go Style Guide](https://google.github.io/styleguide/go/guide)
  as the primary guide. Compatible recommendations from the
  [Uber Go Style Guide](https://github.com/uber-go/guide/blob/master/style.md)
  supplement it. Google takes precedence when the guides disagree.
- Apply a maximally strict, explicitly selected lint profile to the whole
  repository, including tests and examples. Do not use a new-code-only baseline.
- Deliver one PR from `chore/strict-go-lint`, with separate commits for
  mechanical formatting, refactoring, behavior fixes, and configuration/CI.
- Include a full manual audit of the existing SDK against both guides, not just
  a review of lint findings or the resulting diff.

## Tooling and scope

Pin golangci-lint v2.15.0 and Go 1.26.2 for the lint toolchain. Keep the SDK's
minimum Go version and its race, vet, and cgo-disabled build checks at Go 1.25.0.
Target Go 1.25 in lint configuration as well; the newer toolchain must not invite
use of APIs or syntax unavailable at the SDK minimum. Do not add the lint tool's
dependencies to the SDK module.

The selected profile consists of the following checks; do not enable all
available linters indiscriminately:

| Area | Checks |
| --- | --- |
| Formatting | gofumpt, goimports |
| Whitespace | wsl_v5, nlreturn |
| Correctness | govet, staticcheck, unused, ineffassign, nilerr, unconvert, unparam |
| Errors | errcheck, errorlint, errname, forcetypeassert |
| Resources and contexts | bodyclose, noctx, contextcheck, containedctx, fatcontext, durationcheck |
| Style | revive, gocritic, predeclared, nakedret, misspell, dupword, gochecknoinits |
| Tests and diagnostics | thelper, testpackage, tparallel, sloglint |
| Complexity and size | cyclop, gocognit, funlen |
| Security and suppressions | gosec, nolintlint |

Analyze the normal build and the `typesafe_live` build separately. Static
analysis and compile-only validation of the live suite must not execute provider
requests. Examples remain part of validation.

## Mechanical formatting

`make fmt` must apply all selected mechanical formatting, including logical
blank lines. Run the formatters and an explicitly restricted whitespace autofix
pass for `wsl_v5` and `nlreturn`; do not include semantic fixes from other linters
in that command. The combined pipeline must be idempotent.

## Developer commands

Run from the repository root with Go and Make installed:

```sh
make fmt        # rewrite mechanical formatting and logical whitespace
make fmt-check  # report formatting/whitespace drift without rewriting source
make lint       # validate config; analyze normal and typesafe_live builds
```

All commands use `scripts/quality.sh` and `.golangci.yml`. The script sets
`GOTOOLCHAIN=go1.26.2`, installs exactly golangci-lint v2.15.0 with that toolchain,
and verifies both versions before use. Its binary lives outside the module in
`${XDG_CACHE_HOME:-$HOME/.cache}/typesafe-sdk/quality/`; set
`TYPESAFE_QUALITY_CACHE` to another absolute cache directory when needed. A cold
cache needs network access to download the toolchain, tool, and dependencies.
No global Go settings or SDK dependencies are modified. CI must invoke these
same Make targets; repository-wide green acceptance is a separate delivery gate.

`make fmt` first applies gofumpt and goimports, then runs only the `wsl_v5` and
`nlreturn` lint fixes for normal and live-tag source, and finishes with the
mechanical formatters. It never runs the full lint profile with `--fix`.
It finishes with the read-only formatting check. If the pinned whitespace fixer
leaves a diagnostic, the command fails with that diagnostic; expand inline
semicolon statements into a multiline block where needed and rerun it.
`make fmt-check` combines formatter diffs with those same whitespace checks in
both build modes, so a gofmt-clean file can still fail for logical whitespace.
The lint command validates configuration and reports findings from both modes,
even when the ordinary build fails. These checks include tests, fixtures and
examples; generated-file exclusions and exclusion presets are disabled.

Imports use the standard-library, third-party, then current-module groups.
Whitespace checks use the full `wsl_v5` profile, allowing one related preceding
statement and normal call/error-check pairs. `nlreturn` allows blocks of two
lines. Revive retains its standard rules, adding exported documentation,
scope-preserving early returns, import shadowing and control-flow nesting at
most three. Remaining selected analyzers use pinned-version standard settings
unless `.golangci.yml` explicitly overrides them.

Validate compatibility separately with the SDK minimum toolchain:

```sh
GOTOOLCHAIN=go1.25.0 go test -race ./...
GOTOOLCHAIN=go1.25.0 go vet ./...
CGO_ENABLED=0 GOTOOLCHAIN=go1.25.0 go build ./...
GOTOOLCHAIN=go1.25.0 go test -tags=typesafe_live -run '^$' ./...
```

The build includes all examples. The last command compiles the live tests with
an empty test selection; it does not send provider requests. Lint and formatting
analysis do not execute Go tests or example `main` functions.

## Complexity and size

Apply these thresholds to production code, tests, and examples alike:

- Cyclomatic complexity: at most 10.
- Cognitive complexity: at most 15.
- Function size: at most 40 statements.

Do not limit physical function lines. Required whitespace must not force extra
helpers solely to reduce a line count. Keep combined public-client HTTP journeys
intact when extracting helpers, including the causal sequence of input mutation
and retry replay. Helpers running in HTTP-handler goroutines must not terminate
the test goroutine through `t.Fatal` or equivalent calls.

These numerical thresholds are repository policy, not rules prescribed by
Google or Uber. No fixed source-line width is introduced.

## Error checking and suppressions

Disable errcheck's built-in exclusions and check discarded errors and unchecked
type assertions. Do not copy example exclusion presets or blanket test, file-I/O,
or `Close` exclusions into the lint configuration.

Allow only local suppressions naming the exact check and explaining why the
operation is intentional or safe. Preserve intentional API behavior such as
caller-selected image paths and non-security retry jitter. A fully read HTTP
response must not become a failed SDK call solely because a linter requests
handling an irrelevant cleanup error. Every suppression requires human review;
`nolintlint` enforces annotation quality, not the validity of the reasoning.

## Behavior fixes and compatibility

Include localized real fixes discovered during lint adoption in the same PR,
using separate commits and behavior checks. Such fixes must uphold the existing
contract rather than redefine it. Changes to the public API or SDK contract
belong in separate work.

In particular, retain caller-resource ownership, context cancellation/deadline
identity, `errors.Is`/`errors.As` behavior, retained HTTP metadata and partial
bodies, and secret-safe error formatting established by the accepted ADRs.

## Full manual audit

Review all SDK source, tests, examples, and relevant CI/documentation. Findings
must be sought independently of linter output. In particular, inspect ownership
of HTTP resources, goroutine lifetimes, copying of mutable data at boundaries,
contexts and deadlines, error handling and secret exposure, naming, API
organization, and readability of complete test scenarios.

Use this checklist independently of analyzer output:

- Confirm caller-owned HTTP clients/transports remain owned by the caller.
- Follow each goroutine through cancellation, shutdown and synchronization.
- Trace mutable maps, slices, headers and request data across ownership boundaries.
- Follow total and attempt deadlines through requests, body reads and retry waits.
- Check error chains, retained HTTP metadata and diagnostics for secret exposure.
- Review names, documentation, API organization and complete consumer scenarios.
- Read every suppression in context and assess the specific safety argument.
- Record the full reviewed scope and a disposition for each finding, including
  incompatible recommendations requiring separate work.

Record the audit in `docs/reviews/strict-go-lint-audit.md` during implementation.
The report must identify the guide revisions or retrieval dates, the reviewed
code scope, applicable recommendations, concrete findings, fixes and validation,
and reasoned deviations or non-applicable recommendations. Distinguish automated
checks from human judgement; a clean lint result is not proof of full guide
compliance.

Fix actionable findings within the agreed compatibility boundary. Record any
finding that requires a public API or SDK contract change as separate work,
without silently weakening the contract or claiming it was fixed. Existing ADRs
and the public contract take precedence over conflicting style recommendations.

## Completion evidence

- Mechanical formatting is idempotent and fmt-check fails on formatting drift.
- Full lint passes without a new-code baseline or blanket exclusions, including
  the live-tag build.
- Go 1.25.0 race tests, vet, cgo-disabled builds, example compilation, and
  compile-only live-suite checks pass.
- The manual audit report accounts for its findings and justified deviations.
- The PR records the exact final commit and its CI results. Merge, release,
  deployment, and live-provider execution are separate operations.

Domain vocabulary remains in `GLOSSARY.md`; implementation tooling and style
rules belong in this document.
