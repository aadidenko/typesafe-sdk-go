package typesafe_test

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
)

type multipartFixtureImage struct {
	data        []byte
	contentType string
}

type multipartFixtureBody struct {
	requestJSON []byte
	images      map[string]multipartFixtureImage
}

func parseMultipartFixture(raw []byte, contentType string) (multipartFixtureBody, error) {
	reader, err := fixtureMultipartReader(raw, contentType)
	if err != nil {
		return multipartFixtureBody{}, err
	}

	requestJSON, err := fixtureRequestPart(reader)
	if err != nil {
		return multipartFixtureBody{}, err
	}

	images, err := fixtureImageParts(reader)
	if err != nil {
		return multipartFixtureBody{}, err
	}

	return multipartFixtureBody{requestJSON: requestJSON, images: images}, nil
}

func fixtureMultipartReader(raw []byte, contentType string) (*multipart.Reader, error) {
	media, params, err := mime.ParseMediaType(contentType)
	if err != nil || media != "multipart/form-data" {
		return nil, fmt.Errorf("expected multipart: %s", contentType)
	}

	return multipart.NewReader(bytes.NewReader(raw), params["boundary"]), nil
}

func fixtureRequestPart(reader *multipart.Reader) ([]byte, error) {
	first, err := reader.NextPart()
	if err != nil {
		return nil, err
	}

	if first.FormName() != "request" || first.Header.Get("Content-Type") != "application/json" {
		return nil, errors.New("first part must be application/json request")
	}

	body, readErr := io.ReadAll(first)

	return body, errors.Join(readErr, first.Close())
}

func fixtureImageParts(reader *multipart.Reader) (map[string]multipartFixtureImage, error) {
	images := make(map[string]multipartFixtureImage)

	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			return images, nil
		}

		if err != nil {
			return nil, err
		}

		name := part.FormName()
		if name == "" || images[name].data != nil {
			return nil, errors.New("empty or duplicate part name")
		}

		data, readErr := io.ReadAll(part)
		if err := errors.Join(readErr, part.Close()); err != nil {
			return nil, err
		}

		images[name] = multipartFixtureImage{data: data, contentType: part.Header.Get("Content-Type")}
	}
}
