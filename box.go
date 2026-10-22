package typesafe

import (
	"encoding/json"
	"fmt"
)

// BoxQuestion locates the target described by Instructions in an image.
// Experimental image support must be explicitly enabled for the call.
type BoxQuestion struct{ Instructions any }

func (q BoxQuestion) encodeQuestion() (string, json.RawMessage, error) {
	instructions, err := encodeContent(q.Instructions, "instructions", true)
	if err != nil {
		return "", nil, err
	}
	body, err := json.Marshal(struct {
		Type         string          `json:"type"`
		Instructions json.RawMessage `json:"instructions,omitempty"`
	}{"box", instructions})
	return "box", body, err
}

// Box contains edge positions normalized to image height and width.
type Box struct {
	Top    float64 `json:"top"`
	Bottom float64 `json:"bottom"`
	Left   float64 `json:"left"`
	Right  float64 `json:"right"`
}

// BoxAnswer locates a target. A nil Confidence means the model supplied null.
type BoxAnswer struct {
	Box        Box      `json:"box"`
	Confidence *float64 `json:"confidence"`
}

func (BoxAnswer) Kind() string { return "box" }

func (r *SystemOneResponse) Box(name string) (BoxAnswer, error) {
	value, ok := r.Answers[name]
	if !ok {
		return BoxAnswer{}, fmt.Errorf("%w: %s", ErrAnswerMissing, name)
	}
	box, ok := value.(BoxAnswer)
	if !ok {
		return BoxAnswer{}, fmt.Errorf("%w: %s", ErrAnswerKind, name)
	}
	return box, nil
}

func parseBox(object map[string]json.RawMessage, field string, meta HTTPMetadata) (BoxAnswer, error) {
	fail := func(path string) (BoxAnswer, error) {
		return BoxAnswer{}, &ProtocolError{meta, field + path, "required normalized number from 0 to 1"}
	}
	var coordinates map[string]json.RawMessage
	if err := json.Unmarshal(object["box"], &coordinates); err != nil || coordinates == nil {
		return fail(".box")
	}
	var result BoxAnswer
	for name, dest := range map[string]*float64{"top": &result.Box.Top, "bottom": &result.Box.Bottom, "left": &result.Box.Left, "right": &result.Box.Right} {
		var value *float64
		if err := json.Unmarshal(coordinates[name], &value); err != nil || value == nil || *value < 0 || *value > 1 {
			return fail(".box." + name)
		}
		*dest = *value
	}
	raw, exists := object["confidence"]
	if !exists {
		return fail(".confidence")
	}
	if err := json.Unmarshal(raw, &result.Confidence); err != nil || (result.Confidence != nil && (*result.Confidence < 0 || *result.Confidence > 1)) {
		return fail(".confidence")
	}
	return result, nil
}
