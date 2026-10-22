// Command complete discovers a configured model and evaluates all four Question
// types against an image. Running it makes live requests; go test only compiles it.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"log/slog"
	"net/http"
	"os"
	"time"

	typesafe "github.com/aadidenko/typesafe-sdk-go"
)

func main() {
	imagePath := flag.String("image", "", "optional PNG/JPEG path; otherwise generate a red rectangle")
	compression := flag.String("compression", "zstd", "request compression: zstd, gzip, none")
	diagnostics := flag.Bool("log", false, "enable metadata diagnostics on stderr")
	bodies := flag.Bool("log-bodies", false, "explicitly include potentially sensitive wire body prefixes in diagnostics")
	deadline := flag.Duration("deadline", 30*time.Second, "overall workflow deadline")

	flag.Parse()

	if err := runWithDeadline(*deadline, *imagePath, typesafe.Compression(*compression), *diagnostics, *bodies); err != nil {
		reportError(err)
		os.Exit(1)
	}
}

func runWithDeadline(deadline time.Duration, imagePath string, compression typesafe.Compression, diagnostics, bodies bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), deadline)
	defer cancel()

	return run(ctx, imagePath, compression, diagnostics, bodies)
}

func run(ctx context.Context, imagePath string, compression typesafe.Compression, diagnostics, bodies bool) error {
	defaultTransport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return fmt.Errorf("default HTTP transport must be *http.Transport")
	}

	transport := defaultTransport.Clone()

	transport.MaxIdleConnsPerHost = 20

	defer transport.CloseIdleConnections() // The application owns this resource.

	config := exampleConfig(transport, compression, diagnostics, bodies)

	client, err := typesafe.NewClient(config)
	if err != nil {
		return err
	}

	selected, err := selectModel(ctx, client, config.Model)
	if err != nil {
		return err
	}

	// Model metadata has no image-capability flag. Select an image-capable model
	// explicitly with TYPESAFE_DEFAULT_MODEL; the SDK never switches it for media.
	photo, err := loadImage(imagePath)
	if err != nil {
		return err
	}

	response, err := client.SystemOne(ctx, imageRequest(photo), typesafe.WithModel(selected), typesafe.WithHeaders(http.Header{"X-Application": {"complete-example"}}))
	if err != nil {
		return err
	}

	if err := printTypedAnswers(response); err != nil {
		return err
	}

	if err := printApplicationAnswers(response); err != nil {
		return err
	}

	return printDecodedResponse(response)
}

func exampleConfig(transport *http.Transport, compression typesafe.Compression, diagnostics, bodies bool) typesafe.Config {
	config := typesafe.LoadEnv(typesafe.Config{HTTPClient: &http.Client{Transport: transport}, Compression: compression, ExperimentalImages: true})
	if diagnostics {
		config.Logger = slog.New(slog.NewJSONHandler(os.Stderr, nil))
		config.LogBodies = bodies
	}

	if config.Model == "" {
		config.Model = typesafe.DefaultModel
	}

	return config
}

func selectModel(ctx context.Context, client *typesafe.Client, configured string) (string, error) {
	models, err := client.ListModels(ctx)
	if err != nil {
		return "", err
	}

	selected := ""

	for _, model := range models.Models {
		if _, err := fmt.Printf("Available: %s (%s): %s\n", model.Name, model.ReleaseDate, model.Description); err != nil {
			return "", err
		}

		if model.Name == configured {
			selected = model.Name
		}
	}

	if selected == "" {
		return "", fmt.Errorf("configured model is absent from account model discovery")
	}

	return selected, nil
}

func imageRequest(photo typesafe.Image) typesafe.SystemOneRequest {
	var assets typesafe.Assets

	reference := assets.Add(photo)

	return typesafe.SystemOneRequest{
		State: struct {
			Image   string `json:"image"`
			Context []any  `json:"context"`
		}{Image: reference, Context: []any{"Inspect the rectangle in " + reference, nil}},
		Assets: assets,
		Questions: typesafe.Questions{
			"visible":  typesafe.NoulQuestion{Instructions: "Is a red rectangle visible?", Criteria: &typesafe.NoulCriteria{True: "A red rectangle is present", False: "No red rectangle"}},
			"color":    typesafe.ChoiceQuestion{Instructions: "What is the rectangle's main color?", Criteria: map[string]any{"red": nil, "blue": nil, "other": "Any other color"}},
			"size":     typesafe.ScoreQuestion{Instructions: "How much of the image does the rectangle occupy?", Criteria: []any{"A small fraction", "About half", "Almost all"}},
			"location": typesafe.BoxQuestion{Instructions: "Locate the red rectangle in " + reference},
		},
	}
}

func printTypedAnswers(response *typesafe.SystemOneResponse) error {
	visible, err := response.Noul("visible")
	if err != nil {
		return err
	}

	choice, err := response.Choice("color")
	if err != nil {
		return err
	}

	score, err := response.Score("size")
	if err != nil {
		return err
	}

	box, err := response.Box("location")
	if err != nil {
		return err
	}

	if _, err := fmt.Printf("Visible probability %.3f; color %s (confidence %.3f, probabilities %v)\n", visible.Probability, choice.Choice, choice.Confidence, choice.Probabilities); err != nil {
		return err
	}

	if _, err := fmt.Printf("Expected size %.3f (confidence %.3f, legend %v, probabilities %v)\n", score.Score, score.Confidence, score.Legend, score.Probabilities); err != nil {
		return err
	}

	confidence := "unavailable"
	if box.Confidence != nil {
		confidence = fmt.Sprintf("%.3f", *box.Confidence)
	}

	_, err = fmt.Printf("Box %+v, confidence %s; Box Answers: %d\n", box.Box, confidence, len(response.AnswersByKind("box")))

	return err
}

func printApplicationAnswers(response *typesafe.SystemOneResponse) error {
	var app struct {
		Visible struct {
			Probability float64 `json:"noul"`
		}
		Color    struct{ Choice string }
		Size     struct{ Score float64 }
		Location struct {
			Box        typesafe.Box
			Confidence *float64
		}
	}

	if err := response.DecodeAnswers(&app); err != nil {
		return err
	}

	// Domain validation and thresholds belong to the application, explicitly.
	_, err := fmt.Printf("Application decode: visible %.3f, color %s, size %.3f\n", app.Visible.Probability, app.Color.Choice, app.Size.Score)

	return err
}

func printDecodedResponse(response *typesafe.SystemOneResponse) error {
	var full struct {
		Model string
		Usage typesafe.Usage
	}

	if err := response.DecodeResponse(&full); err != nil {
		return err
	}

	_, err := fmt.Printf("Actual model %s, usage %+v, HTTP %d, request ID %s, retained bytes %d\n", full.Model, full.Usage, response.StatusCode, response.RequestID, len(response.RawBody))

	return err
}

func loadImage(path string) (typesafe.Image, error) {
	if path != "" {
		return typesafe.ImageFromFile(path)
	}

	pixels := image.NewRGBA(image.Rect(0, 0, 64, 64))
	draw.Draw(pixels, pixels.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.Draw(pixels, image.Rect(12, 16, 52, 48), image.NewUniform(color.RGBA{R: 255, A: 255}), image.Point{}, draw.Src)

	return typesafe.ImageFromGoImage(pixels, "png")
}

func reportError(err error) {
	_, _ = fmt.Fprintln(os.Stderr, errorMessage(err)) //nolint:errcheck // This is the final stderr diagnostic; reporting its write failure to the same failed stream cannot help.
}

func errorMessage(err error) string {
	var (
		httpError  *typesafe.HTTPError
		protocol   *typesafe.ProtocolError
		validation *typesafe.ValidationError
	)

	switch {
	case errors.Is(err, context.Canceled):
		return "workflow canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "workflow deadline exceeded"
	case errors.As(err, &httpError):
		return fmt.Sprintf("HTTP %d, request ID %s", httpError.StatusCode, httpError.RequestID)
	case errors.As(err, &protocol):
		return fmt.Sprintf("invalid response at %s, request ID %s", protocol.Field, protocol.RequestID)
	case errors.As(err, &validation):
		return validation.Error()
	default:
		return err.Error()
	}
}
