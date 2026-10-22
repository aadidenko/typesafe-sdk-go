package typesafe

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"sort"
)

type imageDescriptor struct {
	Type string `json:"type"`
	Part string `json:"part"`
}

func prepareAssets(body []byte, assets Assets) ([]byte, string, error) {
	if len(assets) == 0 {
		return body, "application/json", nil
	}

	var root map[string]json.RawMessage

	if err := json.Unmarshal(body, &root); err != nil {
		return nil, "", &ValidationError{Field: "request", Message: "must be JSON-encodable"}
	}

	names, descriptors, err := assetDescriptors(assets)
	if err != nil {
		return nil, "", err
	}

	root["assets"], _ = json.Marshal(descriptors) //nolint:errcheck // Image descriptors contain only string fields, so JSON encoding cannot fail.

	request, err := json.Marshal(root)
	if err != nil {
		return nil, "", &ValidationError{Field: "request", Message: "must be JSON-encodable"}
	}

	var buffer bytes.Buffer

	writer := multipart.NewWriter(&buffer)

	if err := writeRequestPart(writer, request); err != nil {
		return nil, "", err
	}

	if err := writeImageParts(writer, names, assets); err != nil {
		return nil, "", err
	}

	if err := writer.Close(); err != nil {
		return nil, "", err
	}

	return buffer.Bytes(), writer.FormDataContentType(), nil
}

func assetDescriptors(assets Assets) ([]string, map[string]imageDescriptor, error) {
	names := make([]string, 0, len(assets))
	for name := range assets {
		names = append(names, name)
	}

	sort.Strings(names)

	descriptors := make(map[string]imageDescriptor, len(names))
	for i, name := range names {
		if name == "" || len(assets[name].data) == 0 {
			return nil, nil, &ValidationError{Field: "assets", Message: "references and images must not be empty"}
		}

		descriptors[name] = imageDescriptor{Type: "image", Part: fmt.Sprintf("image-%d", i)}
	}

	return names, descriptors, nil
}

func writeRequestPart(writer *multipart.Writer, request []byte) error {
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", `form-data; name="request"`)
	header.Set("Content-Type", "application/json")

	return writeMultipartPart(writer, header, request)
}

func writeImageParts(writer *multipart.Writer, names []string, assets Assets) error {
	for i, name := range names {
		data := assets[name].data
		header := make(textproto.MIMEHeader)
		header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="image-%d"; filename="image"`, i))
		header.Set("Content-Type", http.DetectContentType(data))

		if err := writeMultipartPart(writer, header, data); err != nil {
			return err
		}
	}

	return nil
}

func writeMultipartPart(writer *multipart.Writer, header textproto.MIMEHeader, body []byte) error {
	part, err := writer.CreatePart(header)
	if err != nil {
		return err
	}

	_, err = part.Write(body)

	return err
}
