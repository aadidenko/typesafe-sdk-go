# Validate requested answers while preserving unknown response data

The Go SDK validates required response fields, numeric ranges, and the presence and type of requested answers. Missing requested answers and type mismatches are protocol errors with the original response retained for diagnosis. Probability sums need not equal one exactly because of rounding. This deliberately strengthens the Python baseline's structural validation and permissive omissions.

Additional unknown answer types are retained as `UnknownAnswer`, and unknown fields remain available in the original JSON. Explicit decoding into user-defined Go types uses normal JSON semantics without implicit string-to-number coercions. This supports API evolution without silently dropping data or weakening validation of known requested answers.
