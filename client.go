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
	if err := validateCompression(config.Compression); err != nil {
		return nil, err
	}
	config.APIKey = strings.TrimSpace(config.APIKey)
	if strings.TrimSpace(config.APIKey) == "" || strings.IndexFunc(config.APIKey, func(r rune) bool { return r <= 32 || r >= 127 }) >= 0 {
		return nil, &ValidationError{"api_key", "must be a nonempty token without whitespace or control characters"}
	}
	if config.BaseURL == "" {
		config.BaseURL = DefaultBaseURL
	}
	base, err := url.Parse(config.BaseURL)
	if err != nil || base == nil || (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" || base.RawQuery != "" || base.Fragment != "" || base.User != nil {
		return nil, &ValidationError{"base_url", "must be an absolute HTTP(S) URL without credentials, query or fragment"}
	}
	if config.Model == "" {
		config.Model = DefaultModel
	}
	if strings.TrimSpace(config.Model) == "" {
		return nil, &ValidationError{"model", "must not be blank"}
	}
	if config.AttemptTimeout < 0 || config.CallTimeout < 0 {
		return nil, &ValidationError{"timeouts", "must not be negative"}
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
	return &Client{compression: config.Compression, logger: config.Logger, logBodies: config.LogBodies, logLevel: logLevel, retry: retry, experimentalImages: config.ExperimentalImages, apiKey: config.APIKey, baseURL: strings.TrimRight(base.String(), "/"), model: config.Model, httpClient: config.HTTPClient, headers: headers, attemptTimeout: config.AttemptTimeout, callTimeout: config.CallTimeout}, nil
}

func (c *Client) settings(options []CallOption) (callSettings, error) {
	s := callSettings{compression: c.compression, retry: c.retry, experimentalImages: c.experimentalImages, contentType: "application/json", model: c.model, headers: c.headers.Clone(), attemptTimeout: c.attemptTimeout, callTimeout: c.callTimeout}
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
	if len(request.Assets) > 0 && !s.experimentalImages {
		return nil, &ValidationError{"assets", "requires experimental images opt-in"}
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
		if kind == "box" && !s.experimentalImages {
			return nil, &ValidationError{"questions." + name, "Box requires experimental images opt-in"}
		}
		questions[name] = body
		requested[name] = kind
	}
	bodyFields := map[string]any{"model": s.model, "state": state, "questions": questions}
	for name, value := range request.ExtraBody {
		switch name {
		case "model", "state", "questions", "assets":
			return nil, &ValidationError{"extra_body." + name, "is managed by the SDK; use its typed request field or call option"}
		}
		bodyFields[name] = value
	}
	body, err := json.Marshal(bodyFields)
	if err != nil {
		return nil, &ValidationError{"request", "must be JSON-encodable"}
	}
	body, s.contentType, err = prepareAssets(body, request.Assets)
	if err != nil {
		return nil, err
	}
	meta, err := c.do(ctx, http.MethodPost, "/v1/systemone", body, s)
	if err != nil {
		return nil, err
	}
	response, err := parseSystemOne(meta, requested)
	if err != nil {
		c.logProtocolFailure(ctx, http.MethodPost, "/v1/systemone", meta, err)
		return nil, err
	}
	if err := validateAnsweredQuestions(response, questions); err != nil {
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
	defer response.Body.Close()
	meta = HTTPMetadata{StatusCode: response.StatusCode, Headers: response.Header.Clone(), RequestID: response.Header.Get("x-typesafe-request-id")}
	meta.RawBody, err = io.ReadAll(response.Body)
	if err != nil {
		return meta, &TransportError{HTTPMetadata: meta, Cause: err}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return meta, &HTTPError{meta}
	}
	return meta, nil
}
