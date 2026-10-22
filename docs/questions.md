# Named Questions and typed Answers

A System One evaluation can mix Noul, Choice, and Score Questions against one
shared State. Question names identify their corresponding Answers.

```go
request := typesafe.SystemOneRequest{
    State: map[string]any{"message": "Please refund the duplicate charge."},
    Questions: typesafe.Questions{
        "billing": typesafe.NoulQuestion{Instructions: "Is this about billing?"},
        "tone": typesafe.ChoiceQuestion{
            Instructions: "What is the tone?",
            Criteria: map[string]any{"angry": "An upset message", "calm": nil},
        },
        "urgency": typesafe.ScoreQuestion{
            Instructions: "How urgent is this?",
            Criteria: []any{"Can wait", "This week", "Today"},
        },
    },
}
response, err := client.SystemOne(ctx, request)
```

Choice supports 1–255 alternatives. A nil description uses the alternative name
alone. Descriptions and instructions can be strings, structs, maps, or slices.
Score supports 1–10 descriptions with the same content shapes. Score descriptions
must not be null at the top level; nested nulls remain valid in all supported
structured content. All values must be JSON-encodable, with finite numbers.

`response.Choice("tone")` returns `ChoiceAnswer` with `Choice`, `Confidence`,
and named `Probabilities`. Confidence remains distinct from class probabilities.
`response.Score("urgency")` returns `ScoreAnswer` with a fractional `Score`,
`Confidence`, `Legend`, and `Probabilities`. Legend and probability maps use
zero-based integer keys, so `Legend[0]` describes the first requested level.
Typed accessors return an error for a missing name or mismatched kind.

`response.Answers` retains the full named collection.
`response.AnswersByKind("choice")` returns a new map containing Choice Answers;
the same method supports Noul, Score, and other returned discriminators.

The SDK rejects missing required response fields, invalid field types, numbers
outside their ranges, incomplete Choice probabilities, and inconsistent Score
levels. Choice probability names must match the requested alternatives. Score
legends must preserve the requested ordered descriptions, and probability keys
must match those levels. Probability sums may differ slightly from one because
of rounding. Protocol errors retain original body bytes and the invalid field
path; unknown response fields remain available in the original response.

The [classification](../examples/classification/main.go) and
[scoring](../examples/scoring/main.go) examples compile with the library. Running
them requires `TYPESAFE_API_KEY` and makes live evaluations; ordinary tests do not.
