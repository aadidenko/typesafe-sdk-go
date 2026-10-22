# Security policy

## Supported code

Security fixes currently target the latest code on `master`. Older commits
have no separate security maintenance commitment.

## Reporting a vulnerability

Use [GitHub private vulnerability reporting](https://github.com/aadidenko/typesafe-sdk-go/security/advisories/new)
to report suspected vulnerabilities privately. Do not open a public issue or
pull request containing an exploit, credentials, or private application data.

Include the affected commit or version, Go version and operating system,
impact, and a minimal reproduction using synthetic data. Redact API keys,
authorization headers, and request or response bodies containing private data.
If a credential has been exposed, revoke or rotate it with its provider.

Reports are reviewed on a best-effort basis. There is no guaranteed response
time. Coordinate disclosure with the maintainer through the private report.

For ordinary bugs and feature requests, use the public issue forms. Provider
account or service vulnerabilities should be reported to the provider.
