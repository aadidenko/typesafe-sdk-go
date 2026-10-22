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

// Kind identifies a Noul Answer.
func (NoulAnswer) Kind() string { return "noul" }

// UnknownAnswer preserves unrecognized API extensions.
type UnknownAnswer struct {
	Type string
	Raw  json.RawMessage
}

// Kind returns the future discriminator retained from the API.
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

// Noul returns the named Noul Answer, or an error for a missing name or different kind.
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
	var root map[string]json.RawMessage

	if err := json.Unmarshal(meta.RawBody, &root); err != nil || root == nil {
		return nil, protocolField(meta, "$", "expected a JSON object")
	}

	var result SystemOneResponse

	result.HTTPMetadata = meta

	if err := json.Unmarshal(root["model"], &result.Model); err != nil || result.Model == "" {
		return nil, protocolField(meta, "model", "required nonempty string")
	}

	usage, err := parseUsage(root["usage"], meta)
	if err != nil {
		return nil, err
	}

	answers, err := parseAnswers(root["answers"], requested, meta)
	if err != nil {
		return nil, err
	}

	result.Usage = usage
	result.Answers = answers

	return &result, nil
}

func parseUsage(raw json.RawMessage, meta HTTPMetadata) (Usage, error) {
	var fields map[string]json.RawMessage

	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return Usage{}, protocolField(meta, "usage", "required object")
	}

	var usage Usage

	for name, destination := range map[string]*int64{"input_tokens": &usage.InputTokens, "output_tokens": &usage.OutputTokens} {
		raw := fields[name]
		if len(raw) == 0 || string(raw) == "null" {
			return Usage{}, protocolField(meta, "usage."+name, "required integer")
		}

		if err := json.Unmarshal(raw, destination); err != nil || *destination < 0 {
			return Usage{}, protocolField(meta, "usage."+name, "required nonnegative integer")
		}
	}

	return usage, nil
}

func parseAnswers(raw json.RawMessage, requested map[string]string, meta HTTPMetadata) (map[string]Answer, error) {
	var fields map[string]json.RawMessage

	if err := json.Unmarshal(raw, &fields); err != nil || len(fields) == 0 {
		return nil, protocolField(meta, "answers", "required nonempty object")
	}

	answers := make(map[string]Answer, len(fields))
	for name, raw := range fields {
		answer, err := parseAnswer(raw, "answers."+name, requested[name], meta)
		if err != nil {
			return nil, err
		}

		answers[name] = answer
	}

	for name := range requested {
		if _, ok := answers[name]; !ok {
			return nil, protocolField(meta, "answers."+name, "requested answer is missing")
		}
	}

	return answers, nil
}

func parseAnswer(raw json.RawMessage, field, expected string, meta HTTPMetadata) (Answer, error) {
	object, kind, err := parseAnswerEnvelope(raw, field, expected, meta)
	if err != nil {
		return nil, err
	}

	switch kind {
	case "noul":
		return parseNoul(object, field, meta)
	case "choice":
		return parseChoiceAnswer(object, field, meta)
	case "score":
		return parseScoreAnswer(object, field, meta)
	case "box":
		return parseBox(object, field, meta)
	default:
		return UnknownAnswer{Type: kind, Raw: append(json.RawMessage(nil), raw...)}, nil
	}
}

func parseNoul(object map[string]json.RawMessage, field string, meta HTTPMetadata) (NoulAnswer, error) {
	var value *float64

	if err := json.Unmarshal(object["noul"], &value); err != nil || value == nil || *value < 0 || *value > 1 {
		return NoulAnswer{}, protocolField(meta, field+".noul", "required probability from 0 to 1")
	}

	return NoulAnswer{Probability: *value}, nil
}

func parseAnswerEnvelope(raw json.RawMessage, field, expected string, meta HTTPMetadata) (map[string]json.RawMessage, string, error) {
	var object map[string]json.RawMessage

	if err := json.Unmarshal(raw, &object); err != nil || object == nil {
		return nil, "", protocolField(meta, field, "required object")
	}

	var kind string

	if err := json.Unmarshal(object["type"], &kind); err != nil || kind == "" {
		return nil, "", protocolField(meta, field+".type", "required nonempty discriminator")
	}

	if expected != "" && expected != kind {
		return nil, "", protocolField(meta, field+".type", "does not match requested question")
	}

	return object, kind, nil
}
