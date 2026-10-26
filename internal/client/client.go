// Package client isolates provider models from zerops-go v1.0.24.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-log/tflog"
	"github.com/zeropsio/zerops-go/apiError"
	"github.com/zeropsio/zerops-go/sdk"
	"github.com/zeropsio/zerops-go/sdkBase"
)

type Config struct {
	Endpoint, Token, UserAgent string
	Transport                  http.RoundTripper
}
type Client struct {
	sdk             sdk.Handler
	publicSDK       sdk.Handler
	http            *http.Client
	endpoint, token string
	testHTTP        bool
}

func New(cfg Config) (*Client, error) { return newClient(cfg, false) }

// newTestClient permits loopback HTTP only for isolated package tests.
func newTestClient(cfg Config) (*Client, error) { return newClient(cfg, true) }
func newClient(cfg Config, test bool) (*Client, error) {
	u, e := url.Parse(cfg.Endpoint)
	if e != nil || u == nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || strings.Contains(cfg.Endpoint, "#") || (u.Path != "" && u.Path != "/") || (u.Scheme != "https" && !(test && u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost" || u.Hostname() == "::1"))) {
		return nil, errors.New("endpoint must be an HTTPS origin without credentials, query, or path")
	}
	if strings.ContainsAny(cfg.Token, "\r\n") || strings.ContainsAny(cfg.UserAgent, "\r\n") {
		return nil, errors.New("invalid authentication or user-agent header")
	}
	rt := cfg.Transport
	if rt == nil {
		rt = http.DefaultTransport
	}
	h := &http.Client{Transport: &safeTransport{base: rt, userAgent: cfg.UserAgent}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	endpoint := strings.TrimRight(cfg.Endpoint, "/")
	publicSDK := sdk.New(sdkBase.Config{Endpoint: endpoint}, h)
	return &Client{sdk: sdk.AuthorizeSdk(publicSDK, cfg.Token), publicSDK: publicSDK, http: h, endpoint: endpoint, token: cfg.Token, testHTTP: test}, nil
}

type APIError struct {
	HTTPStatus    int
	Code, Message string
	Meta          any
}

// Error deliberately excludes untrusted server messages and metadata.
func (e *APIError) Error() string {
	return fmt.Sprintf("Zerops API HTTP %d (%s)", e.HTTPStatus, safeCode(e.Code))
}
func safeCode(s string) string {
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '.') {
			return "unknown"
		}
	}
	return s
}
func AsAPIError(err error) (*APIError, bool) { var e *APIError; ok := errors.As(err, &e); return e, ok }
func IsNotFound(err error, codes ...string) bool {
	e, ok := AsAPIError(err)
	if !ok || e.HTTPStatus != 400 {
		return false
	}
	for _, code := range codes {
		if e.Code == code {
			return true
		}
	}
	return false
}

type TransportError struct{ cause error }

func (e *TransportError) Error() string {
	// Only this guard-owned diagnostic may cross the normal transport-error
	// redaction boundary: a successful mutation needs its cleanup ID visible.
	var cleanup *productionCleanupError
	if errors.As(e.cause, &cleanup) {
		return cleanup.Error()
	}
	return "Zerops API transport failed"
}
func (e *TransportError) Unwrap() error { return e.cause }
func normalizeError(err error) error {
	if err == nil {
		return nil
	}
	var a apiError.Error
	if errors.As(err, &a) {
		return &APIError{HTTPStatus: a.HttpStatusCode, Code: a.ErrorCode, Message: a.Message, Meta: a.Meta}
	}
	return errors.New("Zerops API response could not be decoded")
}

type requestState struct {
	transport error
	operation string
	raw       bytes.Buffer
}
type stateKey struct{}

func (c *Client) operation(ctx context.Context, name string) (context.Context, *requestState) {
	ctx = tflog.MaskAllFieldValuesStrings(ctx, c.token)
	s := &requestState{operation: name}
	return context.WithValue(ctx, stateKey{}, s), s
}

// getAttemptTimeout bounds one GET attempt (retried up to three times).
var getAttemptTimeout = 60 * time.Second

type safeTransport struct {
	base      http.RoundTripper
	userAgent string
	sleep     func(context.Context, time.Duration) error
}

func sleepContext(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
func (t *safeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("User-Agent", t.userAgent)
	state, _ := req.Context().Value(stateKey{}).(*requestState)
	if state != nil && (state.operation == "RequestDedicatedIPv4" || state.operation == "RestartService" || state.operation == "SetStorageCDN" || state.operation == "RegenerateIntegrationToken") {
		// The SDK serializes an empty DTO as "{}"; these endpoints are bodyless.
		if req.Body != nil {
			req.Body.Close()
		}
		req.Body, req.GetBody, req.ContentLength = nil, nil, 0
		req.Header.Del("Content-Type")
	}
	attempts := 1
	if req.Method == http.MethodGet {
		attempts = 3
	}
	for n := range attempts {
		start := time.Now()
		attempt, cancel := req, context.CancelFunc(func() {})
		if req.Method == http.MethodGet {
			// A stalled read (observed on dev: an HTTP/2 GET hanging for many
			// minutes) must not consume the whole operation deadline; GETs are
			// safe to retry, mutations are never replayed so they keep ctx only.
			ctx, c := context.WithTimeout(req.Context(), getAttemptTimeout)
			attempt, cancel = req.WithContext(ctx), c
		}
		resp, err := t.base.RoundTrip(attempt)
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		fields := map[string]any{"method": req.Method, "status": status, "duration_ms": time.Since(start).Milliseconds(), "retry_count": n}
		if state != nil {
			fields["operation"] = state.operation
		}
		tflog.Debug(req.Context(), "Zerops API request", fields)
		retry := err != nil
		if resp != nil && err == nil && req.Method == http.MethodGet {
			body, readErr := io.ReadAll(resp.Body)
			resp.Body.Close()
			resp.Body = io.NopCloser(bytes.NewReader(body))
			if state != nil {
				state.raw = *bytes.NewBuffer(body)
			}
			var envelope struct {
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			if status == 503 {
				_ = json.Unmarshal(body, &envelope)
			}
			err = readErr
			retry = readErr != nil || (status >= 500 && !(status == 503 && envelope.Error.Code == "notAvailable"))
		}
		cancel()
		if retry && req.Context().Err() != nil {
			retry = false
		}
		if retry && n+1 < attempts {
			if resp != nil {
				resp.Body.Close()
			}
			pause := t.sleep
			if pause == nil {
				pause = sleepContext
			}
			if e := pause(req.Context(), time.Second*time.Duration(1<<n)+time.Duration(rand.Int64N(int64(200*time.Millisecond)))); e != nil {
				err = e
			} else {
				continue
			}
		}
		if err != nil {
			if resp != nil {
				resp.Body.Close()
			}
			if state != nil {
				state.transport = err
			}
			// SDK prints errors from http.Client.Do. Return a synthetic safe response,
			// retaining the original privately in the operation context, instead.
			return &http.Response{StatusCode: 599, Status: "599 Transport failure", Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"error":{"code":"transportFailure"}}`)), Request: req}, nil
		}
		if state != nil && state.operation != "UploadArchive" && req.Method != http.MethodGet {
			resp.Body = &captureBody{Reader: io.TeeReader(resp.Body, &state.raw), Closer: resp.Body}
		}
		return resp, nil
	}
	panic("unreachable")
}

type captureBody struct {
	io.Reader
	io.Closer
}

type sdkResponse interface{ OutputInterface() (interface{}, error) }

func finish[T any](s *requestState, r sdkResponse, outer error) (T, error) {
	var zero T
	if s.transport != nil {
		return zero, &TransportError{cause: s.transport}
	}
	if outer != nil {
		return zero, normalizeError(outer)
	}
	_, err := r.OutputInterface()
	if err != nil {
		return zero, normalizeError(err)
	}
	// SDK output DTOs cannot reliably be re-marshaled: an omitted StringArray
	// errors on MarshalJSON, and some projections omit fields needed by callers.
	// Decode the bytes consumed by the SDK instead, after checking both error
	// channels. This also preserves absent/null response semantics.
	b := s.raw.Bytes()
	var result T
	if json.Unmarshal(b, &result) != nil {
		return zero, errors.New("Zerops API output conversion failed")
	}
	return result, nil
}

// input bridges provider-owned models into SDK DTOs. Callers use local defined
// DTO types to bypass SDK top-level JSON requiredness (constructing the SDK
// struct directly also bypasses those validators). Nested DTO validation stays.
func input[T any](v any) (T, error) {
	var out T
	switch q := v.(type) {
	case PageQuery:
		if q.Limit == 0 {
			q.Limit = 100
		}
		v = q
	case ProjectQuery:
		if q.Limit == 0 {
			q.Limit = 100
		}
		v = q
	case ServiceQuery:
		if q.Limit == 0 {
			q.Limit = 100
		}
		v = q
	case UserDataQuery:
		if q.Limit == 0 {
			q.Limit = 100
		}
		v = q
	case map[string]any:
		for _, key := range []string{"Id", "ProjectId", "Name"} {
			if raw, ok := q[key]; ok {
				value, ok := raw.(string)
				if !ok || value == "" {
					return out, errors.New("invalid API path parameter")
				}
				for _, r := range value {
					if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
						return out, errors.New("invalid API path parameter")
					}
				}
			}
		}
	}
	b, e := json.Marshal(v)
	if e == nil {
		e = json.Unmarshal(b, &out)
	}
	if e != nil {
		return out, errors.New("invalid Zerops request model")
	}
	return out, nil
}
