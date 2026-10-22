package typesafe

import (
	"encoding/json"
	"math"
	"reflect"
	"strconv"
)

func protocolField(meta HTTPMetadata, field, message string) error {
	return &ProtocolError{HTTPMetadata: meta, Field: field, Message: message}
}

func requiredNumber(raw json.RawMessage, field string, meta HTTPMetadata, upperBound float64) (float64, error) {
	var value *float64

	if err := json.Unmarshal(raw, &value); err != nil || value == nil || math.IsInf(*value, 0) || math.IsNaN(*value) || *value < 0 || *value > upperBound {
		return 0, protocolField(meta, field, "required finite number in range")
	}

	return *value, nil
}

func parseProbabilities(raw json.RawMessage, field string, meta HTTPMetadata) (map[string]float64, error) {
	var object map[string]json.RawMessage

	if err := json.Unmarshal(raw, &object); err != nil || len(object) == 0 {
		return nil, protocolField(meta, field, "required nonempty probability object")
	}

	result := make(map[string]float64, len(object))
	for name, raw := range object {
		value, err := requiredNumber(raw, field+"."+name, meta, 1)
		if err != nil {
			return nil, err
		}

		result[name] = value
	}

	return result, nil
}

func parseChoiceAnswer(object map[string]json.RawMessage, field string, meta HTTPMetadata) (ChoiceAnswer, error) {
	var (
		answer ChoiceAnswer
		choice *string
	)

	if err := json.Unmarshal(object["choice"], &choice); err != nil || choice == nil {
		return answer, protocolField(meta, field+".choice", "required string")
	}

	answer.Choice = *choice

	var err error

	answer.Confidence, err = requiredNumber(object["confidence"], field+".confidence", meta, 1)
	if err != nil {
		return answer, err
	}

	answer.Probabilities, err = parseProbabilities(object["probabilities"], field+".probabilities", meta)
	if err != nil {
		return answer, err
	}

	if len(answer.Probabilities) > 255 {
		return answer, protocolField(meta, field+".probabilities", "Choice supports at most 255 alternatives")
	}

	if _, ok := answer.Probabilities[answer.Choice]; !ok {
		return answer, protocolField(meta, field+".choice", "selected alternative is absent from probabilities")
	}

	return answer, nil
}

func parseScoreAnswer(object map[string]json.RawMessage, field string, meta HTTPMetadata) (ScoreAnswer, error) {
	var (
		answer ScoreAnswer
		err    error
	)

	answer.Confidence, err = requiredNumber(object["confidence"], field+".confidence", meta, 1)
	if err != nil {
		return answer, err
	}

	answer.Legend, err = parseScoreLegend(object["legend"], field, meta)
	if err != nil {
		return answer, err
	}

	answer.Score, err = requiredNumber(object["score"], field+".score", meta, float64(len(answer.Legend)-1))
	if err != nil {
		return answer, err
	}

	answer.Probabilities, err = parseScoreProbabilities(object["probabilities"], answer.Legend, field, meta)

	return answer, err
}

func parseScoreLegend(raw json.RawMessage, field string, meta HTTPMetadata) (map[int]any, error) {
	var fields map[string]json.RawMessage

	if err := json.Unmarshal(raw, &fields); err != nil || len(fields) < 1 || len(fields) > 10 {
		return nil, protocolField(meta, field+".legend", "required rubric with 1–10 levels")
	}

	legend := make(map[int]any, len(fields))
	for name, raw := range fields {
		index, description, err := parseScoreLegendLevel(name, raw, field, meta)
		if err != nil {
			return nil, err
		}

		legend[index] = description
	}

	for i := range len(fields) {
		if _, ok := legend[i]; !ok {
			return nil, protocolField(meta, field+".legend", "levels must be contiguous starting at zero")
		}
	}

	return legend, nil
}

func parseScoreDescription(raw json.RawMessage, field string, meta HTTPMetadata) (any, error) {
	if _, err := encodeContent(raw, field, false); err != nil {
		return nil, protocolField(meta, field, "expected string, object or array")
	}

	var description any

	if err := json.Unmarshal(raw, &description); err != nil {
		return nil, protocolField(meta, field, "invalid rubric description")
	}

	return description, nil
}

func parseScoreProbabilities(raw json.RawMessage, legend map[int]any, field string, meta HTTPMetadata) (map[int]float64, error) {
	probabilities, err := parseProbabilities(raw, field+".probabilities", meta)
	if err != nil {
		return nil, err
	}

	if len(probabilities) != len(legend) {
		return nil, protocolField(meta, field+".probabilities", "levels must match legend")
	}

	result := make(map[int]float64, len(probabilities))
	for name, value := range probabilities {
		index, err := strconv.Atoi(name)
		if err != nil || strconv.Itoa(index) != name {
			return nil, protocolField(meta, field+".probabilities."+name, "expected a zero-based integer level")
		}

		if _, ok := legend[index]; !ok {
			return nil, protocolField(meta, field+".probabilities", "levels must match legend")
		}

		result[index] = value
	}

	return result, nil
}

// Compare the answers with encoded question snapshots, rather than invoking
// caller marshalers again or comparing unnormalized application values.
func validateAnsweredQuestions(response *SystemOneResponse, questions map[string]json.RawMessage) error {
	for name, raw := range questions {
		field := "answers." + name

		var question struct {
			Criteria json.RawMessage `json:"criteria"`
		}

		if err := json.Unmarshal(raw, &question); err != nil {
			return protocolField(response.HTTPMetadata, field, "invalid question snapshot")
		}

		if err := validateAnswerCriteria(response.Answers[name], question.Criteria, field, response.HTTPMetadata); err != nil {
			return err
		}
	}

	return nil
}

func validateAnswerCriteria(answer Answer, criteria json.RawMessage, field string, meta HTTPMetadata) error {
	switch answer := answer.(type) {
	case ChoiceAnswer:
		return validateChoiceCriteria(answer, criteria, field, meta)
	case ScoreAnswer:
		return validateScoreCriteria(answer, criteria, field, meta)
	default:
		return nil
	}
}

func validateChoiceCriteria(answer ChoiceAnswer, raw json.RawMessage, field string, meta HTTPMetadata) error {
	var criteria map[string]json.RawMessage

	if err := json.Unmarshal(raw, &criteria); err != nil {
		return protocolField(meta, field+".probabilities", "invalid Choice criteria")
	}

	if len(criteria) != len(answer.Probabilities) {
		return protocolField(meta, field+".probabilities", "alternatives must match requested criteria")
	}

	for key := range criteria {
		if _, ok := answer.Probabilities[key]; !ok {
			return protocolField(meta, field+".probabilities", "alternatives must match requested criteria")
		}
	}

	return nil
}

func validateScoreCriteria(answer ScoreAnswer, raw json.RawMessage, field string, meta HTTPMetadata) error {
	var criteria []any

	if err := json.Unmarshal(raw, &criteria); err != nil {
		return protocolField(meta, field+".legend", "invalid Score criteria")
	}

	if len(criteria) != len(answer.Legend) {
		return protocolField(meta, field+".legend", "levels must match requested criteria")
	}

	for index, description := range criteria {
		if !reflect.DeepEqual(description, answer.Legend[index]) {
			return protocolField(meta, field+".legend", "legend must preserve requested criteria")
		}
	}

	return nil
}

func parseScoreLegendLevel(name string, raw json.RawMessage, field string, meta HTTPMetadata) (int, any, error) {
	index, err := strconv.Atoi(name)
	if err != nil || index < 0 || strconv.Itoa(index) != name {
		return 0, nil, protocolField(meta, field+".legend."+name, "expected a zero-based integer level")
	}

	description, err := parseScoreDescription(raw, field+".legend."+name, meta)

	return index, description, err
}
