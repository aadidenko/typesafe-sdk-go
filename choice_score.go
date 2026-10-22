package typesafe

import (
	"encoding/json"
	"fmt"
)

// ChoiceQuestion selects among 1–255 named alternatives. A nil description
// leaves an alternative to be interpreted by its name alone.
type ChoiceQuestion struct {
	Instructions any
	Criteria     map[string]any
}

func (q ChoiceQuestion) encodeQuestion() (string, json.RawMessage, error) {
	if len(q.Criteria) < 1 || len(q.Criteria) > 255 {
		return "", nil, &ValidationError{"criteria", "Choice requires 1–255 alternatives"}
	}

	instructions, err := encodeContent(q.Instructions, "instructions", true)
	if err != nil {
		return "", nil, err
	}

	criteria := make(map[string]json.RawMessage, len(q.Criteria))
	for name, value := range q.Criteria {
		encoded, err := encodeContent(value, "criteria."+name, true)
		if err != nil {
			return "", nil, err
		}

		criteria[name] = encoded
	}

	body, err := json.Marshal(struct {
		Type         string                     `json:"type"`
		Instructions json.RawMessage            `json:"instructions,omitempty"`
		Criteria     map[string]json.RawMessage `json:"criteria"`
	}{"choice", instructions, criteria})

	return "choice", body, err
}

// ScoreQuestion rates content against 1–10 ordered levels, indexed from zero.
type ScoreQuestion struct {
	Instructions any
	Criteria     []any
}

func (q ScoreQuestion) encodeQuestion() (string, json.RawMessage, error) {
	if len(q.Criteria) < 1 || len(q.Criteria) > 10 {
		return "", nil, &ValidationError{"criteria", "Score requires 1–10 levels"}
	}

	instructions, err := encodeContent(q.Instructions, "instructions", true)
	if err != nil {
		return "", nil, err
	}

	criteria := make([]json.RawMessage, len(q.Criteria))
	for i, value := range q.Criteria {
		encoded, err := encodeContent(value, fmt.Sprintf("criteria[%d]", i), false)
		if err != nil {
			return "", nil, err
		}

		criteria[i] = encoded
	}

	body, err := json.Marshal(struct {
		Type         string            `json:"type"`
		Instructions json.RawMessage   `json:"instructions,omitempty"`
		Criteria     []json.RawMessage `json:"criteria"`
	}{"score", instructions, criteria})

	return "score", body, err
}

// ChoiceAnswer retains the selection, its Confidence, and all class probabilities.
type ChoiceAnswer struct {
	Choice        string             `json:"choice"`
	Confidence    float64            `json:"confidence"`
	Probabilities map[string]float64 `json:"probabilities"`
}

func (ChoiceAnswer) Kind() string { return "choice" }

// ScoreAnswer retains a fractional expected score and its ordered rubric.
// Legend and Probabilities keys are zero-based integer level indices.
type ScoreAnswer struct {
	Score         float64         `json:"score"`
	Confidence    float64         `json:"confidence"`
	Legend        map[int]any     `json:"legend"`
	Probabilities map[int]float64 `json:"probabilities"`
}

func (ScoreAnswer) Kind() string { return "score" }

func (r *SystemOneResponse) Choice(name string) (ChoiceAnswer, error) {
	answer, ok := r.Answers[name]
	if !ok {
		return ChoiceAnswer{}, fmt.Errorf("%w: %s", ErrAnswerMissing, name)
	}

	choice, ok := answer.(ChoiceAnswer)
	if !ok {
		return ChoiceAnswer{}, fmt.Errorf("%w: %s", ErrAnswerKind, name)
	}

	return choice, nil
}

func (r *SystemOneResponse) Score(name string) (ScoreAnswer, error) {
	answer, ok := r.Answers[name]
	if !ok {
		return ScoreAnswer{}, fmt.Errorf("%w: %s", ErrAnswerMissing, name)
	}

	score, ok := answer.(ScoreAnswer)
	if !ok {
		return ScoreAnswer{}, fmt.Errorf("%w: %s", ErrAnswerKind, name)
	}

	return score, nil
}

// AnswersByKind returns a new named collection with the requested discriminator.
// It also works for future kinds retained as UnknownAnswer.
func (r *SystemOneResponse) AnswersByKind(kind string) map[string]Answer {
	result := make(map[string]Answer)

	for name, answer := range r.Answers {
		if answer.Kind() == kind {
			result[name] = answer
		}
	}

	return result
}
