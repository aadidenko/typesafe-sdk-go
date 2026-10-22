package typesafe

import (
	"encoding/json"
	"fmt"
)

// Answer is a named judgment returned by the API.
type Answer interface{ Kind() string }

// NoulAnswer holds the probability of yes or true, distinct from Confidence.
type NoulAnswer struct {
	Probability float64 `json:"noul"`
}

func (NoulAnswer) Kind() string { return "noul" }

// UnknownAnswer preserves unrecognized API extensions.
type UnknownAnswer struct {
	Type string
	Raw  json.RawMessage
}

func (a UnknownAnswer) Kind() string { return a.Type }

// Usage counts the input and output tokens for an evaluation.
type Usage struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
}

// SystemOneResponse retains validated Answers and the complete HTTP response.
// Callers need not close a response.
type SystemOneResponse struct {
	HTTPMetadata
	Model   string
	Usage   Usage
	Answers map[string]Answer
}

func (r *SystemOneResponse) Noul(name string) (NoulAnswer, error) {
	answer, ok := r.Answers[name]
	if !ok {
		return NoulAnswer{}, fmt.Errorf("%w: %s", ErrAnswerMissing, name)
	}
	noul, ok := answer.(NoulAnswer)
	if !ok {
		return NoulAnswer{}, fmt.Errorf("%w: %s", ErrAnswerKind, name)
	}
	return noul, nil
}

func parseSystemOne(meta HTTPMetadata, requested map[string]string) (*SystemOneResponse, error) {
	fail := func(field, message string) (*SystemOneResponse, error) {
		return nil, &ProtocolError{meta, field, message}
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(meta.RawBody, &root); err != nil || root == nil {
		return fail("$", "expected a JSON object")
	}
	var result SystemOneResponse
	result.HTTPMetadata = meta
	if err := json.Unmarshal(root["model"], &result.Model); err != nil || result.Model == "" {
		return fail("model", "required nonempty string")
	}
	var usage map[string]json.RawMessage
	if err := json.Unmarshal(root["usage"], &usage); err != nil || usage == nil {
		return fail("usage", "required object")
	}
	for name, dest := range map[string]*int64{"input_tokens": &result.Usage.InputTokens, "output_tokens": &result.Usage.OutputTokens} {
		raw := usage[name]
		if len(raw) == 0 || string(raw) == "null" {
			return fail("usage."+name, "required integer")
		}
		if err := json.Unmarshal(raw, dest); err != nil || *dest < 0 {
			return fail("usage."+name, "required nonnegative integer")
		}
	}
	var answers map[string]json.RawMessage
	if err := json.Unmarshal(root["answers"], &answers); err != nil || len(answers) == 0 {
		return fail("answers", "required nonempty object")
	}
	result.Answers = make(map[string]Answer, len(answers))
	for name, raw := range answers {
		field := "answers." + name
		var object map[string]json.RawMessage
		if err := json.Unmarshal(raw, &object); err != nil || object == nil {
			return fail(field, "required object")
		}
		var kind string
		if err := json.Unmarshal(object["type"], &kind); err != nil || kind == "" {
			return fail(field+".type", "required nonempty discriminator")
		}
		if expected, ok := requested[name]; ok && expected != kind {
			return fail(field+".type", "does not match requested question")
		}
		if kind == "noul" {
			var value *float64
			if err := json.Unmarshal(object["noul"], &value); err != nil || value == nil || *value < 0 || *value > 1 {
				return fail(field+".noul", "required probability from 0 to 1")
			}
			result.Answers[name] = NoulAnswer{*value}
		} else {
			result.Answers[name] = UnknownAnswer{kind, append(json.RawMessage(nil), raw...)}
		}
	}
	for name := range requested {
		if _, ok := result.Answers[name]; !ok {
			return fail("answers."+name, "requested answer is missing")
		}
	}
	return &result, nil
}
