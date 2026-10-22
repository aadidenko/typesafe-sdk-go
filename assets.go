package typesafe

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"os"
	"strings"
	"sync/atomic"
)

// Image is an immutable image source prepared for each evaluation.
// Use the ImageFrom constructors rather than its zero value.
type Image struct{ data []byte }

// ImageFromBytes copies and validates PNG or JPEG bytes.
func ImageFromBytes(data []byte) (Image, error) {
	if _, format, err := image.Decode(bytes.NewReader(data)); err != nil || (format != "png" && format != "jpeg") {
		return Image{}, &ValidationError{Field: "image", Message: "must contain a usable PNG or JPEG image"}
	}

	return Image{data: append([]byte(nil), data...)}, nil
}

// ImageFromFile reads a PNG or JPEG file into an immutable source snapshot.
func ImageFromFile(path string) (Image, error) {
	data, err := os.ReadFile(path) //nolint:gosec // The caller intentionally selects the image file path.
	if err != nil {
		return Image{}, &ValidationError{Field: "image.file", Message: "cannot read image file"}
	}

	return ImageFromBytes(data)
}

// ImageFromBase64 accepts standard base64 with optional ASCII whitespace.
func ImageFromBase64(value string) (Image, error) {
	value = strings.Map(func(r rune) rune {
		switch r {
		case ' ', '\t', '\r', '\n', '\v', '\f':
			return -1
		}

		return r
	}, value)

	data, err := base64.StdEncoding.Strict().DecodeString(value)
	if err != nil {
		return Image{}, &ValidationError{Field: "image.base64", Message: "must contain valid base64"}
	}

	return ImageFromBytes(data)
}

// ImageFromGoImage encodes an image as png or jpeg (jpg is also accepted).
// It does not resize, rotate or otherwise transform image content.
func ImageFromGoImage(value image.Image, format string) (result Image, err error) {
	// Standard image encoders can panic on malformed third-party Image values.
	defer func() {
		if recover() != nil {
			result = Image{}
			err = &ValidationError{Field: "image", Message: "must contain a usable image"}
		}
	}()

	if value == nil || value.Bounds().Empty() {
		return Image{}, &ValidationError{Field: "image", Message: "must contain a nonempty image"}
	}

	var data bytes.Buffer

	switch strings.ToLower(format) {
	case "png":
		err = png.Encode(&data, value)
	case "jpeg", "jpg":
		err = jpeg.Encode(&data, value, &jpeg.Options{Quality: 95})
	default:
		return Image{}, &ValidationError{Field: "image.format", Message: "must be png or jpeg"}
	}

	if err != nil {
		return Image{}, &ValidationError{Field: "image", Message: "cannot encode image"}
	}

	return Image{data: data.Bytes()}, nil
}

// Assets registers image sources by their exact references in request content.
// References are ordinary strings, including when embedded in longer strings.
type Assets map[string]Image

var imageReferenceCounter atomic.Uint64

// Add registers an image under a generated reference and returns that reference.
// Each registration has its own descriptor even when two sources are identical.
func (a *Assets) Add(value Image) string {
	if *a == nil {
		*a = make(Assets)
	}

	for {
		reference := fmt.Sprintf("<image:%d>", imageReferenceCounter.Add(1))
		if _, exists := (*a)[reference]; !exists {
			(*a)[reference] = value
			return reference
		}
	}
}
