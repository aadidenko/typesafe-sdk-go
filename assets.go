package typesafe

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
	"sort"
	"strings"
	"sync/atomic"
)

// Image is an immutable image source prepared for each evaluation.
// Use the ImageFrom constructors rather than its zero value.
type Image struct{ data []byte }

// ImageFromBytes copies and validates PNG or JPEG bytes.
func ImageFromBytes(data []byte) (Image, error) {
	if _, format, err := image.Decode(bytes.NewReader(data)); err != nil || (format != "png" && format != "jpeg") {
		return Image{}, &ValidationError{"image", "must contain a usable PNG or JPEG image"}
	}

	return Image{append([]byte(nil), data...)}, nil
}

// ImageFromFile reads a PNG or JPEG file into an immutable source snapshot.
func ImageFromFile(path string) (Image, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Image{}, &ValidationError{"image.file", "cannot read image file"}
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
		return Image{}, &ValidationError{"image.base64", "must contain valid base64"}
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
			err = &ValidationError{"image", "must contain a usable image"}
		}
	}()

	if value == nil || value.Bounds().Empty() {
		return Image{}, &ValidationError{"image", "must contain a nonempty image"}
	}

	var data bytes.Buffer

	switch strings.ToLower(format) {
	case "png":
		err = png.Encode(&data, value)
	case "jpeg", "jpg":
		err = jpeg.Encode(&data, value, &jpeg.Options{Quality: 95})
	default:
		return Image{}, &ValidationError{"image.format", "must be png or jpeg"}
	}

	if err != nil {
		return Image{}, &ValidationError{"image", "cannot encode image"}
	}

	return Image{data.Bytes()}, nil
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

func prepareAssets(body []byte, assets Assets) ([]byte, string, error) {
	if len(assets) == 0 {
		return body, "application/json", nil
	}

	var root map[string]json.RawMessage

	if err := json.Unmarshal(body, &root); err != nil {
		return nil, "", &ValidationError{"request", "must be JSON-encodable"}
	}

	names := make([]string, 0, len(assets))
	for name := range assets {
		names = append(names, name)
	}

	sort.Strings(names)

	descriptors := make(map[string]any, len(names))
	for i, name := range names {
		if name == "" || len(assets[name].data) == 0 {
			return nil, "", &ValidationError{"assets", "references and images must not be empty"}
		}

		descriptors[name] = struct {
			Type string `json:"type"`
			Part string `json:"part"`
		}{"image", fmt.Sprintf("image-%d", i)}
	}

	root["assets"], _ = json.Marshal(descriptors)

	request, err := json.Marshal(root)
	if err != nil {
		return nil, "", &ValidationError{"request", "must be JSON-encodable"}
	}

	var buffer bytes.Buffer

	writer := multipart.NewWriter(&buffer)
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", `form-data; name="request"`)
	header.Set("Content-Type", "application/json")

	part, err := writer.CreatePart(header)
	if err != nil {
		return nil, "", err
	}

	if _, err = part.Write(request); err != nil {
		return nil, "", err
	}

	for i, name := range names {
		data := assets[name].data
		header := make(textproto.MIMEHeader)
		header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="image-%d"; filename="image"`, i))
		header.Set("Content-Type", http.DetectContentType(data))

		part, err := writer.CreatePart(header)
		if err != nil {
			return nil, "", err
		}

		if _, err = part.Write(data); err != nil {
			return nil, "", err
		}
	}

	if err := writer.Close(); err != nil {
		return nil, "", err
	}

	return buffer.Bytes(), writer.FormDataContentType(), nil
}
