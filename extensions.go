package typesafe

import (
	"encoding/json"
	"strings"
)

// RawQuestion holds a complete JSON object for an unknown future Question kind.
// Its type field must be a nonblank string. Known noul/choice/score/box kinds must
// use the concrete SDK structs, preserving their local validation and opt-ins.
// For example: RawQuestion(`{"type":"future-ranking","options":["a","b"]}`).
// The caller must not mutate its bytes concurrently with a call.
type RawQuestion json.RawMessage

func (q RawQuestion) encodeQuestion() (string, json.RawMessage, error) {
	var envelope map[string]json.RawMessage

	if err := json.Unmarshal(q, &envelope); err != nil || envelope == nil {
		return "", nil, &ValidationError{"raw_question", "must be a JSON object"}
	}

	var kind string

	if err := json.Unmarshal(envelope["type"], &kind); err != nil || strings.TrimSpace(kind) == "" {
		return "", nil, &ValidationError{"raw_question.type", "must be a nonblank string"}
	}

	switch kind {
	case "noul", "choice", "score", "box":
		return "", nil, &ValidationError{"raw_question.type", "known kinds require a concrete SDK Question"}
	}

	return kind, append(json.RawMessage(nil), q...), nil
}
