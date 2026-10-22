# Capability matrix

Reference: official Python SDK v0.7.5 at
`f36e68549ceca3191e5791e5f99ea1baa1a258b5`; wire authority is the checked-in API
0.2.0 OpenAPI snapshot. This matrix currently records the custom decoding and
extension slice. The final acceptance ticket expands it to all 56 spec stories
and every pinned public capability; this partial table does not claim complete
SDK parity.

| Python capability | Go behavior / deliberate redesign | Evidence |
|---|---|---|
| `system_one(response_model=BaseModel)` | `DecodeResponse` into an ordinary caller-owned Go structure after SDK validation | `TestCustomDecodingPreservesOriginalAnswersAndMetadata`, examples/custom-decoding |
| Response subclass with lifted answer fields | Explicit `DecodeAnswers` on original named Answer object; no inheritance or field lifting | `TestCustomDecodingUsesOrdinaryGoJSONErrors` |
| User model type coercions and validation | Standard JSON types and errors; explicit application validation; no implicit string-to-number conversion | `TestCustomDecodingUsesOrdinaryGoJSONErrors` |
| Raw HTTP response, custom unknown fields | Original bytes, status, headers and request ID retained independently of repeated decoding | `TestCustomDecodingPreservesOriginalAnswersAndMetadata` |
| Raw Question dictionaries with future `type` | Complete `RawQuestion` JSON object; unknown discriminator validated as envelope only; known kinds require typed structs | `TestFutureQuestionAndUnknownAnswersRoundTrip`, `TestRawQuestionRejectsInvalidEnvelopeAndKnownKindsBeforeHTTP`, examples/extensions |
| Future Answers ignored by Python parsed model | Direct `UnknownAnswer` discriminator/raw payload preservation; additional unknown Answers retained | `TestFutureQuestionAndUnknownAnswersRoundTrip` |
| Missing or mismatched requested Answer tolerated by Python | Protocol errors with retained HTTP data; matching future RawQuestion can receive UnknownAnswer | `TestFutureQuestionStillRequiresMatchingNamedAnswer`, existing requested-answer contract tests |
| `extra_body` last-write-wins merge | ExtraBody transmits unrelated future fields; collision with model/state/questions/assets is a local validation error | `TestExtraBodyAddsFutureFieldsWithoutReplacingCoreRequest`, `TestExtraBodyRejectsCoreCollisionsAndInvalidValuesBeforeHTTP` |
| Automatic zstd at complete prepared body >= 1 KiB | Zero `Config.Compression` selects available pure-Go zstd; JSON and complete multipart compressed once and replayed identically, bounded to provider's 8 MiB window and 32 MiB logical/encoded sizes | `TestDefaultCompressionThreshold`, `TestMultipartCompressionThresholdUsesCompleteBody`, `TestZstdWindowSupportsLogicalBodiesLargerThanEightMiB`, `TestLogicalBodyLimitIsNotExpandedByCompression`, `TestCompressedBodyAlsoRespectsProviderLimit` |
| Python compression opt-out; optional zstd dependency | `CompressionNone`, explicit `CompressionZstd`, client/call overrides; standard-library `CompressionGzip` is an intentional addition, no optional codec installation or cgo | `TestCompressionModesAndCallOverrides`, `TestCompressedJSONAndMultipartRetriesReplayExactBytes`, examples/compression |
