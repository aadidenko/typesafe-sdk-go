# TypeSafe System One

TypeSafe evaluates typed questions against supplied content and returns structured judgments. These terms describe the shared domain of the Go SDK and the TypeSafe API.

## Language

**System One**:
An evaluation of named questions against a shared state, producing named answers and token usage.
_Avoid_: Chat completion

**State**:
The content shared by all questions in a System One evaluation.
_Avoid_: Conversation, prompt

**Question**:
A named judgment to make about the state, expressed through a question type, optional instructions, and any criteria required by that type.
_Avoid_: Message

**Answer**:
The structured judgment corresponding to a named question, with values determined by its question type.
_Avoid_: Generated text

**Noul**:
A question type whose answer is the probability that a statement is true or that a yes/no question has a yes answer.
_Avoid_: Boolean answer, confidence

**Choice**:
A question type that selects one named option from defined criteria and reports its confidence and the options' probabilities.
_Avoid_: Free-text classification

**Score**:
A question type that evaluates ordered rubric levels and returns their probability-weighted score, confidence, and level probabilities.
_Avoid_: Integer rating

**Box**:
A question type that locates a target in an image through four edges expressed as fractions of image width and height.
_Avoid_: Pixel coordinates

**Confidence**:
A model-reported measure of certainty in a judgment, distinct from the probabilities of its alternatives.
_Avoid_: Class probability, noul

**Asset**:
Media supplied with an evaluation and referenced from its state, instructions, or criteria; currently, TypeSafe supports image assets.
_Avoid_: Attachment message

**Asset reference**:
A string identifying an asset at each place where that asset belongs in the evaluation's content.
_Avoid_: Image URL
