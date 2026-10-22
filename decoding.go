package typesafe

import (
	"encoding/json"
	"fmt"
)

// DecodeResponse decodes the entire original response into destination using
// ordinary encoding/json semantics. It leaves retained metadata and typed
// Answers unchanged, and can be called repeatedly, including after an error.
// Unknown response fields are available to caller-defined structures.
func (m HTTPMetadata) DecodeResponse(destination any) error {
	if err := json.Unmarshal(m.RawBody, destination); err != nil {
		return fmt.Errorf("typesafe: decode response: %w", err)
	}
	return nil
}

// DecodeAnswers decodes the original named answers object into destination.
// No fields are lifted, no SDK model inheritance is required, and application
// validation remains the caller's responsibility. JSON errors support errors.As.
func (r *SystemOneResponse) DecodeAnswers(destination any) error {
	if r == nil {
		return &ValidationError{"response", "must not be nil"}
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(r.RawBody, &root); err != nil {
		return fmt.Errorf("typesafe: decode answers: %w", err)
	}
	if err := json.Unmarshal(root["answers"], destination); err != nil {
		return fmt.Errorf("typesafe: decode answers: %w", err)
	}
	return nil
}
