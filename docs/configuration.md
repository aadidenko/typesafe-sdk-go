# Configuration and model discovery

`NewClient(Config)` reads only explicit settings. Environment loading is an
optional separate operation:

```go
config := typesafe.LoadEnv(typesafe.Config{
    Model: "jev-latest", // Overrides TYPESAFE_DEFAULT_MODEL.
})
client, err := typesafe.NewClient(config)
```

`LoadEnv` fills omitted APIKey, BaseURL, Model and LogLevel from
`TYPESAFE_API_KEY`, `TYPESAFE_BASE_URL`, `TYPESAFE_DEFAULT_MODEL` and
`TYPESAFE_LOG_LEVEL`. Blank environment values are ignored. An explicit nonempty
string wins; an explicit invalid API key is rejected instead of falling back.
API keys have surrounding whitespace stripped and must contain printable ASCII
without internal whitespace. Validation errors never include the key.

Log levels are `debug`, `info`, `warn`/`warning`, `error` and `off`, ignoring case
and surrounding whitespace. Unknown values are ignored. `Config.LogLevel` is an
optional `*slog.Level`; explicit values override the environment and
`LogLevelOff` represents `off`. Loading environment configuration changes no
global logger or handler. Diagnostics still require an explicitly supplied
logger; merely loading a log level does not install one. Body logging has a
separate opt-in.

Default base URL is `https://api.typesafe.ai`; default model is `jev-latest`.
Default attempt timeout is 10 seconds, and the hard overall call budget is 30
seconds. A shorter context deadline wins. `WithModel`, `WithHeaders` and
`WithTimeouts` apply to one call; they do not change shared client defaults.
Model selection matters to SystemOne; ListModels accepts the same applicable
header, timeout and reliability options.

```go
models, err := client.ListModels(ctx)
if err != nil { return err }
for _, model := range models.Models {
    fmt.Println(model.Name, model.Description, model.ReleaseDate)
}
```

Model discovery sends `GET /v1/models`. Returned names include aliases; the
actual model answering SystemOne is retained in its response. Both operations
retain status, cloned headers, request ID and original body bytes after closing
the network body. Malformed successful responses produce `ProtocolError` with
the retained data and a field path. HTTP failures and context cancellation use
the same error contract as SystemOne.

Set `Config.BaseURL` to a gateway root such as `https://gateway.example/api`.
The `/api` prefix is preserved for both `/v1/models` and `/v1/systemone`. URLs
must be absolute HTTP(S), without credentials, query or fragment. Supply
`Config.HTTPClient` to configure standard transport TLS, proxies, pools and
HTTP versions. Construction and calls do not modify its timeout, redirect
policy or transport, and the SDK never closes caller-owned resources. A caller
HTTP client's own timeout can impose an additional shorter limit. The caller
remains responsible for closing its idle connections when appropriate.

Custom header maps are copied and normalized case-insensitively. Call headers
replace matching client defaults even when the original map uses lowercase
keys; duplicate spellings of one key within a single map are rejected.
Authentication, Accept, SDK/runtime identification, retry-count, content format,
compression, experimental images and HTTP routing/framing/hop-by-hop headers
are SDK-managed and cannot be supplied through custom headers. Use the
corresponding configuration option instead. Unrelated provider opt-ins, such as
`TypeSafe-Danger-Zone-Long-Context: true`, are allowed. Header names and values
are validated locally. Caller maps and slices are never modified; as with Go
maps generally, do not mutate them concurrently while constructing a client or
an option.

The [runnable model example](../examples/models/main.go) demonstrates explicit
environment loading, gateway configuration, model metadata and a custom
caller-owned HTTP client. Running it makes a live request and requires an
externally supplied API key. It is compiled, without invoking main, by normal
`go test ./...`.
