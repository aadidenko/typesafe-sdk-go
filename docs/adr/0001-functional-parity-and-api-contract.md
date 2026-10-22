# Functional parity with the Python SDK, governed by the API contract

The Go SDK will cover every capability of the official Python SDK v0.7.5, commit `f36e68549ceca3191e5791e5f99ea1baa1a258b5`. A checked-in snapshot of the TypeSafe OpenAPI document governs the HTTP contract when the two references differ. Python-specific coercions, omissions, and lifecycle behavior are not compatibility requirements; deliberate differences must be documented and tested. This fixes a reviewable scope while allowing an idiomatic Go implementation.

References: [Python SDK baseline](https://github.com/typesafe-ai/typesafe-sdk-python/tree/f36e68549ceca3191e5791e5f99ea1baa1a258b5), [OpenAPI snapshot](../reference/typesafe-openapi.json).
