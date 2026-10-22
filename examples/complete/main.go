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

	ctx, cancel := context.WithTimeout(context.Background(), *deadline)

	defer cancel()

	if err := run(ctx, *imagePath, typesafe.Compression(*compression), *diagnostics, *bodies); err != nil {
		reportError(err)
		os.Exit(1)
	}
}

func run(ctx context.Context, imagePath string, compression typesafe.Compression, diagnostics, bodies bool) error {
	transport := http.DefaultTransport.(*http.Transport).Clone()

	transport.MaxIdleConnsPerHost = 20

	defer transport.CloseIdleConnections() // The application owns this resource.

	config := typesafe.LoadEnv(typesafe.Config{HTTPClient: &http.Client{Transport: transport}, Compression: compression, ExperimentalImages: true})
	if diagnostics {
		config.Logger = slog.New(slog.NewJSONHandler(os.Stderr, nil))
		config.LogBodies = bodies
	}

	if config.Model == "" {
		config.Model = typesafe.DefaultModel
	}

	client, err := typesafe.NewClient(config)
	if err != nil {
		return err
	}

	models, err := client.ListModels(ctx)
	if err != nil {
		return err
	}

	selected := ""

	for _, model := range models.Models {
		fmt.Printf("Available: %s (%s): %s\n", model.Name, model.ReleaseDate, model.Description)

		if model.Name == config.Model {
			selected = model.Name
		}
	}

	if selected == "" {
		return fmt.Errorf("configured model is absent from account model discovery")
	}

	// Model metadata has no image-capability flag. Select an image-capable model
	// explicitly with TYPESAFE_DEFAULT_MODEL; the SDK never switches it for media.
	photo, err := loadImage(imagePath)
	if err != nil {
		return err
	}

	var assets typesafe.Assets

	reference := assets.Add(photo)

	response, err := client.SystemOne(ctx, typesafe.SystemOneRequest{
		State: struct {
			Image   string `json:"image"`
			Context []any  `json:"context"`
		}{reference, []any{"Inspect the rectangle in " + reference, nil}},
		Assets: assets,
		Questions: typesafe.Questions{
			"visible":  typesafe.NoulQuestion{Instructions: "Is a red rectangle visible?", Criteria: &typesafe.NoulCriteria{True: "A red rectangle is present", False: "No red rectangle"}},
			"color":    typesafe.ChoiceQuestion{Instructions: "What is the rectangle's main color?", Criteria: map[string]any{"red": nil, "blue": nil, "other": "Any other color"}},
			"size":     typesafe.ScoreQuestion{Instructions: "How much of the image does the rectangle occupy?", Criteria: []any{"A small fraction", "About half", "Almost all"}},
			"location": typesafe.BoxQuestion{Instructions: "Locate the red rectangle in " + reference},
		},
	}, typesafe.WithModel(selected), typesafe.WithHeaders(http.Header{"X-Application": {"complete-example"}}))
	if err != nil {
		return err
	}

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

	fmt.Printf("Visible probability %.3f; color %s (confidence %.3f, probabilities %v)\n", visible.Probability, choice.Choice, choice.Confidence, choice.Probabilities)
	fmt.Printf("Expected size %.3f (confidence %.3f, legend %v, probabilities %v)\n", score.Score, score.Confidence, score.Legend, score.Probabilities)

	confidence := "unavailable"
	if box.Confidence != nil {
		confidence = fmt.Sprintf("%.3f", *box.Confidence)
	}

	fmt.Printf("Box %+v, confidence %s; Box Answers: %d\n", box.Box, confidence, len(response.AnswersByKind("box")))

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
	fmt.Printf("Application decode: visible %.3f, color %s, size %.3f\n", app.Visible.Probability, app.Color.Choice, app.Size.Score)

	var full struct {
		Model string
		Usage typesafe.Usage
	}

	if err := response.DecodeResponse(&full); err != nil {
		return err
	}

	fmt.Printf("Actual model %s, usage %+v, HTTP %d, request ID %s, retained bytes %d\n", full.Model, full.Usage, response.StatusCode, response.RequestID, len(response.RawBody))

	return nil
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
	var (
		httpError  *typesafe.HTTPError
		protocol   *typesafe.ProtocolError
		validation *typesafe.ValidationError
	)

	switch {
	case errors.Is(err, context.Canceled):
		fmt.Fprintln(os.Stderr, "workflow canceled")
	case errors.Is(err, context.DeadlineExceeded):
		fmt.Fprintln(os.Stderr, "workflow deadline exceeded")
	case errors.As(err, &httpError):
		fmt.Fprintf(os.Stderr, "HTTP %d, request ID %s\n", httpError.StatusCode, httpError.RequestID)
	case errors.As(err, &protocol):
		fmt.Fprintf(os.Stderr, "invalid response at %s, request ID %s\n", protocol.Field, protocol.RequestID)
	case errors.As(err, &validation):
		fmt.Fprintln(os.Stderr, validation)
	default:
		fmt.Fprintln(os.Stderr, err)
	}
}
