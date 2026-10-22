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

func requiredNumber(raw json.RawMessage, field string, meta HTTPMetadata, min, max float64) (float64, error) {
	var value *float64

	if err := json.Unmarshal(raw, &value); err != nil || value == nil || math.IsInf(*value, 0) || math.IsNaN(*value) || *value < min || *value > max {
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
		value, err := requiredNumber(raw, field+"."+name, meta, 0, 1)
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

	answer.Confidence, err = requiredNumber(object["confidence"], field+".confidence", meta, 0, 1)
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

	answer.Confidence, err = requiredNumber(object["confidence"], field+".confidence", meta, 0, 1)
	if err != nil {
		return answer, err
	}

	var legend map[string]json.RawMessage

	if err := json.Unmarshal(object["legend"], &legend); err != nil || len(legend) < 1 || len(legend) > 10 {
		return answer, protocolField(meta, field+".legend", "required rubric with 1–10 levels")
	}

	answer.Legend = make(map[int]any, len(legend))
	for name, raw := range legend {
		index, err := strconv.Atoi(name)
		if err != nil || index < 0 || strconv.Itoa(index) != name {
			return answer, protocolField(meta, field+".legend."+name, "expected a zero-based integer level")
		}

		if _, err := encodeContent(raw, field+".legend."+name, false); err != nil {
			return answer, protocolField(meta, field+".legend."+name, "expected string, object or array")
		}

		var description any

		if err := json.Unmarshal(raw, &description); err != nil {
			return answer, protocolField(meta, field+".legend."+name, "invalid rubric description")
		}

		answer.Legend[index] = description
	}

	for i := range len(legend) {
		if _, ok := answer.Legend[i]; !ok {
			return answer, protocolField(meta, field+".legend", "levels must be contiguous starting at zero")
		}
	}

	answer.Score, err = requiredNumber(object["score"], field+".score", meta, 0, float64(len(legend)-1))
	if err != nil {
		return answer, err
	}

	probabilities, err := parseProbabilities(object["probabilities"], field+".probabilities", meta)
	if err != nil {
		return answer, err
	}

	if len(probabilities) != len(legend) {
		return answer, protocolField(meta, field+".probabilities", "levels must match legend")
	}

	answer.Probabilities = make(map[int]float64, len(probabilities))
	for name, value := range probabilities {
		index, err := strconv.Atoi(name)
		if err != nil || strconv.Itoa(index) != name {
			return answer, protocolField(meta, field+".probabilities."+name, "expected a zero-based integer level")
		}

		if _, ok := answer.Legend[index]; !ok {
			return answer, protocolField(meta, field+".probabilities", "levels must match legend")
		}

		answer.Probabilities[index] = value
	}

	return answer, nil
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

		switch answer := response.Answers[name].(type) {
		case ChoiceAnswer:
			var criteria map[string]json.RawMessage

			if err := json.Unmarshal(question.Criteria, &criteria); err != nil {
				return protocolField(response.HTTPMetadata, field+".probabilities", "invalid Choice criteria")
			}

			if len(criteria) != len(answer.Probabilities) {
				return protocolField(response.HTTPMetadata, field+".probabilities", "alternatives must match requested criteria")
			}

			for key := range criteria {
				if _, ok := answer.Probabilities[key]; !ok {
					return protocolField(response.HTTPMetadata, field+".probabilities", "alternatives must match requested criteria")
				}
			}
		case ScoreAnswer:
			var criteria []any

			if err := json.Unmarshal(question.Criteria, &criteria); err != nil {
				return protocolField(response.HTTPMetadata, field+".legend", "invalid Score criteria")
			}

			if len(criteria) != len(answer.Legend) {
				return protocolField(response.HTTPMetadata, field+".legend", "levels must match requested criteria")
			}

			for index, description := range criteria {
				if !reflect.DeepEqual(description, answer.Legend[index]) {
					return protocolField(response.HTTPMetadata, field+".legend", "legend must preserve requested criteria")
				}
			}
		}
	}

	return nil
}
