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
	}{"Duplicate charge"}, Questions: typesafe.Questions{
		"billing": typesafe.NoulQuestion{Instructions: "Billing?"},
		"tone":    typesafe.ChoiceQuestion{Instructions: map[string]any{"task": []any{"Classify", nil}}, Criteria: map[string]any{"angry": "An upset message", "calm": nil, "excited": []any{"Enthusiasm", nil}}},
		"urgency": typesafe.ScoreQuestion{Instructions: []any{"How urgent?", nil}, Criteria: []any{"Can wait", map[string]any{"when": "This week", "nested": []any{nil}}, []any{"Today", nil}}},
	}}
}

func TestMixedNoulChoiceScore(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			State     map[string]any            `json:"state"`
			Questions map[string]map[string]any `json:"questions"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.State["message"] != "Duplicate charge" || len(body.Questions) != 3 {
			t.Errorf("state/questions=%+v", body)
		}
		for name, kind := range map[string]string{"billing": "noul", "tone": "choice", "urgency": "score"} {
			if body.Questions[name]["type"] != kind {
				t.Errorf("%s kind=%v", name, body.Questions[name]["type"])
			}
		}
		criteria := body.Questions["tone"]["criteria"].(map[string]any)
		if v, ok := criteria["calm"]; !ok || v != nil {
			t.Error("undescribed choice lost")
		}
		if !reflect.DeepEqual(body.Questions["urgency"]["criteria"], []any{"Can wait", map[string]any{"when": "This week", "nested": []any{nil}}, []any{"Today", nil}}) {
			t.Error("structured score levels lost")
		}
		w.Write([]byte(mixedResponse))
	}))
	defer server.Close()
	client, _ := typesafe.NewClient(typesafe.Config{APIKey: "test", BaseURL: server.URL})
	response, err := client.SystemOne(context.Background(), mixedRequest())
	if err != nil {
		t.Fatal(err)
	}
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
	if len(response.AnswersByKind("noul")) != 1 || len(response.AnswersByKind("choice")) != 1 || len(response.AnswersByKind("score")) != 1 || len(response.AnswersByKind("box")) != 0 {
		t.Fatal("answer filtering failed")
	}
}

func TestChoiceScoreValidationBeforeHTTP(t *testing.T) {
	var traffic atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { traffic.Add(1); w.Write([]byte(mixedResponse)) }))
	defer server.Close()
	client, _ := typesafe.NewClient(typesafe.Config{APIKey: "test", BaseURL: server.URL})
	tooManyChoices := make(map[string]any)
	for i := range 256 {
		tooManyChoices[strconv.Itoa(i)] = nil
	}
	tooManyLevels := make([]any, 11)
	for i := range tooManyLevels {
		tooManyLevels[i] = "level"
	}
	cases := []typesafe.Question{
		typesafe.ChoiceQuestion{}, typesafe.ChoiceQuestion{Criteria: tooManyChoices},
		typesafe.ChoiceQuestion{Criteria: map[string]any{"invalid": true}}, typesafe.ChoiceQuestion{Criteria: map[string]any{"invalid": map[string]any{"value": math.Inf(1)}}},
		typesafe.ChoiceQuestion{Instructions: 42, Criteria: map[string]any{"ok": nil}},
		typesafe.ScoreQuestion{}, typesafe.ScoreQuestion{Criteria: tooManyLevels}, typesafe.ScoreQuestion{Criteria: []any{nil}},
		typesafe.ScoreQuestion{Criteria: []any{42}}, typesafe.ScoreQuestion{Criteria: []any{[]any{func() {}}}},
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
		{"choice missing selection", "choice", `{"type":"choice","confidence":0.7,"probabilities":{"a":1}}`, "choice"},
		{"choice null selection", "choice", `{"type":"choice","choice":null,"confidence":0.7,"probabilities":{"a":1}}`, "choice"},
		{"choice missing confidence", "choice", `{"type":"choice","choice":"a","probabilities":{"a":1}}`, "confidence"},
		{"choice null confidence", "choice", `{"type":"choice","choice":"a","confidence":null,"probabilities":{"a":1}}`, "confidence"},
		{"choice confidence range", "choice", `{"type":"choice","choice":"a","confidence":1.1,"probabilities":{"a":1}}`, "confidence"},
		{"choice missing probabilities", "choice", `{"type":"choice","choice":"a","confidence":0.7}`, "probabilities"},
		{"choice empty probabilities", "choice", `{"type":"choice","choice":"a","confidence":0.7,"probabilities":{}}`, "probabilities"},
		{"choice null probability", "choice", `{"type":"choice","choice":"a","confidence":0.7,"probabilities":{"a":null}}`, "probabilities.a"},
		{"choice probability string", "choice", `{"type":"choice","choice":"a","confidence":0.7,"probabilities":{"a":"1"}}`, "probabilities.a"},
		{"choice probability range", "choice", `{"type":"choice","choice":"a","confidence":0.7,"probabilities":{"a":-1}}`, "probabilities.a"},
		{"choice selection missing from probabilities", "choice", `{"type":"choice","choice":"b","confidence":0.7,"probabilities":{"a":1}}`, "choice"},
		{"choice alternatives differ from question", "choice", `{"type":"choice","choice":"b","confidence":0.7,"probabilities":{"b":1}}`, "probabilities"},
		{"score missing value", "score", `{"type":"score","confidence":0.7,"legend":{"0":"Only"},"probabilities":{"0":1}}`, "score"},
		{"score null value", "score", `{"type":"score","score":null,"confidence":0.7,"legend":{"0":"Only"},"probabilities":{"0":1}}`, "score"},
		{"score missing confidence", "score", `{"type":"score","score":0,"legend":{"0":"Only"},"probabilities":{"0":1}}`, "confidence"},
		{"score negative confidence", "score", `{"type":"score","score":0,"confidence":-0.1,"legend":{"0":"Only"},"probabilities":{"0":1}}`, "confidence"},
		{"score missing legend", "score", `{"type":"score","score":0,"confidence":0.7,"probabilities":{"0":1}}`, "legend"},
		{"score empty legend", "score", `{"type":"score","score":0,"confidence":0.7,"legend":{},"probabilities":{"0":1}}`, "legend"},
		{"score null level", "score", `{"type":"score","score":0,"confidence":0.7,"legend":{"0":null},"probabilities":{"0":1}}`, "legend.0"},
		{"score scalar level", "score", `{"type":"score","score":0,"confidence":0.7,"legend":{"0":42},"probabilities":{"0":1}}`, "legend.0"},
		{"score noninteger level", "score", `{"type":"score","score":0,"confidence":0.7,"legend":{"first":"Only"},"probabilities":{"first":1}}`, "legend.first"},
		{"score noncanonical level", "score", `{"type":"score","score":0,"confidence":0.7,"legend":{"00":"Only"},"probabilities":{"00":1}}`, "legend.00"},
		{"score levels start at zero", "score", `{"type":"score","score":0,"confidence":0.7,"legend":{"1":"Only"},"probabilities":{"1":1}}`, "legend"},
		{"score missing probabilities", "score", `{"type":"score","score":0,"confidence":0.7,"legend":{"0":"Only"}}`, "probabilities"},
		{"score null probability", "score", `{"type":"score","score":0,"confidence":0.7,"legend":{"0":"Only"},"probabilities":{"0":null}}`, "probabilities.0"},
		{"score probability too high", "score", `{"type":"score","score":0,"confidence":0.7,"legend":{"0":"Only"},"probabilities":{"0":1.1}}`, "probabilities.0"},
		{"score probability keys differ", "score", `{"type":"score","score":0,"confidence":0.7,"legend":{"0":"Only"},"probabilities":{"1":1}}`, "probabilities"},
		{"score outside rubric", "score", `{"type":"score","score":1,"confidence":0.7,"legend":{"0":"Only"},"probabilities":{"0":1}}`, "score"},
		{"score negative value", "score", `{"type":"score","score":-0.1,"confidence":0.7,"legend":{"0":"Only"},"probabilities":{"0":1}}`, "score"},
		{"score legend differs from question", "score", `{"type":"score","score":0,"confidence":0.7,"legend":{"0":"Different"},"probabilities":{"0":1}}`, "legend"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			body := `{"model":"actual","answers":{"rating":` + test.answer + `},"usage":{"input_tokens":1,"output_tokens":1}}`
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("x-typesafe-request-id", "invalid-answer")
				w.Write([]byte(body))
			}))
			defer server.Close()
			client, _ := typesafe.NewClient(typesafe.Config{APIKey: "test", BaseURL: server.URL})
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
			t.Run(kind+"/"+strconv.Itoa(count), func(t *testing.T) {
				var question typesafe.Question
				if kind == "choice" {
					criteria := make(map[string]any, count)
					for i := range count {
						criteria[strconv.Itoa(i)] = nil
					}
					question = typesafe.ChoiceQuestion{Criteria: criteria}
				} else {
					criteria := make([]any, count)
					for i := range count {
						criteria[i] = map[string]any{"level": i, "nested": []any{nil}}
					}
					question = typesafe.ScoreQuestion{Criteria: criteria}
				}
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					var body struct {
						Questions map[string]struct {
							Type     string          `json:"type"`
							Criteria json.RawMessage `json:"criteria"`
						} `json:"questions"`
					}
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					answer := map[string]any{"type": kind, "confidence": 0.3}
					probabilities := make(map[string]float64, count)
					for i := range count {
						probabilities[strconv.Itoa(i)] = 1 / float64(count)
					}
					answer["probabilities"] = probabilities
					if kind == "choice" {
						var criteria map[string]any
						json.Unmarshal(body.Questions["rating"].Criteria, &criteria)
						if len(criteria) != count {
							t.Errorf("criteria count=%d", len(criteria))
						}
						answer["choice"] = "0"
					} else {
						var criteria []any
						json.Unmarshal(body.Questions["rating"].Criteria, &criteria)
						if len(criteria) != count {
							t.Errorf("criteria count=%d", len(criteria))
						}
						legend := make(map[string]any, count)
						for i, value := range criteria {
							legend[strconv.Itoa(i)] = value
						}
						answer["legend"] = legend
						answer["score"] = float64(count-1) / 2
					}
					json.NewEncoder(w).Encode(map[string]any{"model": "actual", "answers": map[string]any{"rating": answer}, "usage": map[string]int{"input_tokens": 0, "output_tokens": 0}})
				}))
				defer server.Close()
				client, _ := typesafe.NewClient(typesafe.Config{APIKey: "test", BaseURL: server.URL, Compression: typesafe.CompressionNone})
				response, err := client.SystemOne(context.Background(), typesafe.SystemOneRequest{State: "state", Questions: typesafe.Questions{"rating": question}})
				if err != nil {
					t.Fatal(err)
				}
				if kind == "choice" {
					answer, err := response.Choice("rating")
					if err != nil || len(answer.Probabilities) != count {
						t.Fatalf("choice=%+v error=%v", answer, err)
					}
				} else {
					answer, err := response.Score("rating")
					if err != nil || len(answer.Legend) != count || answer.Score != float64(count-1)/2 {
						t.Fatalf("score=%+v error=%v", answer, err)
					}
				}
			})
		}
	}
}

func TestRoundedDistributionsAndAccessorErrors(t *testing.T) {
	body := `{"model":"actual","answers":{"tone":{"type":"choice","choice":"a","confidence":0.5,"probabilities":{"a":0.3333333,"b":0.3333333,"c":0.3333333}},"score":{"type":"score","score":1,"confidence":0.6,"legend":{"0":"a","1":"b","2":"c"},"probabilities":{"0":0.3333333,"1":0.3333333,"2":0.3333333}}},"usage":{"input_tokens":0,"output_tokens":0}}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(body)) }))
	defer server.Close()
	client, _ := typesafe.NewClient(typesafe.Config{APIKey: "test", BaseURL: server.URL})
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
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body struct {
					Questions map[string]struct {
						Instructions any `json:"instructions"`
						Criteria     any `json:"criteria"`
					} `json:"questions"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				received := body.Questions["rating"]
				if received.Instructions != "original" {
					t.Errorf("instructions=%v", received.Instructions)
				}
				answer := map[string]any{"type": kind, "confidence": 1.0}
				if kind == "choice" {
					if !reflect.DeepEqual(received.Criteria, map[string]any{"a": "original"}) {
						t.Errorf("criteria=%v", received.Criteria)
					}
					answer["choice"] = "a"
					answer["probabilities"] = map[string]float64{"a": 1}
				} else {
					if !reflect.DeepEqual(received.Criteria, []any{"original"}) {
						t.Errorf("criteria=%v", received.Criteria)
					}
					answer["score"] = 0
					answer["legend"] = map[string]string{"0": "original"}
					answer["probabilities"] = map[string]float64{"0": 1}
				}
				json.NewEncoder(w).Encode(map[string]any{"model": "actual", "answers": map[string]any{"rating": answer}, "usage": map[string]int{"input_tokens": 0, "output_tokens": 0}})
			}))
			defer server.Close()
			client, _ := typesafe.NewClient(typesafe.Config{APIKey: "test", BaseURL: server.URL})
			if _, err := client.SystemOne(context.Background(), typesafe.SystemOneRequest{State: "state", Questions: typesafe.Questions{"rating": question}}); err != nil {
				t.Fatal(err)
			}
		})
	}
}
