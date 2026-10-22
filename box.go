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
	}{Type: "box", Instructions: instructions})

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

// Kind identifies a Box Answer.
func (BoxAnswer) Kind() string { return "box" }

// Box returns the named Box Answer, or an error for a missing name or different kind.
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
	box, err := parseBoxCoordinates(object["box"], field, meta)
	if err != nil {
		return BoxAnswer{}, err
	}

	confidence, err := parseBoxConfidence(object, field, meta)
	if err != nil {
		return BoxAnswer{}, err
	}

	return BoxAnswer{Box: box, Confidence: confidence}, nil
}

func parseBoxCoordinates(raw json.RawMessage, field string, meta HTTPMetadata) (Box, error) {
	var coordinates map[string]json.RawMessage

	if err := json.Unmarshal(raw, &coordinates); err != nil || coordinates == nil {
		return Box{}, protocolField(meta, field+".box", "required normalized number from 0 to 1")
	}

	var box Box

	for name, destination := range map[string]*float64{"top": &box.Top, "bottom": &box.Bottom, "left": &box.Left, "right": &box.Right} {
		var value *float64

		if err := json.Unmarshal(coordinates[name], &value); err != nil || value == nil || *value < 0 || *value > 1 {
			return Box{}, protocolField(meta, field+".box."+name, "required normalized number from 0 to 1")
		}

		*destination = *value
	}

	return box, nil
}

func parseBoxConfidence(object map[string]json.RawMessage, field string, meta HTTPMetadata) (*float64, error) {
	raw, exists := object["confidence"]
	if !exists {
		return nil, protocolField(meta, field+".confidence", "required normalized number from 0 to 1")
	}

	var confidence *float64

	if err := json.Unmarshal(raw, &confidence); err != nil || (confidence != nil && (*confidence < 0 || *confidence > 1)) {
		return nil, protocolField(meta, field+".confidence", "required normalized number from 0 to 1")
	}

	return confidence, nil
}
