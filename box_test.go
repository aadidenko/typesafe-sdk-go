package typesafe_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	typesafe "github.com/aadidenko/typesafe-sdk-go"
)

func TestBoxAndNoulWithNullableConfidence(t *testing.T) {
	for _, confidence := range []string{"null", "0", "0.9"} {
		t.Run(confidence, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				serveBoxResponse(t, w, r, confidence)
			}))
			defer server.Close()

			client := mustClient(t, typesafe.Config{APIKey: "key", BaseURL: server.URL, ExperimentalImages: true})

			response, err := client.SystemOne(context.Background(), typesafe.SystemOneRequest{State: "photo", Questions: typesafe.Questions{"location": typesafe.BoxQuestion{Instructions: []any{"locate", "photo"}}, "visible": typesafe.NoulQuestion{Instructions: "visible?"}}}, typesafe.WithModel("chosen-box"))
			if err != nil {
				t.Fatal(err)
			}

			assertBoxResponse(t, response, confidence)
		})
	}
}

func TestInvalidBoxResponsesRetainOriginal(t *testing.T) {
	for name, answer := range map[string]string{
		"missing confidence":  `{"type":"box","box":{"top":0,"bottom":1,"left":0,"right":1}}`,
		"negative confidence": `{"type":"box","box":{"top":0,"bottom":1,"left":0,"right":1},"confidence":-0.1}`,
		"large confidence":    `{"type":"box","box":{"top":0,"bottom":1,"left":0,"right":1},"confidence":1.1}`,
		"string confidence":   `{"type":"box","box":{"top":0,"bottom":1,"left":0,"right":1},"confidence":"0.9"}`,
		"missing box":         `{"type":"box","confidence":null}`,
		"missing coordinate":  `{"type":"box","box":{"bottom":1,"left":0,"right":1},"confidence":null}`,
		"null coordinate":     `{"type":"box","box":{"top":null,"bottom":1,"left":0,"right":1},"confidence":null}`,
		"negative coordinate": `{"type":"box","box":{"top":-0.1,"bottom":1,"left":0,"right":1},"confidence":null}`,
		"large coordinate":    `{"type":"box","box":{"top":0,"bottom":1.1,"left":0,"right":1},"confidence":null}`,
		"string coordinate":   `{"type":"box","box":{"top":"0","bottom":1,"left":0,"right":1},"confidence":null}`,
		"wrong kind":          `{"type":"noul","noul":0.5}`,
	} {
		t.Run(name, func(t *testing.T) {
			body := `{"model":"m","usage":{"input_tokens":0,"output_tokens":0},"answers":{"q":` + answer + `}}`
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("x-typesafe-request-id", "box-invalid")
				fixtureWriteString(t, w, body)
			}))

			defer server.Close()

			client := mustClient(t, typesafe.Config{APIKey: "key", BaseURL: server.URL, ExperimentalImages: true})
			_, err := client.SystemOne(context.Background(), typesafe.SystemOneRequest{State: "image", Questions: typesafe.Questions{"q": typesafe.BoxQuestion{Instructions: "locate"}}})

			var protocol *typesafe.ProtocolError

			if !errors.As(err, &protocol) || string(protocol.RawBody) != body || protocol.RequestID != "box-invalid" {
				t.Fatalf("protocol error must retain original response: %v", err)
			}
		})
	}
}

func assertBoxResponse(t *testing.T, response *typesafe.SystemOneResponse, confidence string) {
	t.Helper()

	box, err := response.Box("location")
	if err != nil {
		t.Fatal(err)
	}

	if box.Box.Top != .2 || box.Box.Bottom != .8 || box.Box.Left != .1 || box.Box.Right != .9 {
		t.Errorf("wrong coordinates: %+v", box.Box)
	}

	assertBoxConfidence(t, box, confidence)

	if response.Answers["location"].Kind() != "box" {
		t.Error("Box kind must be inspectable")
	}

	assertBoxAccessors(t, response)
}

func assertBoxConfidence(t *testing.T, box typesafe.BoxAnswer, confidence string) {
	t.Helper()

	if confidence == "null" {
		if box.Confidence != nil {
			t.Error("null confidence must stay nil")
		}

		return
	}

	if box.Confidence == nil {
		t.Error("numeric confidence must stay present")

		return
	}

	expected := map[string]float64{"0": 0, "0.9": .9}
	if *box.Confidence != expected[confidence] {
		t.Error("numeric confidence changed")
	}
}

func assertBoxAccessors(t *testing.T, response *typesafe.SystemOneResponse) {
	t.Helper()

	noul, err := response.Noul("visible")
	if err != nil || noul.Probability != .7 {
		t.Fatalf("mixed Noul answer: %+v %v", noul, err)
	}

	if _, err := response.Box("visible"); !errors.Is(err, typesafe.ErrAnswerKind) {
		t.Errorf("wrong accessor: %v", err)
	}

	if _, err := response.Box("missing"); !errors.Is(err, typesafe.ErrAnswerMissing) {
		t.Errorf("missing accessor: %v", err)
	}
}

func serveBoxResponse(t *testing.T, w http.ResponseWriter, r *http.Request, confidence string) {
	t.Helper()

	if r.Header.Get("Content-Type") != "application/json" {
		t.Error("Box without asset must remain JSON")
	}

	var request struct {
		Model string `json:"model"`
	}

	if !fixtureDecode(t, r.Body, &request) {
		return
	}

	if request.Model != "chosen-box" {
		t.Errorf("media changed chosen model: %q", request.Model)
	}

	if r.Header.Get("TypeSafe-Danger-Zone-Images") != "true" {
		t.Error("missing opt in")
	}

	fixtureWriteString(t, w, `{"model":"m","usage":{"input_tokens":0,"output_tokens":0},"answers":{"location":{"type":"box","box":{"top":0.2,"bottom":0.8,"left":0.1,"right":0.9},"confidence":`+confidence+`},"visible":{"type":"noul","noul":0.7}}}`)
}
