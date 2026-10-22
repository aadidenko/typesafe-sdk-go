# Contributing

This is an independent Go implementation of the TypeSafe API. Start with the
README, [glossary](GLOSSARY.md), and relevant [architecture decisions](docs/adr/).
The [capability matrix](docs/capabilities.md) describes the compatibility scope.

## Proposing changes

Use the issue forms for bug reports and feature requests. Discuss changes to
the public API or HTTP contract in an issue before implementing them. Include
a minimal reproduction with synthetic data; never post credentials, private
request bodies, or customer data. Report vulnerabilities through
[the security policy](SECURITY.md).

Maintainers use `needs-triage`, `needs-info`, `ready-for-agent`,
`ready-for-human`, and `wontfix` according to the
[triage policy](docs/agents/triage-labels.md). A readiness label is not merge
approval.

## Development and validation

Install Go 1.25+ and Make. From the repository root, run:

```sh
make fmt-check
make lint
GOTOOLCHAIN=go1.25.0 go test -race ./...
GOTOOLCHAIN=go1.25.0 go vet ./...
CGO_ENABLED=0 GOTOOLCHAIN=go1.25.0 go build ./...
GOTOOLCHAIN=go1.25.0 go test -tags=typesafe_live -run '^$' ./...
```

`make fmt` applies mechanical formatting. The quality commands download the
pinned Go 1.26.2 toolchain and golangci-lint outside the module when needed.
Follow the [code-style policy](docs/code-style.md); do not suppress checks
broadly or add the lint tool's dependencies to the SDK module.

Ordinary tests use local HTTP fixtures and require no provider credentials.
Keep regressions deterministic and exercise observable SDK behavior. Live
tests are manual, require explicit opt-in, and may incur provider charges.
Do not add live provider execution to CI. Examples that contact the provider
must also be invoked explicitly.

## Pull requests

Fork the repository and open a focused PR against `master`. Explain the
problem, resulting behavior, related issue, and validation performed. Update
documentation and meaningful regression coverage when behavior changes.
Avoid unrelated formatting or refactoring.

The `quality` and `test` jobs must pass before merge. Workflows from external
contributors require maintainer approval before running. Changes merge using
Squash merge; a single contributor does not need approval from a second person.

Contributions are provided under this project's [MIT license](LICENSE).
Retain applicable third-party notices and document any new third-party material.
