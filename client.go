package typesafe

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"runtime"
	"strings"
	"time"
)

const Version = "0.1.0-dev"

// Config explicitly configures a Client. Zero timeout fields select defaults.
// Construction never reads environment variables. HTTPClient remains caller-owned.
type Config struct {
	APIKey, BaseURL, Model      string
	HTTPClient                  *http.Client
	Headers                     http.Header
	AttemptTimeout, CallTimeout time.Duration
}
type Client struct {
	apiKey, baseURL, model      string
	httpClient                  *http.Client
	headers                     http.Header
	attemptTimeout, callTimeout time.Duration
}
type callSettings struct {
	model                       string
	headers                     http.Header
	attemptTimeout, callTimeout time.Duration
}

// CallOption configures one call without changing client defaults.
type CallOption func(*callSettings) error

func WithModel(model string) CallOption {
	return func(s *callSettings) error {
		if strings.TrimSpace(model) == "" {
			return &ValidationError{"model", "must not be blank"}
		}
		s.model = model
		return nil
	}
}
func WithHeaders(headers http.Header) CallOption {
	snapshot := headers.Clone()
	return func(s *callSettings) error {
		for k, v := range snapshot {
			s.headers[k] = append([]string(nil), v...)
		}
		return nil
	}
}
func WithTimeouts(attempt, call time.Duration) CallOption {
	return func(s *callSettings) error {
		if attempt <= 0 || call <= 0 {
			return &ValidationError{"timeouts", "must be positive"}
		}
		s.attemptTimeout = attempt
		s.callTimeout = call
		return nil
	}
}

func NewClient(config Config) (*Client, error) {
	if strings.TrimSpace(config.APIKey) == "" || strings.IndexFunc(config.APIKey, func(r rune) bool { return r <= 32 || r >= 127 }) >= 0 {
		return nil, &ValidationError{"api_key", "must be a nonempty token without whitespace or control characters"}
	}
	if config.BaseURL == "" {
		config.BaseURL = "https://api.typesafe.ai"
	}
	base, err := url.Parse(config.BaseURL)
	if err != nil || base == nil || (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" || base.RawQuery != "" || base.Fragment != "" || base.User != nil {
		return nil, &ValidationError{"base_url", "must be an absolute HTTP(S) URL without credentials, query or fragment"}
	}
	if config.Model == "" {
		config.Model = "jev-latest"
	}
	if strings.TrimSpace(config.Model) == "" {
		return nil, &ValidationError{"model", "must not be blank"}
	}
	if config.AttemptTimeout < 0 || config.CallTimeout < 0 {
		return nil, &ValidationError{"timeouts", "must not be negative"}
	}
	if config.AttemptTimeout == 0 {
		config.AttemptTimeout = 10 * time.Second
	}
	if config.CallTimeout == 0 {
		config.CallTimeout = 30 * time.Second
	}
	if config.HTTPClient == nil {
		config.HTTPClient = &http.Client{}
	}
	return &Client{config.APIKey, strings.TrimRight(base.String(), "/"), config.Model, config.HTTPClient, config.Headers.Clone(), config.AttemptTimeout, config.CallTimeout}, nil
}

func (c *Client) settings(options []CallOption) (callSettings, error) {
	s := callSettings{c.model, c.headers.Clone(), c.attemptTimeout, c.callTimeout}
	if s.headers == nil {
		s.headers = make(http.Header)
	}
	for _, option := range options {
		if option == nil {
			return s, &ValidationError{"options", "must not be nil"}
		}
		if err := option(&s); err != nil {
			return s, err
		}
	}
	return s, nil
}

func (c *Client) SystemOne(ctx context.Context, request SystemOneRequest, options ...CallOption) (*SystemOneResponse, error) {
	s, err := c.settings(options)
	if err != nil {
		return nil, err
	}
	state, err := encodeContent(request.State, "state", false)
	if err != nil {
		return nil, err
	}
	if len(request.Questions) == 0 {
		return nil, &ValidationError{"questions", "must not be empty"}
	}
	questions := make(map[string]json.RawMessage, len(request.Questions))
	requested := make(map[string]string, len(request.Questions))
	for name, question := range request.Questions {
		if name == "" || question == nil || (reflect.ValueOf(question).Kind() == reflect.Pointer && reflect.ValueOf(question).IsNil()) {
			return nil, &ValidationError{"questions", "names and questions must not be empty"}
		}
		kind, body, err := question.encodeQuestion()
		if err != nil {
			return nil, err
		}
		questions[name] = body
		requested[name] = kind
	}
	body, err := json.Marshal(struct {
		Model     string                     `json:"model"`
		State     json.RawMessage            `json:"state"`
		Questions map[string]json.RawMessage `json:"questions"`
	}{s.model, state, questions})
	if err != nil {
		return nil, &ValidationError{"request", "must be JSON-encodable"}
	}
	meta, err := c.do(ctx, http.MethodPost, "/v1/systemone", body, s)
	if err != nil {
		return nil, err
	}
	response, err := parseSystemOne(meta, requested)
	if err != nil {
		return nil, err
	}
	if err := validateAnsweredQuestions(response, questions); err != nil {
		return nil, err
	}
	return response, nil
}

func (c *Client) do(ctx context.Context, method, path string, body []byte, s callSettings) (HTTPMetadata, error) {
	total, cancel := context.WithTimeout(ctx, s.callTimeout)
	defer cancel()
	attempt, stop := context.WithTimeout(total, s.attemptTimeout)
	defer stop()
	req, err := http.NewRequestWithContext(attempt, method, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return HTTPMetadata{}, &TransportError{err}
	}
	req.Header = s.headers.Clone()
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("User-Agent", "typesafe-sdk-go/"+Version+" Go/"+runtime.Version())
	req.Header.Set("X-TypeSafe-SDK", "typesafe-sdk-go/"+Version)
	req.Header.Set("X-TypeSafe-Runtime", runtime.Version())
	req.Header.Set("X-TypeSafe-Retry-Count", "0")
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	response, err := c.httpClient.Do(req)
	if err != nil {
		return HTTPMetadata{}, &TransportError{err}
	}
	defer response.Body.Close()
	meta := HTTPMetadata{StatusCode: response.StatusCode, Headers: response.Header.Clone(), RequestID: response.Header.Get("x-typesafe-request-id")}
	meta.RawBody, err = io.ReadAll(response.Body)
	if err != nil {
		return meta, &TransportError{err}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return meta, &HTTPError{meta}
	}
	return meta, nil
}
