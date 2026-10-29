package allisall

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"runtime"
	"strings"
)

// response is a successful (2xx) response, read in full.
type response struct {
	status int
	header http.Header
	body   []byte
}

// providerError carries an error from the caller's token provider through
// the retry loop, so the operation can return it unwrapped.
type providerError struct{ err error }

func (e *providerError) Error() string { return e.err.Error() }

func userAgent() string {
	return "allisall-go/" + version + " go/" + strings.TrimPrefix(runtime.Version(), "go")
}

// attempt sends one HTTP request and reads its response. It returns:
//   - the response, for a 2xx status;
//   - an *APIError, for any other status;
//   - a *ConnectionError, when no response arrived within the attempt;
//   - the caller's context error, when ctx ended;
//   - a *providerError, when the token provider failed.
func (c *Client) attempt(ctx context.Context, method, path string, body []byte) (*response, error) {
	token := c.apiKey
	if c.tokenProvider != nil {
		t, err := c.tokenProvider(ctx)
		if err != nil {
			return nil, &providerError{err: err}
		}
		t = strings.TrimSpace(t)
		if t == "" {
			return nil, &providerError{err: fmt.Errorf("%w: the token provider returned an empty token", ErrAllisall)}
		}
		if hasInvalidCredentialChar(t) {
			// Never retried, and the message never holds the token.
			return nil, &providerError{err: fmt.Errorf("%w: the token provider returned a token containing a character outside printable ASCII", ErrAllisall)}
		}
		token = t
	}

	requestID := newUUID()
	attemptCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(attemptCtx, method, c.baseURL.JoinPath(path).String(), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("allisall: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-Id", requestID)
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, &ConnectionError{RequestID: requestID, Err: err}
	}
	defer func() { _ = resp.Body.Close() }()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, &ConnectionError{RequestID: requestID, Err: err}
	}

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return &response{status: resp.StatusCode, header: resp.Header, body: data}, nil
	}
	return nil, c.newAPIError(resp.StatusCode, resp.Header, data)
}

// newAPIError builds the error for a non-2xx response. A body is read as
// a problem document only when its media type is application/problem+json
// and it is a JSON object.
func (c *Client) newAPIError(status int, header http.Header, body []byte) *APIError {
	e := &APIError{Status: status, Headers: header}
	if mediaType, _, err := mime.ParseMediaType(header.Get("Content-Type")); err == nil && mediaType == "application/problem+json" {
		if p, ok := decodeProblem(body); ok {
			e.Problem = p
			e.Code = p.Code
		}
	}
	if e.Problem != nil && e.Problem.RequestID != "" {
		e.RequestID = e.Problem.RequestID
	} else {
		e.RequestID = header.Get("X-Request-Id")
	}
	if v := header.Get("Retry-After"); v != "" {
		e.RetryAfter, e.hasRetryAfter = parseRetryAfter(v, c.now())
	}
	return e
}

// decodeProblem reads a problem document leniently: any JSON object is a
// problem. A member of the wrong type is left at its zero value, and an
// errors entry that does not decode as an object of the right shape is
// dropped, so one malformed member never costs the caller the code, the
// detail or the request id.
func decodeProblem(body []byte) (*Problem, bool) {
	var members map[string]json.RawMessage
	if !isJSONObject(body) || json.Unmarshal(body, &members) != nil {
		return nil, false
	}
	p := &Problem{}
	member := func(name string, into any) {
		if raw, ok := members[name]; ok {
			// On a type mismatch the target keeps its zero value.
			_ = json.Unmarshal(raw, into)
		}
	}
	member("type", &p.Type)
	member("title", &p.Title)
	member("status", &p.Status)
	member("detail", &p.Detail)
	member("code", &p.Code)
	member("request_id", &p.RequestID)
	var entries []json.RawMessage
	member("errors", &entries)
	for _, raw := range entries {
		var fe FieldError
		if isJSONObject(raw) && json.Unmarshal(raw, &fe) == nil {
			p.Errors = append(p.Errors, fe)
		}
	}
	return p, true
}

func isJSONObject(b []byte) bool {
	b = bytes.TrimSpace(b)
	return len(b) > 0 && b[0] == '{'
}
