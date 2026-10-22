package typesafe_test

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"sync/atomic"
	"testing"

	typesafe "github.com/aadidenko/typesafe-sdk-go"
)

const mixedResponse = `{"model":"actual","answers":{"billing":{"type":"noul","noul":0.98},"tone":{"type":"choice","choice":"calm","confidence":0.7,"probabilities":{"angry":0.15,"calm":0.8,"excited":0.05}},"urgency":{"type":"score","score":1.7,"confidence":0.55,"legend":{"0":"Can wait","1":{"when":"This week","nested":[null]},"2":["Today",null]},"probabilities":{"0":0.1,"1":0.1,"2":0.8}}},"usage":{"input_tokens":120,"output_tokens":12}}`

func mixedRequest() typesafe.SystemOneRequest {
	return typesafe.SystemOneRequest{State: struct {
		Message string `json:"message"`
	}{Message: "Duplicate charge"}, Questions: typesafe.Questions{
		"billing": typesafe.NoulQuestion{Instructions: "Billing?"},
		"tone":    typesafe.ChoiceQuestion{Instructions: map[string]any{"task": []any{"Classify", nil}}, Criteria: map[string]any{"angry": "An upset message", "calm": nil, "excited": []any{"Enthusiasm", nil}}},
		"urgency": typesafe.ScoreQuestion{Instructions: []any{"How urgent?", nil}, Criteria: []any{"Can wait", map[string]any{"when": "This week", "nested": []any{nil}}, []any{"Today", nil}}},
	}}
}

func TestMixedNoulChoiceScore(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !assertMixedRequest(t, r) {
			return
		}

		fixtureWrite(t, w, []byte(mixedResponse))
	}))

	defer server.Close()

	client := mustClient(t, typesafe.Config{APIKey: "test", BaseURL: server.URL})

	response, err := client.SystemOne(context.Background(), mixedRequest())
	if err != nil {
		t.Fatal(err)
	}

	assertMixedAnswers(t, response)
}

func assertMixedRequest(t *testing.T, r *http.Request) bool {
	t.Helper()

	var body struct {
		State     map[string]any `json:"state"`
		Questions map[string]struct {
			Type     string `json:"type"`
			Criteria any    `json:"criteria"`
		} `json:"questions"`
	}

	if !fixtureDecode(t, r.Body, &body) {
		return false
	}

	if body.State["message"] != "Duplicate charge" || len(body.Questions) != 3 {
		t.Errorf("state/questions=%+v", body)
	}

	for name, kind := range map[string]string{"billing": "noul", "tone": "choice", "urgency": "score"} {
		if body.Questions[name].Type != kind {
			t.Errorf("%s kind=%v", name, body.Questions[name].Type)
		}
	}

	criteria, ok := body.Questions["tone"].Criteria.(map[string]any)
	if !ok {
		t.Error("Choice criteria is not an object")

		return false
	}

	if value, ok := criteria["calm"]; !ok || value != nil {
		t.Error("undescribed choice lost")
	}

	if !reflect.DeepEqual(body.Questions["urgency"].Criteria, []any{"Can wait", map[string]any{"when": "This week", "nested": []any{nil}}, []any{"Today", nil}}) {
		t.Error("structured score levels lost")
	}

	return true
}

func assertMixedAnswers(t *testing.T, response *typesafe.SystemOneResponse) {
	t.Helper()

	choice, err := response.Choice("tone")
	if err != nil {
		t.Fatal(err)
	}

	if choice.Choice != "calm" || choice.Confidence != .7 || choice.Probabilities["calm"] != .8 {
		t.Fatalf("choice=%+v", choice)
	}

	score, err := response.Score("urgency")
	if err != nil {
		t.Fatal(err)
	}

	if score.Score != 1.7 || score.Confidence != .55 || score.Probabilities[2] != .8 || score.Legend[0] != "Can wait" {
		t.Fatalf("score=%+v", score)
	}

	assertMixedAnswerKinds(t, response)
}

func assertMixedAnswerKinds(t *testing.T, response *typesafe.SystemOneResponse) {
	t.Helper()

	for kind, count := range map[string]int{"noul": 1, "choice": 1, "score": 1, "box": 0} {
		if got := len(response.AnswersByKind(kind)); got != count {
			t.Errorf("answer kind %s count=%d want %d", kind, got, count)
		}
	}
}

func TestChoiceScoreValidationBeforeHTTP(t *testing.T) {
	var traffic atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		traffic.Add(1)
		fixtureWrite(t, w, []byte(mixedResponse))
	}))

	defer server.Close()

	client := mustClient(t, typesafe.Config{APIKey: "test", BaseURL: server.URL})
	tooManyChoices := make(map[string]any)

	for i := range 256 {
		tooManyChoices[strconv.Itoa(i)] = nil
	}

	tooManyLevels := make([]any, 11)
	for i := range tooManyLevels {
		tooManyLevels[i] = "level"
	}

	cases := []typesafe.Question{
		typesafe.ChoiceQuestion{},
		typesafe.ChoiceQuestion{Criteria: tooManyChoices},
		typesafe.ChoiceQuestion{Criteria: map[string]any{"invalid": true}},
		typesafe.ChoiceQuestion{Criteria: map[string]any{"invalid": map[string]any{"value": math.Inf(1)}}},
		typesafe.ChoiceQuestion{Instructions: 42, Criteria: map[string]any{"ok": nil}},
		typesafe.ScoreQuestion{},
		typesafe.ScoreQuestion{Criteria: tooManyLevels},
		typesafe.ScoreQuestion{Criteria: []any{nil}},
		typesafe.ScoreQuestion{Criteria: []any{42}},
		typesafe.ScoreQuestion{Criteria: []any{[]any{func() {}}}},
		typesafe.ScoreQuestion{Instructions: false, Criteria: []any{"level"}},
	}
	for i, question := range cases {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			_, err := client.SystemOne(context.Background(), typesafe.SystemOneRequest{State: "state", Questions: typesafe.Questions{"test": question}})
			if !errors.Is(err, typesafe.ErrValidation) {
				t.Fatalf("expected validation error, got %v", err)
			}
		})
	}

	if traffic.Load() != 0 {
		t.Fatal("invalid questions reached HTTP")
	}
}

func TestChoiceAndScoreRejectInvalidAnswers(t *testing.T) {
	cases := []struct{ name, kind, answer, path string }{
		{name: "choice missing selection", kind: "choice", answer: `{"type":"choice","confidence":0.7,"probabilities":{"a":1}}`, path: "choice"},
		{name: "choice null selection", kind: "choice", answer: `{"type":"choice","choice":null,"confidence":0.7,"probabilities":{"a":1}}`, path: "choice"},
		{name: "choice missing confidence", kind: "choice", answer: `{"type":"choice","choice":"a","probabilities":{"a":1}}`, path: "confidence"},
		{name: "choice null confidence", kind: "choice", answer: `{"type":"choice","choice":"a","confidence":null,"probabilities":{"a":1}}`, path: "confidence"},
		{name: "choice confidence range", kind: "choice", answer: `{"type":"choice","choice":"a","confidence":1.1,"probabilities":{"a":1}}`, path: "confidence"},
		{name: "choice missing probabilities", kind: "choice", answer: `{"type":"choice","choice":"a","confidence":0.7}`, path: "probabilities"},
		{name: "choice empty probabilities", kind: "choice", answer: `{"type":"choice","choice":"a","confidence":0.7,"probabilities":{}}`, path: "probabilities"},
		{name: "choice null probability", kind: "choice", answer: `{"type":"choice","choice":"a","confidence":0.7,"probabilities":{"a":null}}`, path: "probabilities.a"},
		{name: "choice probability string", kind: "choice", answer: `{"type":"choice","choice":"a","confidence":0.7,"probabilities":{"a":"1"}}`, path: "probabilities.a"},
		{name: "choice probability range", kind: "choice", answer: `{"type":"choice","choice":"a","confidence":0.7,"probabilities":{"a":-1}}`, path: "probabilities.a"},
		{name: "choice selection missing from probabilities", kind: "choice", answer: `{"type":"choice","choice":"b","confidence":0.7,"probabilities":{"a":1}}`, path: "choice"},
		{name: "choice alternatives differ from question", kind: "choice", answer: `{"type":"choice","choice":"b","confidence":0.7,"probabilities":{"b":1}}`, path: "probabilities"},
		{name: "score missing value", kind: "score", answer: `{"type":"score","confidence":0.7,"legend":{"0":"Only"},"probabilities":{"0":1}}`, path: "score"},
		{name: "score null value", kind: "score", answer: `{"type":"score","score":null,"confidence":0.7,"legend":{"0":"Only"},"probabilities":{"0":1}}`, path: "score"},
		{name: "score missing confidence", kind: "score", answer: `{"type":"score","score":0,"legend":{"0":"Only"},"probabilities":{"0":1}}`, path: "confidence"},
		{name: "score negative confidence", kind: "score", answer: `{"type":"score","score":0,"confidence":-0.1,"legend":{"0":"Only"},"probabilities":{"0":1}}`, path: "confidence"},
		{name: "score missing legend", kind: "score", answer: `{"type":"score","score":0,"confidence":0.7,"probabilities":{"0":1}}`, path: "legend"},
		{name: "score empty legend", kind: "score", answer: `{"type":"score","score":0,"confidence":0.7,"legend":{},"probabilities":{"0":1}}`, path: "legend"},
		{name: "score null level", kind: "score", answer: `{"type":"score","score":0,"confidence":0.7,"legend":{"0":null},"probabilities":{"0":1}}`, path: "legend.0"},
		{name: "score scalar level", kind: "score", answer: `{"type":"score","score":0,"confidence":0.7,"legend":{"0":42},"probabilities":{"0":1}}`, path: "legend.0"},
		{name: "score noninteger level", kind: "score", answer: `{"type":"score","score":0,"confidence":0.7,"legend":{"first":"Only"},"probabilities":{"first":1}}`, path: "legend.first"},
		{name: "score noncanonical level", kind: "score", answer: `{"type":"score","score":0,"confidence":0.7,"legend":{"00":"Only"},"probabilities":{"00":1}}`, path: "legend.00"},
		{name: "score levels start at zero", kind: "score", answer: `{"type":"score","score":0,"confidence":0.7,"legend":{"1":"Only"},"probabilities":{"1":1}}`, path: "legend"},
		{name: "score missing probabilities", kind: "score", answer: `{"type":"score","score":0,"confidence":0.7,"legend":{"0":"Only"}}`, path: "probabilities"},
		{name: "score null probability", kind: "score", answer: `{"type":"score","score":0,"confidence":0.7,"legend":{"0":"Only"},"probabilities":{"0":null}}`, path: "probabilities.0"},
		{name: "score probability too high", kind: "score", answer: `{"type":"score","score":0,"confidence":0.7,"legend":{"0":"Only"},"probabilities":{"0":1.1}}`, path: "probabilities.0"},
		{name: "score probability keys differ", kind: "score", answer: `{"type":"score","score":0,"confidence":0.7,"legend":{"0":"Only"},"probabilities":{"1":1}}`, path: "probabilities"},
		{name: "score outside rubric", kind: "score", answer: `{"type":"score","score":1,"confidence":0.7,"legend":{"0":"Only"},"probabilities":{"0":1}}`, path: "score"},
		{name: "score negative value", kind: "score", answer: `{"type":"score","score":-0.1,"confidence":0.7,"legend":{"0":"Only"},"probabilities":{"0":1}}`, path: "score"},
		{name: "score legend differs from question", kind: "score", answer: `{"type":"score","score":0,"confidence":0.7,"legend":{"0":"Different"},"probabilities":{"0":1}}`, path: "legend"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			body := `{"model":"actual","answers":{"rating":` + test.answer + `},"usage":{"input_tokens":1,"output_tokens":1}}`
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("x-typesafe-request-id", "invalid-answer")
				fixtureWrite(t, w, []byte(body))
			}))

			defer server.Close()

			client := mustClient(t, typesafe.Config{APIKey: "test", BaseURL: server.URL})

			var question typesafe.Question = typesafe.ChoiceQuestion{Criteria: map[string]any{"a": nil}}

			if test.kind == "score" {
				question = typesafe.ScoreQuestion{Criteria: []any{"Only"}}
			}

			_, err := client.SystemOne(context.Background(), typesafe.SystemOneRequest{State: "state", Questions: typesafe.Questions{"rating": question}})

			var protocol *typesafe.ProtocolError

			if !errors.As(err, &protocol) || !errors.Is(err, typesafe.ErrProtocol) {
				t.Fatalf("expected protocol error, got %v", err)
			}

			if protocol.Field != "answers.rating."+test.path || string(protocol.RawBody) != body || protocol.RequestID != "invalid-answer" {
				t.Fatalf("protocol=%+v", protocol)
			}
		})
	}
}

func TestIndividualChoiceAndScoreBoundaryCardinalities(t *testing.T) {
	for _, kind := range []string{"choice", "score"} {
		maximum := 255
		if kind == "score" {
			maximum = 10
		}

		for _, count := range []int{1, maximum} {
			t.Run(kind+"/"+strconv.Itoa(count), func(t *testing.T) { testQuestionBoundary(t, kind, count) })
		}
	}
}

func boundaryQuestion(kind string, count int) typesafe.Question {
	if kind == "choice" {
		criteria := make(map[string]any, count)
		for i := range count {
			criteria[strconv.Itoa(i)] = nil
		}

		return typesafe.ChoiceQuestion{Criteria: criteria}
	}

	criteria := make([]any, count)
	for i := range count {
		criteria[i] = map[string]any{"level": i, "nested": []any{nil}}
	}

	return typesafe.ScoreQuestion{Criteria: criteria}
}

func testQuestionBoundary(t *testing.T, kind string, count int) {
	t.Helper()

	question := boundaryQuestion(kind, count)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { serveBoundaryAnswer(t, w, r, kind, count) }))

	defer server.Close()

	client := mustClient(t, typesafe.Config{APIKey: "test", BaseURL: server.URL, Compression: typesafe.CompressionNone})

	response, err := client.SystemOne(context.Background(), typesafe.SystemOneRequest{State: "state", Questions: typesafe.Questions{"rating": question}})
	if err != nil {
		t.Fatal(err)
	}

	assertQuestionBoundary(t, response, kind, count)
}

func serveBoundaryAnswer(t *testing.T, w http.ResponseWriter, r *http.Request, kind string, count int) {
	t.Helper()

	var body struct {
		Questions map[string]struct {
			Type     string          `json:"type"`
			Criteria json.RawMessage `json:"criteria"`
		} `json:"questions"`
	}

	if !fixtureDecode(t, r.Body, &body) {
		return
	}

	answer, ok := boundaryAnswer(t, kind, count, body.Questions["rating"].Criteria)
	if !ok {
		return
	}

	probabilities := make(map[string]float64, count)
	for i := range count {
		probabilities[strconv.Itoa(i)] = 1 / float64(count)
	}

	answer["probabilities"] = probabilities
	writeQuestionResponse(t, w, answer)
}

func boundaryAnswer(t *testing.T, kind string, count int, raw json.RawMessage) (map[string]any, bool) {
	t.Helper()

	answer := map[string]any{"type": kind, "confidence": 0.3}
	if kind == "choice" {
		var criteria map[string]any

		if err := json.Unmarshal(raw, &criteria); err != nil {
			t.Error(err)

			return nil, false
		}

		if len(criteria) != count {
			t.Errorf("criteria count=%d", len(criteria))
		}

		answer["choice"] = "0"

		return answer, true
	}

	legend, ok := boundaryLegend(t, raw, count)
	if !ok {
		return nil, false
	}

	answer["legend"] = legend
	answer["score"] = float64(count-1) / 2

	return answer, true
}

func boundaryLegend(t *testing.T, raw json.RawMessage, count int) (map[string]any, bool) {
	t.Helper()

	var criteria []any

	if err := json.Unmarshal(raw, &criteria); err != nil {
		t.Error(err)

		return nil, false
	}

	if len(criteria) != count {
		t.Errorf("criteria count=%d", len(criteria))
	}

	legend := make(map[string]any, count)
	for i, value := range criteria {
		legend[strconv.Itoa(i)] = value
	}

	return legend, true
}

func writeQuestionResponse(t *testing.T, w http.ResponseWriter, answer map[string]any) {
	t.Helper()

	if err := json.NewEncoder(w).Encode(map[string]any{"model": "actual", "answers": map[string]any{"rating": answer}, "usage": map[string]int{"input_tokens": 0, "output_tokens": 0}}); err != nil {
		t.Error(err)
	}
}

func assertQuestionBoundary(t *testing.T, response *typesafe.SystemOneResponse, kind string, count int) {
	t.Helper()

	if kind == "choice" {
		answer, err := response.Choice("rating")
		if err != nil || len(answer.Probabilities) != count {
			t.Fatalf("choice=%+v error=%v", answer, err)
		}

		return
	}

	answer, err := response.Score("rating")
	if err != nil || len(answer.Legend) != count || answer.Score != float64(count-1)/2 {
		t.Fatalf("score=%+v error=%v", answer, err)
	}
}

func TestRoundedDistributionsAndAccessorErrors(t *testing.T) {
	body := `{"model":"actual","answers":{"tone":{"type":"choice","choice":"a","confidence":0.5,"probabilities":{"a":0.3333333,"b":0.3333333,"c":0.3333333}},"score":{"type":"score","score":1,"confidence":0.6,"legend":{"0":"a","1":"b","2":"c"},"probabilities":{"0":0.3333333,"1":0.3333333,"2":0.3333333}}},"usage":{"input_tokens":0,"output_tokens":0}}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fixtureWrite(t, w, []byte(body)) }))

	defer server.Close()

	client := mustClient(t, typesafe.Config{APIKey: "test", BaseURL: server.URL})

	response, err := client.SystemOne(context.Background(), typesafe.SystemOneRequest{State: "state", Questions: typesafe.Questions{"tone": typesafe.ChoiceQuestion{Criteria: map[string]any{"a": nil, "b": nil, "c": nil}}, "score": typesafe.ScoreQuestion{Criteria: []any{"a", "b", "c"}}}})
	if err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"missing", "score"} {
		_, err := response.Choice(name)
		expected := typesafe.ErrAnswerMissing

		if name == "score" {
			expected = typesafe.ErrAnswerKind
		}

		if !errors.Is(err, expected) {
			t.Fatalf("Choice(%q)=%v", name, err)
		}
	}

	for _, name := range []string{"missing", "tone"} {
		_, err := response.Score(name)
		expected := typesafe.ErrAnswerMissing

		if name == "tone" {
			expected = typesafe.ErrAnswerKind
		}

		if !errors.Is(err, expected) {
			t.Fatalf("Score(%q)=%v", name, err)
		}
	}

	filtered := response.AnswersByKind("choice")
	delete(filtered, "tone")

	if len(response.Answers) != 2 {
		t.Fatal("filtering mutated named answers")
	}
}

func TestChoiceScoreContentUsesValidatedSnapshot(t *testing.T) {
	for _, kind := range []string{"choice", "score"} {
		t.Run(kind, func(t *testing.T) {
			var question typesafe.Question = typesafe.ChoiceQuestion{Instructions: &changingJSON{}, Criteria: map[string]any{"a": &changingJSON{}}}

			if kind == "score" {
				question = typesafe.ScoreQuestion{Instructions: &changingJSON{}, Criteria: []any{&changingJSON{}}}
			}

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { serveSnapshotAnswer(t, w, r, kind) }))

			defer server.Close()

			client := mustClient(t, typesafe.Config{APIKey: "test", BaseURL: server.URL})
			if _, err := client.SystemOne(context.Background(), typesafe.SystemOneRequest{State: "state", Questions: typesafe.Questions{"rating": question}}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func serveSnapshotAnswer(t *testing.T, w http.ResponseWriter, r *http.Request, kind string) {
	t.Helper()

	var body struct {
		Questions map[string]struct {
			Instructions any `json:"instructions"`
			Criteria     any `json:"criteria"`
		} `json:"questions"`
	}

	if !fixtureDecode(t, r.Body, &body) {
		return
	}

	received := body.Questions["rating"]
	if received.Instructions != "original" {
		t.Errorf("instructions=%v", received.Instructions)
	}

	answer := snapshotAnswer(t, kind, received.Criteria)
	writeQuestionResponse(t, w, answer)
}

func snapshotAnswer(t *testing.T, kind string, criteria any) map[string]any {
	t.Helper()

	answer := map[string]any{"type": kind, "confidence": 1.0}
	if kind == "choice" {
		if !reflect.DeepEqual(criteria, map[string]any{"a": "original"}) {
			t.Errorf("criteria=%v", criteria)
		}

		answer["choice"] = "a"
		answer["probabilities"] = map[string]float64{"a": 1}

		return answer
	}

	if !reflect.DeepEqual(criteria, []any{"original"}) {
		t.Errorf("criteria=%v", criteria)
	}

	answer["score"] = 0
	answer["legend"] = map[string]string{"0": "original"}
	answer["probabilities"] = map[string]float64{"0": 1}

	return answer
}
