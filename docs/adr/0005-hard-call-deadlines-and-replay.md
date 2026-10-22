# Bound retries with a hard call deadline and replay prepared bodies

Calls default to a hard 30-second deadline covering retry waits, HTTP operations, and response body reads, with at most 10 seconds per attempt. A shorter caller context deadline takes precedence, and cancellation prevents further retries. Unlike the Python SDK's soft retry budget, these limits bound in-flight operations as well as future retry scheduling.

The default policy permits two retries for HTTP 408, 429, and 5xx responses and temporary network failures, with exponential backoff, jitter, and Retry-After support. Retrying an ambiguous network failure can execute an evaluation again; the SDK cannot guarantee exactly-once execution.

Request bodies are prepared once and replayed for retries, including multipart uploads. zstd is the default for bodies of at least 1 KiB; callers may select gzip or disable compression. This preserves the Python compression capability and supports the API's standard-library-compatible alternative.
