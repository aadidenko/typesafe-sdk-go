#!/bin/sh
# Keep the development tool and its dependencies outside the SDK module.
set -eu

export GOTOOLCHAIN=go1.26.2
lint_version=2.15.0
cache_dir=${TYPESAFE_QUALITY_CACHE:-${XDG_CACHE_HOME:-$HOME/.cache}/typesafe-sdk/quality}
tool_dir=$cache_dir/golangci-lint-$lint_version-$GOTOOLCHAIN
lint=$tool_dir/golangci-lint

if [ ! -x "$lint" ]; then
    mkdir -p "$tool_dir"
    (cd "$tool_dir" && GOBIN="$tool_dir" go install "github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v$lint_version")
fi

# Reject stale or substituted binaries even when the cache is already populated.
case $("$lint" version) in
    "golangci-lint has version $lint_version built with $GOTOOLCHAIN "*) ;;
    *) echo "Expected golangci-lint $lint_version built with $GOTOOLCHAIN; remove $tool_dir and retry." >&2; exit 1 ;;
esac

config=.golangci.yml
case ${1:-} in
    fmt)
        "$lint" fmt --config "$config" ./...
        # Only the named whitespace linters and configured mechanical formatters
        # may apply fixes. Never run the complete lint profile with --fix here.
        "$lint" run --config "$config" --enable-only=wsl_v5,nlreturn --fix ./...
        "$lint" run --config "$config" --enable-only=wsl_v5,nlreturn --fix --build-tags=typesafe_live ./...
        "$lint" fmt --config "$config" ./...
        # The pinned fixer can report success without correcting inline
        # semicolon statements. Verify the final files instead of trusting it.
        sh "$0" fmt-check
        ;;
    fmt-check)
        status=0
        "$lint" fmt --config "$config" --diff ./... || status=1
        "$lint" run --config "$config" --enable-only=wsl_v5,nlreturn ./... || status=1
        "$lint" run --config "$config" --enable-only=wsl_v5,nlreturn --build-tags=typesafe_live ./... || status=1
        exit "$status"
        ;;
    lint)
        "$lint" config verify --config "$config"
        status=0
        "$lint" run --config "$config" ./... || status=1
        "$lint" run --config "$config" --build-tags=typesafe_live ./... || status=1
        exit "$status"
        ;;
    *) echo "Usage: $0 {fmt|fmt-check|lint}" >&2; exit 2 ;;
esac
