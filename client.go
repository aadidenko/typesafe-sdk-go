package typesafe

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Version identifies the SDK in outbound metadata.
const Version = "0.1.0-dev"

// Config explicitly configures a Client. Zero timeout fields select defaults.
// Construction never reads environment variables. HTTPClient remains caller-owned.
type Config struct {
	Compression Compression
	// Logger enables SDK diagnostics without changing global logging.
	Logger *slog.Logger
	// LogBodies separately opts into bounded wire bodies, which may contain sensitive content.
	LogBodies              bool
	Retry                  *RetryPolicy
	ExperimentalImages     bool
	APIKey, BaseURL, Model string
	HTTPClient             *http.Client
	// LogLevel optionally filters a supplied logger; nil preserves its own level.
	LogLevel                    *slog.Level
	Headers                     http.Header
	AttemptTimeout, CallTimeout time.Duration
}

// Client evaluates requests concurrently using immutable configuration snapshots.
// Caller-supplied HTTP clients remain caller-owned.
type Client struct {
	compression                 Compression
	logger                      *slog.Logger
	logBodies                   bool
	logLevel                    *slog.Level
	retry                       RetryPolicy
	experimentalImages          bool
	apiKey, baseURL, model      string
	httpClient                  *http.Client
	headers                     http.Header
	attemptTimeout, callTimeout time.Duration
}
type callSettings struct {
	compression                 Compression
	retry                       RetryPolicy
	experimentalImages          bool
	contentType                 string
	contentEncoding             string
	model                       string
	headers                     http.Header
	attemptTimeout, callTimeout time.Duration
}

// CallOption configures one call without changing client defaults.
type CallOption func(*callSettings) error

// WithExperimentalImages explicitly enables or disables experimental media for one call.
func WithExperimentalImages(enabled bool) CallOption {
	return func(s *callSettings) error { s.experimentalImages = enabled; return nil }
}

// WithModel selects a nonblank model for one call.
func WithModel(model string) CallOption {
	return func(s *callSettings) error {
		if strings.TrimSpace(model) == "" {
			return &ValidationError{Field: "model", Message: "must not be blank"}
		}

		s.model = model

		return nil
	}
}

// WithHeaders snapshots additional headers for one call; SDK-managed headers are rejected.
func WithHeaders(headers http.Header) CallOption {
	snapshot, err := copyHeaders(headers)

	return func(s *callSettings) error {
		if err != nil {
			return err
		}

		for k, v := range snapshot {
			s.headers[k] = append([]string(nil), v...)
		}

		return nil
	}
}

// WithTimeouts sets positive per-attempt and total budgets for one call.
func WithTimeouts(attempt, call time.Duration) CallOption {
	return func(s *callSettings) error {
		if attempt <= 0 || call <= 0 {
			return &ValidationError{Field: "timeouts", Message: "must be positive"}
		}

		s.attemptTimeout = attempt
		s.callTimeout = call

		return nil
	}
}

// NewClient validates configuration and snapshots mutable settings without reading the environment.
func NewClient(config Config) (*Client, error) {
	config, err := validatedConfig(config)
	if err != nil {
		return nil, err
	}

	retry := DefaultRetryPolicy()
	if config.Retry != nil {
		retry = *config.Retry
	}

	retry, err = copyRetryPolicy(retry)
	if err != nil {
		return nil, err
	}

	headers, err := copyHeaders(config.Headers)
	if err != nil {
		return nil, err
	}

	logLevel := config.LogLevel
	if logLevel != nil {
		level := *logLevel

		logLevel = &level
	}

	return &Client{
		compression: config.Compression, logger: config.Logger,
		logBodies: config.LogBodies, logLevel: logLevel, retry: retry,
		experimentalImages: config.ExperimentalImages,
		apiKey:             config.APIKey, baseURL: config.BaseURL, model: config.Model,
		httpClient: config.HTTPClient, headers: headers,
		attemptTimeout: config.AttemptTimeout, callTimeout: config.CallTimeout,
	}, nil
}

func validatedConfig(config Config) (Config, error) {
	if err := validateCompression(config.Compression); err != nil {
		return config, err
	}

	config.APIKey = strings.TrimSpace(config.APIKey)
	if config.APIKey == "" || strings.IndexFunc(config.APIKey, invalidTokenRune) >= 0 {
		return config, &ValidationError{Field: "api_key", Message: "must be a nonempty token without whitespace or control characters"}
	}

	baseURL, err := configuredBaseURL(config.BaseURL)
	if err != nil {
		return config, err
	}

	config.BaseURL = baseURL
	if config.Model == "" {
		config.Model = DefaultModel
	}

	if strings.TrimSpace(config.Model) == "" {
		return config, &ValidationError{Field: "model", Message: "must not be blank"}
	}

	return configuredTimeouts(config)
}

func invalidTokenRune(r rune) bool { return r <= 32 || r >= 127 }

func configuredBaseURL(raw string) (string, error) {
	if raw == "" {
		raw = DefaultBaseURL
	}

	base, err := url.Parse(raw)
	if err != nil || base == nil || (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" || base.RawQuery != "" || base.Fragment != "" || base.User != nil {
		return "", &ValidationError{Field: "base_url", Message: "must be an absolute HTTP(S) URL without credentials, query or fragment"}
	}

	return strings.TrimRight(base.String(), "/"), nil
}

func configuredTimeouts(config Config) (Config, error) {
	if config.AttemptTimeout < 0 || config.CallTimeout < 0 {
		return config, &ValidationError{Field: "timeouts", Message: "must not be negative"}
	}

	if config.AttemptTimeout == 0 {
		config.AttemptTimeout = DefaultAttemptTimeout
	}

	if config.CallTimeout == 0 {
		config.CallTimeout = DefaultCallTimeout
	}

	if config.HTTPClient == nil {
		config.HTTPClient = &http.Client{}
	}

	return config, nil
}

func (c *Client) settings(options []CallOption) (callSettings, error) {
	s := callSettings{compression: c.compression, retry: c.retry, experimentalImages: c.experimentalImages, contentType: "application/json", model: c.model, headers: c.headers.Clone(), attemptTimeout: c.attemptTimeout, callTimeout: c.callTimeout}
	if s.headers == nil {
		s.headers = make(http.Header)
	}

	for _, option := range options {
		if option == nil {
			return s, &ValidationError{Field: "options", Message: "must not be nil"}
		}

		if err := option(&s); err != nil {
			return s, err
		}
	}

	return s, nil
}

// SystemOne evaluates named questions against the supplied state and explicit assets.
func (c *Client) SystemOne(ctx context.Context, request SystemOneRequest, options ...CallOption) (*SystemOneResponse, error) {
	s, err := c.settings(options)
	if err != nil {
		return nil, err
	}

	prepared, err := prepareEvaluation(request, s)
	if err != nil {
		return nil, err
	}

	s.contentType = prepared.contentType

	meta, err := c.do(ctx, http.MethodPost, "/v1/systemone", prepared.body, s)
	if err != nil {
		return nil, err
	}

	response, err := parseSystemOne(meta, prepared.requested)
	if err != nil {
		c.logProtocolFailure(ctx, http.MethodPost, "/v1/systemone", meta, err)

		return nil, err
	}

	if err := validateAnsweredQuestions(response, prepared.questions); err != nil {
		c.logProtocolFailure(ctx, http.MethodPost, "/v1/systemone", meta, err)

		return nil, err
	}

	return response, nil
}

func (c *Client) doAttempt(ctx context.Context, method, path string, body []byte, s callSettings, retryNumber int) (meta HTTPMetadata, err error) {
	started := time.Now()
	defer func() { c.logHTTPResponse(ctx, method, path, retryNumber+1, started, meta, err) }()

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return HTTPMetadata{}, &TransportError{Cause: err}
	}

	req.Header = s.headers.Clone()
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("User-Agent", "typesafe-sdk-go/"+Version+" Go/"+runtime.Version())
	req.Header.Set("X-TypeSafe-SDK", "typesafe-sdk-go/"+Version)
	req.Header.Set("X-TypeSafe-Runtime", runtime.Version())
	req.Header.Set("X-TypeSafe-Retry-Count", strconv.Itoa(retryNumber))
	req.Header.Set("Accept", "application/json")

	if body != nil {
		req.Header.Set("Content-Type", s.contentType)
	}

	if s.contentEncoding != "" {
		req.Header.Set("Content-Encoding", s.contentEncoding)
	}

	if s.experimentalImages {
		req.Header.Set("TypeSafe-Danger-Zone-Images", "true")
	}

	c.logHTTPRequest(ctx, method, path, retryNumber+1, req.Header, body)

	response, err := c.httpClient.Do(req)
	if err != nil {
		return HTTPMetadata{}, &TransportError{Cause: err}
	}
	defer response.Body.Close() //nolint:errcheck // Body bytes and read errors are authoritative; cleanup must not change a completed response.

	meta = HTTPMetadata{StatusCode: response.StatusCode, Headers: response.Header.Clone(), RequestID: response.Header.Get("x-typesafe-request-id")}

	meta.RawBody, err = io.ReadAll(response.Body)
	if err != nil {
		return meta, &TransportError{HTTPMetadata: meta, Cause: err}
	}

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return meta, &HTTPError{HTTPMetadata: meta}
	}

	return meta, nil
}

type preparedEvaluation struct {
	body        []byte
	contentType string
	questions   map[string]json.RawMessage
	requested   map[string]string
}

func prepareEvaluation(request SystemOneRequest, s callSettings) (preparedEvaluation, error) {
	if len(request.Assets) > 0 && !s.experimentalImages {
		return preparedEvaluation{}, &ValidationError{Field: "assets", Message: "requires experimental images opt-in"}
	}

	state, err := encodeContent(request.State, "state", false)
	if err != nil {
		return preparedEvaluation{}, err
	}

	questions, requested, err := encodeQuestions(request.Questions, s.experimentalImages)
	if err != nil {
		return preparedEvaluation{}, err
	}

	body, err := encodeEvaluationBody(s.model, state, questions, request.ExtraBody)
	if err != nil {
		return preparedEvaluation{}, err
	}

	body, contentType, err := prepareAssets(body, request.Assets)
	if err != nil {
		return preparedEvaluation{}, err
	}

	return preparedEvaluation{body: body, contentType: contentType, questions: questions, requested: requested}, nil
}

func encodeQuestions(questionsInput Questions, experimentalImages bool) (map[string]json.RawMessage, map[string]string, error) {
	if len(questionsInput) == 0 {
		return nil, nil, &ValidationError{Field: "questions", Message: "must not be empty"}
	}

	questions := make(map[string]json.RawMessage, len(questionsInput))
	requested := make(map[string]string, len(questionsInput))

	for name, question := range questionsInput {
		if name == "" || question == nil || (reflect.ValueOf(question).Kind() == reflect.Pointer && reflect.ValueOf(question).IsNil()) {
			return nil, nil, &ValidationError{Field: "questions", Message: "names and questions must not be empty"}
		}

		kind, body, err := question.encodeQuestion()
		if err != nil {
			return nil, nil, err
		}

		if kind == "box" && !experimentalImages {
			return nil, nil, &ValidationError{Field: "questions." + name, Message: "Box requires experimental images opt-in"}
		}

		questions[name] = body
		requested[name] = kind
	}

	return questions, requested, nil
}

func encodeEvaluationBody(model string, state json.RawMessage, questions map[string]json.RawMessage, extraBody map[string]any) ([]byte, error) {
	bodyFields := map[string]any{"model": model, "state": state, "questions": questions}

	for name, value := range extraBody {
		switch name {
		case "model", "state", "questions", "assets":
			return nil, &ValidationError{Field: "extra_body." + name, Message: "is managed by the SDK; use its typed request field or call option"}
		}

		bodyFields[name] = value
	}

	body, err := json.Marshal(bodyFields)
	if err != nil {
		return nil, &ValidationError{Field: "request", Message: "must be JSON-encodable"}
	}

	return body, nil
}
