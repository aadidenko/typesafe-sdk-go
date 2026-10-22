package typesafe

import (
	"encoding/json"
)

// Question is one typed judgment in a System One evaluation.
type Question interface {
	encodeQuestion() (string, json.RawMessage, error)
}

// Questions associates each judgment with the name returned in Answers.
type Questions map[string]Question

// SystemOneRequest shares State among named Questions.
type SystemOneRequest struct {
	State     any
	Questions Questions
	// ExtraBody adds future top-level fields; model, state, questions and assets
	// are reserved and must be set through the typed request or call options.
	ExtraBody map[string]any
	Assets    Assets
}

// NoulCriteria explains what counts as true or false. Nil descriptions are omitted.
type NoulCriteria struct {
	True  any `json:"true,omitempty"`
	False any `json:"false,omitempty"`
}

// NoulQuestion asks for the probability of yes or true.
type NoulQuestion struct {
	Instructions any
	Criteria     *NoulCriteria
}

func (q NoulQuestion) encodeQuestion() (string, json.RawMessage, error) {
	instructions, err := encodeContent(q.Instructions, "instructions", true)
	if err != nil {
		return "", nil, err
	}
	var criteria map[string]json.RawMessage
	if q.Criteria != nil {
		criteria = make(map[string]json.RawMessage)
		for name, value := range map[string]any{"true": q.Criteria.True, "false": q.Criteria.False} {
			if value == nil {
				continue
			}
			encoded, err := encodeContent(value, "criteria."+name, true)
			if err != nil {
				return "", nil, err
			}
			criteria[name] = encoded
		}
	}
	body, err := json.Marshal(struct {
		Type         string                     `json:"type"`
		Instructions json.RawMessage            `json:"instructions,omitempty"`
		Criteria     map[string]json.RawMessage `json:"criteria,omitempty"`
	}{"noul", instructions, criteria})
	return "noul", body, err
}

// encodeContent accepts JSON strings, objects and arrays; nested scalars and nulls
// retain normal encoding/json semantics. Encoding once ensures custom marshalers
// send the same snapshot that was validated.
func encodeContent(value any, field string, nullable bool) (json.RawMessage, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, &ValidationError{field, "must be JSON-encodable"}
	}
	var decoded any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		return nil, &ValidationError{field, "must contain valid JSON"}
	}
	switch decoded.(type) {
	case string, map[string]any, []any:
		return encoded, nil
	case nil:
		if nullable {
			return encoded, nil
		}
	}
	return nil, &ValidationError{field, "must be a string, object or array"}
}
