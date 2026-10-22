# Capability matrix

The baseline is official Python SDK **v0.7.5**, commit
`f36e68549ceca3191e5791e5f99ea1baa1a258b5`. The checked-in
[TypeSafe API 0.2.0 OpenAPI snapshot](reference/typesafe-openapi.json) governs the
wire contract when the references differ. This matrix covers all 56 stories in
[spec #1](https://github.com/aadidenko/typesafe-sdk-go/issues/1), all **46** names
in the pinned Python `__all__`, importable version metadata, and the associated
public methods and configuration. Evidence is deterministic local HTTP testing
unless explicitly identified as a manual live test.

Test symbols below are searchable in the repository's `*_test.go` files. The
[acceptance record](acceptance.md) states which checks were actually run and the
limits of that evidence. A fixture result is not live-provider or model-quality
verification.

## The 56 user stories

| # | Implemented behavior | Concrete evidence |
|---|---|---|
| 1 | Standalone `github.com/aadidenko/typesafe-sdk-go`, package `typesafe`, Go 1.25+, no Python/cgo | go.mod; CI cgo-disabled build; acceptance commands |
| 2 | Reusable concurrent Client | `TestConcurrentCompleteRequestsRemainIsolated`, `TestConcurrentModelsAndEvaluationsKeepCallSettingsIsolated` |
| 3 | Named Questions against one shared State | `TestCompleteWorkflow`, `TestMixedNoulChoiceScore` |
| 4 | Noul instructions and true/false structured criteria | `TestSystemOneNoul`, `TestNamedImageMultipartReferences`, examples/quickstart |
| 5 | Choice alternatives with optional descriptions, 1–255 limits | `TestMixedNoulChoiceScore`, `TestIndividualChoiceAndScoreBoundaryCardinalities`, `TestChoiceScoreValidationBeforeHTTP`, examples/classification |
| 6 | Score ordered zero-based levels, 1–10 limits, fractional values | Same boundary/validation tests; `TestMixedNoulChoiceScore`, examples/scoring |
| 7 | Box normalized image coordinates | `TestBoxAndNoulWithNullableConfidence`, `TestInvalidBoxResponsesRetainOriginal`, examples/images |
| 8 | All four Question kinds in one evaluation | `TestCompleteWorkflow`, examples/complete |
| 9 | JSON-compatible text, structs, maps and slices, with nested nulls | `TestSystemOneNoul`, `TestMixedNoulChoiceScore`, `TestImageSourcesAndGeneratedReferences`, `TestCompleteWorkflow` |
| 10 | Structured instructions and criteria, including Go structs | `TestCompleteWorkflow`, `TestMixedNoulChoiceScore`, `TestNamedImageMultipartReferences` |
| 11 | Local shape/encoding/nonfinite-number failures before traffic | `TestInvalidContentRejectedBeforeHTTP`, `TestChoiceScoreValidationBeforeHTTP` |
| 12 | Names reliably match original requested Questions | `TestSuccessfulResponseMustSatisfyContract`, `TestFutureQuestionStillRequiresMatchingNamedAnswer` |
| 13 | Noul/Choice/Score/Box accessors return values and explicit missing/kind errors | `TestAllAnswersAndAccessorErrors`, `TestRoundedDistributionsAndAccessorErrors`, `TestBoxAndNoulWithNullableConfidence` |
| 14 | Full named collection and filtering by discriminator | `TestCompleteWorkflow`, `TestMixedNoulChoiceScore`; `Answers`, `AnswersByKind`, `Answer.Kind` |
| 15 | All primitive values, distributions, rubrics and coordinates retained | `TestCompleteWorkflow`, `TestMixedNoulChoiceScore`, `TestBoxAndNoulWithNullableConfidence` |
| 16 | Confidence distinct from probabilities and Noul | `TestMixedNoulChoiceScore` and `TestCompleteWorkflow` use unequal Confidence/class-probability values |
| 17 | Box null Confidence distinct from numeric zero and missing | `TestBoxAndNoulWithNullableConfidence`, `TestInvalidBoxResponsesRetainOriginal` |
| 18 | Actual model plus required token Usage | `TestSystemOneNoul`, `TestSuccessfulResponseMustSatisfyContract`, `TestCompleteWorkflow` |
| 19 | Models with name, description and release date | `TestListModelsUsesGatewayAndRetainsMetadata`, `TestListModelsRejectsMalformedSuccessAndRetainsErrorMetadata`, examples/models |
| 20 | Client and per-call model selection, no media model switching | `TestConcurrentModelsAndEvaluationsKeepCallSettingsIsolated`, `TestBoxAndNoulWithNullableConfidence` |
| 21 | Explicit application struct decoding of named Answers | `TestCustomDecodingPreservesOriginalAnswersAndMetadata`, `TestCustomDecodingUsesOrdinaryGoJSONErrors`, examples/custom-decoding |
| 22 | Full original response decoding into application structures | Same custom-decoding tests; `TestCompleteWorkflow` decodes a future top-level field |
| 23 | Retained status, cloned headers, request ID and body bytes; no response Close | `TestSystemOneNoul`, `TestCustomDecodingPreservesOriginalAnswersAndMetadata`, `TestCompleteWorkflow` |
| 24 | Required fields, malformed success, missing names and mismatched kinds produce ProtocolError | `TestSuccessfulResponseMustSatisfyContract`, `TestChoiceAndScoreRejectInvalidAnswers`, `TestInvalidBoxResponsesRetainOriginal` |
| 25 | Unknown fields and future Answers remain available | `TestFutureQuestionAndUnknownAnswersRoundTrip`, `TestAllAnswersAndAccessorErrors`, `TestCompleteWorkflow` |
| 26 | Image bytes, files, base64 and image.Image, PNG/JPEG | `TestImageSourcesAndGeneratedReferences`, `TestNamedImageMultipartReferences` |
| 27 | Named or generated references in request-scoped Assets | Same image tests; `Assets.Add`, examples/images |
| 28 | Nested/embedded reference strings in State, instructions and criteria | `TestNamedImageMultipartReferences`, `TestCompleteWorkflow` |
| 29 | Repeated reference uploads once | `TestNamedImageMultipartReferences`, `TestImageSnapshotsAndDistinctNamedUploads` |
| 30 | Binary multipart with first JSON request part and matching descriptors | `TestNamedImageMultipartReferences`, `TestCompleteWorkflow` |
| 31 | Clear local file/base64/image source failures | `TestInvalidImageSourcesAndOptInFailLocally` |
| 32 | Explicit experimental opt-in for images and Box | `TestInvalidImageSourcesAndOptInFailLocally` |
| 33 | Media header reflects opt-in while retaining selected model | `TestNamedImageMultipartReferences`, `TestBoxAndNoulWithNullableConfidence` |
| 34 | Explicit construction and opt-in environment helper | `TestExplicitConfigurationIgnoresEnvironmentAndClonesHeaders`, `TestEnvironmentIsExplicitAndExplicitConfigWins` |
| 35 | Explicit settings win; blank environment ignored; safe key validation | `TestEnvironmentIsExplicitAndExplicitConfigWins`, `TestBlankEnvironmentAndExplicitLogLevel`, `TestInvalidAPIKeyNeverAppearsInError` |
| 36 | API key, gateway URL, model, headers, compression and timeouts | Configuration/header/timeout tests; `TestCompressionModesAndCallOverrides`; examples/models and examples/compression |
| 37 | Standard caller-supplied HTTP client/transport | `TestSuppliedHTTPClientAndTransportRemainCallerOwned`, `TestListModelsUsesGatewayAndRetainsMetadata`, examples/models |
| 38 | Supplied HTTP resources remain caller-owned | `TestSuppliedHTTPClientAndTransportRemainCallerOwned`, `TestCompleteWorkflow` continued shared-client use |
| 39 | Isolated per-call settings and request data | `TestConcurrentCompleteRequestsRemainIsolated`, `TestPolicySnapshotsAndConcurrentOverrides` |
| 40 | Context cancellation interrupts active work and waits | `TestCancellationInterruptsActiveHTTPAndBodyRead`, `TestCancellationDuringRetryWaitPreventsMoreAttempts`, `TestLoggingPreservesActiveCancellation` |
| 41 | Hard total budget plus per-attempt deadline, including body reads | `TestShortestDeadlineBoundsCall`, `TestAttemptTimeoutControlAndTotalBudget`, `TestAttemptTimeoutCanRetryWhileBudgetSurvives` |
| 42 | Default transient HTTP and network retries | `TestDefaultRetriesTransientHTTPAndReplaysJSON`, `TestRetryEligibilityAndFinalHTTPMetadata`, `TestUnavailableConnectionRetryControls`, `TestClosedConnectionCanRecover` |
| 43 | Capped exponential backoff/jitter and server delay hints | `TestRetryDelayHints`, `TestServerHintsCanBeDisabledAndBackoffIsCancelable`, `TestHugeBackoffRemainsBoundedByCallDeadline`; retry.go capped/subtractive implementation |
| 44 | Single-attempt mode, custom status/connection/timeout rules and additive predicate | `TestCustomAndDisabledRetryPolicies`, `TestUnavailableConnectionRetryControls`, `TestAttemptTimeoutControlAndTotalBudget`, `TestRetryPolicyValidation` |
| 45 | JSON/multipart prepared once and replayed consistently | `TestDefaultRetriesTransientHTTPAndReplaysJSON`, `TestMultipartRetriesReplayPreparedFileSnapshot`, `TestCompressedJSONAndMultipartRetriesReplayExactBytes` |
| 46 | Default zstd at >=1 KiB, gzip, none; pure Go | `TestDefaultCompressionThreshold`, `TestCompressionModesAndCallOverrides`, `TestMultipartCompressionThresholdUsesCompleteBody`, `TestZstdWindowSupportsLogicalBodiesLargerThanEightMiB`, cgo-disabled build |
| 47 | errors.Is/As across validation, protocol, HTTP, transport and context failures | Client/response/retry tests; `TestCompleteWorkflow`; examples/logging and examples/complete |
| 48 | HTTP categories retain final status, body, headers and request ID | `TestHTTPFailuresRetainMetadataAndCategories`, `TestRetryEligibilityAndFinalHTTPMetadata`, `TestCustomAndDisabledRetryPolicies` empty-body case |
| 49 | Optional slog, separate bounded body opt-in, secret-header redaction | `TestLoggingIsOptionalAndBodiesNeedSeparateOptIn`, `TestLoggingRedactsSecretHeadersAcrossRetries`, `TestLoggingProtectsHeaderVariantsAndFailureCauses`, `TestLoggingTruncatesBodiesWithoutChangingRetainedData` |
| 50 | Future RawQuestion and unrelated ExtraBody | `TestFutureQuestionAndUnknownAnswersRoundTrip`, `TestExtraBodyAddsFutureFieldsWithoutReplacingCoreRequest`, examples/extensions |
| 51 | Core request fields and managed headers protected | `TestExtraBodyRejectsCoreCollisionsAndInvalidValuesBeforeHTTP`, `TestManagedHeadersAreRejectedBeforeHTTP`, `TestHeadersAreCanonicalOverridesAndCallerOwned` |
| 52 | Complete baseline and deliberate Go redesign audit | This matrix, its full public inventory below, and accepted ADRs |
| 53 | Compiling examples for every requested integration area | README example index; examples/complete plus focused examples |
| 54 | Deterministic real-public-client HTTP fixtures, no credentials/paid traffic | `TestCompleteWorkflow` and the full normal test suite |
| 55 | Go1.25 race/vet/format/minimum-version CI and cgo-disabled build | .github/workflows/ci.yml; acceptance.md executed commands; all examples compile |
| 56 | Separately gated manually invoked real-provider checks | manual_live_test.go: `TestLiveModels`, `TestLiveSystemOneNoul`; build tag `typesafe_live` plus `TYPESAFE_RUN_LIVE=1`; compiled only during local acceptance |

## Exhaustive pinned Python public inventory

These mappings describe capabilities rather than identical Python syntax or
runtime semantics. Source pointers refer to the pinned `src/typesafe_sdk` tree.

| Python exports or public behavior | Go implementation / explicit redesign | Evidence |
|---|---|---|
| `TypeSafeClient`, `AsyncTypeSafeClient` | One reusable `Client`, `NewClient(Config)`, contexts and goroutines | Concurrent client/workflow tests |
| `Models`, `AsyncModels` | `Client.ListModels` directly; no resource wrapper | Model tests, examples/models |
| `Noul`, `Choice`, `Score`, `Box` | Concrete `NoulQuestion`, `ChoiceQuestion`, `ScoreQuestion`, `BoxQuestion` with fixed discriminators | Primitive and complete workflow tests |
| `NoulModel`, `ChoiceModel`, `ScoreModel`, `BoxModel`, `QuestionModel`, `Question`, `Questions`, `NoulCriteria` | Typed question structs, `Question` interface, named `Questions` map and `NoulCriteria`; no TypedDict/model inheritance | Request contract tests; `RawQuestion` for future types only |
| `Content`, `ContentValue`, `JSONContent`, `JSONValue` | Ordinary Go structs/maps/slices/strings, nested JSON scalars/nulls; no Python dataclass/Pydantic runtime | Content validation, snapshot and structured workflow tests |
| `Asset`, `Image` | `Image`, four constructors, request `Assets`, generated `Assets.Add` references | Image-source/multipart tests |
| `Answer`, `NoulAnswer`, `ChoiceAnswer`, `ScoreAnswer`, `BoxAnswer`, `BoxCoordinates` | `Answer` interface, named answer structs; coordinate struct named `Box`; float64 judgments, integer Score indices, nullable Box Confidence | Primitive response tests; complete workflow |
| `SystemOneResponse`, `Usage` | Required typed response plus `HTTPMetadata` and explicit decoding | Required-presence and decoding tests |
| `ListModelsResponse`, `ModelMetadata` | Matching typed response with retained metadata | Model tests |
| `RetryPolicy` | Count/status/connection/timeout eligibility, additive Predicate, initial/max backoff, Jitter and RespectRetryAfter; hard Config.CallTimeout | Retry tests; docs/retries.md |
| `TypeSafeError`, `TypeSafeAPIError`, `TypeSafeAPIConnectionError`, `TypeSafeAPITimeoutError`, `TypeSafeAPIResponseValidationError` | ValidationError, HTTPError, TransportError, ProtocolError, sentinels, and context error chains replace exception inheritance | errors.Is/As tests |
| `TypeSafeBadRequestError`, `TypeSafeAuthenticationError`, `TypeSafePermissionDeniedError`, `TypeSafeNotFoundError`, `TypeSafeUnprocessableEntityError`, `TypeSafeRateLimitError`, `TypeSafeInternalServerError` | HTTPError with ErrBadRequest, ErrAuthentication, ErrPermission, ErrNotFound, ErrServerValidation, ErrRateLimit, ErrServer | HTTP-category/final-metadata tests |
| `constants` | APIKeyEnv, BaseURLEnv, DefaultModelEnv, LogLevelEnv, DefaultBaseURL, DefaultModel, DefaultAttemptTimeout, DefaultCallTimeout, LogLevelOff | environment.go and configuration tests; no Python module namespace needed |
| Importable `__version__` (outside `__all__`) | Exported `Version` and SDK/runtime identification headers | `TestSystemOneNoul`; client.go |
| Constructor `api_key`, `model`, `retry`, `timeout`, `headers`, `transport`, `http_client`, `base_url`, `compression` | Config.APIKey/Model/Retry/AttemptTimeout+CallTimeout/Headers/HTTPClient/BaseURL/Compression; standard Transport configured through HTTPClient | Configuration/ownership/compression tests |
| `system_one` options `model`, `assets`, `retry`, `timeout`, `extra_headers`, `extra_body`, `response_model` | SystemOneRequest, per-call options and explicit DecodeAnswers/DecodeResponse | Complete workflow, extensions/decoding/retry tests |
| `models.list(retry, timeout, extra_headers)` | ListModels shares header/timeout/retry CallOptions | Model and ListModels retry/cancellation tests |
| `.models`, `.attach_assets`, `.close`, sync/async context managers and `.aclose` | Direct ListModels, explicit per-request Assets, contexts/goroutines; no mutable attachment scope or SDK close that disrupts caller resources | Ownership and concurrent workflow tests |
| `.answers`, `.nouls`, `.choices`, `.scores`, `.boxes`, `.model`, `.usage` | Answers, typed accessors, AnswersByKind and Kind; Model/Usage; no cached property wrapper required | Complete workflow and accessor tests |
| `.from_http_response`, `.request_id`, `.raw_http_response`, inherited Pydantic value helpers | Public operations create validated responses; retained StatusCode/Headers/RequestID/RawBody and normal Go JSON decoding provide data access | Metadata/decoding tests; missing request ID is empty rather than raising |
| Independent custom models, inherited response models and lifted fields | Explicit DecodeAnswers/DecodeResponse into caller structures; ordinary JSON errors; application validation remains explicit | Custom-decoding tests/examples |
| `Image(data)`, `.from_base64`, `.from_pil`, `str(image)` | ImageFromBytes/File/Base64/GoImage and explicit reference strings; immutable construction snapshots; PNG/JPEG; no resizing/orientation correction | Image-source/snapshot tests |
| Retry `exceptions` and `predicate` | Go additive Predicate uses errors.Is/As and subsumes exception-class matching | Custom policy tests and retry example |
| Retry soft `timeout`, including Python None | Always-bounded positive call budget with per-attempt timeout; no unbounded retry mode | Deadline/stalled-body tests; ADR 0005 |
| `retry_after_ms` error detail and error message extraction | Original headers/body retained for caller inspection; policy parses millisecond/seconds/date hints; routine formatting omits arbitrary body/credentials | Retry-hint/final-metadata tests |
| Environment defaults, log level and global Python import initialization | Explicit LoadEnv with override precedence; no import-global config or logging; supported debug/info/warn/warning/error/off | Environment and optional-level tests |
| Python optional zstd and no gzip | Reliable pure-Go zstd at threshold, explicit gzip and none; 32MiB logical/encoded caps and bounded zstd window | Compression tests and docs/compression.md |
| Python debug logging implicitly includes bodies | Explicit supplied slog logger; independent LogBodies, bounded 4096-byte prefixes, secret headers always redacted | Logging opt-in/redaction/truncation tests |

## Deliberate differences governed by the ADRs

- Explicit request Assets and ordinary reference strings replace image discovery
  inside arbitrary objects and mutable client attachment contexts. Images are
  immutable validated PNG/JPEG snapshots; no Python resolver cache is required.
- Caller-owned HTTP clients/transports remain usable; SDK operations never close
  or mutate them. Contexts/goroutines replace separate sync/async lifecycles.
- Successful response fields, ranges, requested names and discriminators are
  checked strictly. Usage cannot silently default missing values to zero;
  nullable Box confidence remains explicit. Rounded probability sums are valid.
- UnknownAnswer preserves future types directly; original JSON retains all
  unfamiliar fields. Missing request IDs are represented by an empty string.
- Explicit decoding has normal Go JSON semantics, without inheritance, automatic
  answer-field lifting, string-to-number coercion or automatic user validation.
- Hard deadlines bound attempts, waits and body reads. An ambiguous failure may
  repeat an evaluation; exactly-once execution is not promised.
- ExtraBody cannot replace core fields; custom headers cannot replace managed
  authentication, protocol, runtime, retry, framing, compression or media fields.
- The pinned OpenAPI permits type-only Noul Questions, unlike Python's additional
  empty-judgment check. The HTTP contract governs this difference. Go rejects
  unsupported content and nonfinite numbers rather than coercing them to text.

Package release, deployment and additional endpoints, streaming, pagination,
server-side jobs, non-image media and application decision policies are outside
spec #1. Live provider/model quality has not been established by these fixtures.
