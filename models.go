package typesafe

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// ModelMetadata describes a model name or alias accepted by SystemOne.
type ModelMetadata struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	ReleaseDate string `json:"release_date"`
}

// ListModelsResponse includes the available models and the complete HTTP response.
type ListModelsResponse struct {
	HTTPMetadata
	Models []ModelMetadata
}

// ListModels discovers the models and aliases available to the authenticated account.
// It shares SystemOne's context, timeout, HTTP error and metadata behavior.
func (c *Client) ListModels(ctx context.Context, options ...CallOption) (*ListModelsResponse, error) {
	settings, err := c.settings(options)
	if err != nil {
		return nil, err
	}

	meta, err := c.do(ctx, http.MethodGet, "/v1/models", nil, settings)
	if err != nil {
		return nil, err
	}

	response, err := parseModels(meta)
	c.logProtocolFailure(ctx, http.MethodGet, "/v1/models", meta, err)

	return response, err
}

func parseModels(meta HTTPMetadata) (*ListModelsResponse, error) {
	fail := func(field string) (*ListModelsResponse, error) {
		return nil, &ProtocolError{HTTPMetadata: meta, Field: field, Message: "required field has an invalid structural type"}
	}

	var root map[string]json.RawMessage

	if err := json.Unmarshal(meta.RawBody, &root); err != nil || root == nil {
		return fail("$")
	}

	var models []map[string]json.RawMessage

	if err := json.Unmarshal(root["models"], &models); err != nil || models == nil {
		return fail("models")
	}

	result := &ListModelsResponse{HTTPMetadata: meta, Models: make([]ModelMetadata, len(models))}
	for i, model := range models {
		for name, dest := range map[string]*string{"name": &result.Models[i].Name, "description": &result.Models[i].Description, "release_date": &result.Models[i].ReleaseDate} {
			var value *string

			if err := json.Unmarshal(model[name], &value); err != nil || value == nil {
				return fail(fmt.Sprintf("models[%d].%s", i, name))
			}

			*dest = *value
		}
	}

	return result, nil
}
