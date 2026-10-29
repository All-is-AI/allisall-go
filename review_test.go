package allisall

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestAProblemWithAMalformedMemberIsKept(t *testing.T) {
	body := `{"type":"https://api.allisai.ai/errors/validation_failed","title":"The request is invalid",` +
		`"status":"four hundred","detail":"2 problems","code":"validation_failed","request_id":"req-1",` +
		`"errors":[{"code":123,"pointer":"/a","detail":"bad"},null,"text",` +
		`{"code":"invalid_value","pointer":"/events","detail":"good"}]}`
	err := ingestOnce(t, newResponse(400, "application/problem+json", body, "X-Request-Id", "hdr-1"))
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Problem == nil {
		t.Fatalf("err = %v, want an APIError with its problem", err)
	}
	if apiErr.Code != ErrorCodeValidationFailed || apiErr.Problem.Detail != "2 problems" || apiErr.RequestID != "req-1" {
		t.Errorf("APIError = %+v, problem %+v; want the code, detail and request id kept", apiErr, apiErr.Problem)
	}
	if apiErr.Status != 400 || apiErr.Problem.Status != 0 {
		t.Errorf("status %d, problem status %d; want 400 and the mistyped member left at zero", apiErr.Status, apiErr.Problem.Status)
	}
	want := []FieldError{{Code: FieldErrorCodeInvalidValue, Pointer: "/events", Detail: "good"}}
	if !reflect.DeepEqual(apiErr.Problem.Errors, want) {
		t.Errorf("Errors = %+v, want only the good entry %+v", apiErr.Problem.Errors, want)
	}
}

func TestAnEmptyProblemRequestIDFallsBackOnTheHeader(t *testing.T) {
	for _, member := range []string{`"request_id":"",`, ``} {
		body := `{"type":"t","title":"t","status":401,"detail":"d",` + member + `"code":"unauthenticated"}`
		err := ingestOnce(t, newResponse(401, "application/problem+json", body, "X-Request-Id", "hdr-1"))
		var apiErr *APIError
		if !errors.As(err, &apiErr) || apiErr.Problem == nil || apiErr.RequestID != "hdr-1" {
			t.Errorf("body %s: err = %v, want the problem and RequestID from the header", body, err)
		}
	}
}

func TestFormatHidesTheCredential(t *testing.T) {
	const key = "aia_0123456789abcdefSECRETsecret"
	c, err := NewClient(WithAPIKey(key), WithBaseURL("https://user:pass@example.test/api"))
	if err != nil {
		t.Fatal(err)
	}
	for _, verb := range []string{"%v", "%+v", "%#v", "%s"} {
		for _, v := range []any{c, *c, c.Events, *c.Events} {
			out := fmt.Sprintf(verb, v)
			for _, part := range []string{key, "0123456789", "SECRET", "secret", "pass"} {
				if strings.Contains(out, part) {
					t.Errorf("Sprintf(%q, %T) = %q, which holds %q", verb, v, out, part)
				}
			}
		}
	}
	if out := fmt.Sprintf("%+v", c); !strings.Contains(out, "example.test") || !strings.Contains(out, "redacted") {
		t.Errorf("%%+v = %q, want the base URL and a redacted credential", out)
	}
}

// leaksCredential reports whether an error message holds the credential,
// part of it, or any non-ASCII byte that could have come from it.
func leaksCredential(msg, credential string) bool {
	if strings.Contains(msg, credential) || strings.Contains(msg, "aia_") {
		return true
	}
	for i := 0; i < len(msg); i++ {
		if msg[i] > 0x7e {
			return true
		}
	}
	return false
}

func TestACredentialOutsidePrintableASCIIIsRefused(t *testing.T) {
	bad := []string{
		"aia_a\nb", "aia_a\r\nX-Injected: 1", "aia_\x00", "aia_\x1f", "aia_\x7f", " aia_\x1f ", // control characters, inside
		"aia_ab cd", " aia_ab cd\n", // an inner space, which trimming leaves
		"aia_caf\u00e9", "aia_k\U0001F600", "aia_\u00a0k", "aia_\xff", // non-ASCII, and a byte that is not UTF-8
	}

	t.Run("at NewClient", func(t *testing.T) {
		for _, key := range bad {
			c, err := NewClient(WithAPIKey(key))
			if err == nil || c != nil || !errors.Is(err, ErrAllisall) || leaksCredential(err.Error(), key) {
				t.Errorf("WithAPIKey(%q): %v, %v; want an ErrAllisall error without the key", key, c, err)
			}
			if strings.ContainsRune(key, 0) {
				continue // the OS cannot hold a NUL in an environment variable
			}
			t.Setenv(envAPIKey, key)
			c, err = NewClient()
			if err == nil || c != nil || !errors.Is(err, ErrAllisall) || leaksCredential(err.Error(), key) {
				t.Errorf("AIA_API_KEY=%q: %v, %v; want an ErrAllisall error without the key", key, c, err)
			}
		}
	})

	t.Run("from a token provider", func(t *testing.T) {
		for _, token := range bad {
			calls := 0
			h := newHarness(t, nil)
			h.client.apiKey = ""
			h.client.tokenProvider = func(context.Context) (string, error) { calls++; return token, nil }
			_, err := h.client.Events.Ingest(context.Background(), smallEvents(3))
			var ingestErr *IngestError
			if !errors.As(err, &ingestErr) || !errors.Is(ingestErr.Err, ErrAllisall) {
				t.Fatalf("token %q: err = %v, want an IngestError whose cause matches ErrAllisall", token, err)
			}
			if leaksCredential(err.Error(), token) {
				t.Errorf("token %q: the error message holds the token: %v", token, err)
			}
			if calls != 1 || h.transport.count() != 0 || len(ingestErr.Remaining) != 3 {
				t.Errorf("token %q: provider called %d times, %d requests, %d remaining; want 1, 0, 3", token, calls, h.transport.count(), len(ingestErr.Remaining))
			}
		}
	})

	t.Run("white space at either end is trimmed", func(t *testing.T) {
		for _, key := range []string{" aia_k ", "aia_k\n", "\taia_k\r\n", " aia_k"} {
			sentBy := func(t *testing.T, opts ...Option) string {
				t.Helper()
				h := newHarness(t, nil, opts...)
				if _, err := h.client.Events.Ingest(context.Background(), smallEvents(1)); err != nil {
					t.Fatalf("key %q: %v", key, err)
				}
				return h.transport.get(0).header.Get("Authorization")
			}
			// The option.
			if got := sentBy(t, WithAPIKey(key)); got != "Bearer aia_k" {
				t.Errorf("WithAPIKey(%q) sent %q, want %q", key, got, "Bearer aia_k")
			}
			// The environment: newHarness sets its own key, so build here.
			t.Setenv(envAPIKey, key)
			if c, err := NewClient(); err != nil {
				t.Errorf("AIA_API_KEY=%q: %v", key, err)
			} else if c.apiKey != "aia_k" {
				t.Errorf("AIA_API_KEY=%q: key %q, want aia_k", key, c.apiKey)
			}
			// A provider.
			h := newHarness(t, nil)
			h.client.apiKey = ""
			h.client.tokenProvider = func(context.Context) (string, error) { return key, nil }
			if _, err := h.client.Events.Ingest(context.Background(), smallEvents(1)); err != nil {
				t.Fatalf("provider %q: %v", key, err)
			}
			if got := h.transport.get(0).header.Get("Authorization"); got != "Bearer aia_k" {
				t.Errorf("provider token %q sent %q, want %q", key, got, "Bearer aia_k")
			}
		}
	})

	t.Run("a provider token empty after trimming is refused", func(t *testing.T) {
		for _, token := range []string{"", "  \n"} {
			calls := 0
			h := newHarness(t, nil)
			h.client.apiKey = ""
			h.client.tokenProvider = func(context.Context) (string, error) { calls++; return token, nil }
			_, err := h.client.Events.Ingest(context.Background(), smallEvents(3))
			var ingestErr *IngestError
			if !errors.As(err, &ingestErr) || !errors.Is(ingestErr.Err, ErrAllisall) ||
				!strings.Contains(ingestErr.Err.Error(), "the token provider returned an empty token") {
				t.Fatalf("token %q: err = %v, want an IngestError caused by the empty-token error", token, err)
			}
			if calls != 1 || h.transport.count() != 0 || len(h.recordedWaits()) != 0 || len(ingestErr.Remaining) != 3 {
				t.Errorf("token %q: provider called %d times, %d requests, %d waits, %d remaining; want 1, 0, 0, 3",
					token, calls, h.transport.count(), len(h.recordedWaits()), len(ingestErr.Remaining))
			}
		}
	})

	t.Run("a key empty after trimming is missing", func(t *testing.T) {
		for _, key := range []string{" ", "\n", " \t\r\n "} {
			if c, err := NewClient(WithAPIKey(key)); err == nil || c != nil || !errors.Is(err, ErrAllisall) || !strings.Contains(err.Error(), "empty") {
				t.Errorf("WithAPIKey(%q): %v, %v; want the empty-key error", key, c, err)
			}
			t.Setenv(envAPIKey, key)
			if c, err := NewClient(); err == nil || c != nil || !errors.Is(err, ErrAllisall) || !strings.Contains(err.Error(), "no credential") {
				t.Errorf("AIA_API_KEY=%q: %v, %v; want the missing-credential error", key, c, err)
			}
		}
	})

	t.Run("every printable ASCII character is accepted", func(t *testing.T) {
		var b strings.Builder
		for c := byte(0x21); c <= 0x7e; c++ {
			b.WriteByte(c)
		}
		key := b.String()
		if _, err := NewClient(WithAPIKey(key)); err != nil {
			t.Errorf("WithAPIKey: %v", err)
		}
		t.Setenv(envAPIKey, key)
		if _, err := NewClient(); err != nil {
			t.Errorf("AIA_API_KEY: %v", err)
		}
		h := newHarness(t, nil)
		h.client.apiKey = ""
		h.client.tokenProvider = func(context.Context) (string, error) { return key, nil }
		if _, err := h.client.Events.Ingest(context.Background(), smallEvents(1)); err != nil {
			t.Fatalf("provider: %v", err)
		}
		if got := h.transport.get(0).header.Get("Authorization"); got != "Bearer "+key {
			t.Errorf("Authorization = %q, want the key unchanged", got)
		}
	})
}

func TestTheSDKsOwnClientRefusesRedirects(t *testing.T) {
	var followed, hit atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/events", func(w http.ResponseWriter, r *http.Request) {
		hit.Add(1)
		w.Header().Set("Location", "/elsewhere")
		w.WriteHeader(http.StatusTemporaryRedirect)
	})
	mux.HandleFunc("/elsewhere", func(w http.ResponseWriter, r *http.Request) {
		followed.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"accepted":1,"duplicates":0,"rejected":0,"errors":[],"errors_truncated":false}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c, err := NewClient(WithAPIKey("aia_k"), WithBaseURL(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	c.sleep = func(context.Context, time.Duration) error { t.Error("a redirect was retried"); return nil }
	_, err = c.Events.Ingest(context.Background(), smallEvents(1))

	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusTemporaryRedirect || apiErr.Problem != nil {
		t.Fatalf("err = %v, want an APIError with status 307", err)
	}
	if followed.Load() != 0 {
		t.Errorf("the redirect was followed %d times", followed.Load())
	}
	if hit.Load() != 1 {
		t.Errorf("the endpoint was called %d times, want 1", hit.Load())
	}

	t.Run("a caller's client keeps its own policy", func(t *testing.T) {
		hc := &http.Client{}
		c, err := NewClient(WithAPIKey("aia_k"), WithBaseURL(srv.URL), WithHTTPClient(hc))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := c.Events.Ingest(context.Background(), smallEvents(1)); err != nil {
			t.Fatalf("Ingest through a client that follows redirects: %v", err)
		}
		if hc.CheckRedirect != nil || followed.Load() != 1 {
			t.Errorf("CheckRedirect set %t, followed %d; want the caller's client unchanged and following", hc.CheckRedirect != nil, followed.Load())
		}
	})
}

func TestARowErrorOutsideItsBatchIsRefused(t *testing.T) {
	for _, index := range []int{2, -1} { // the batch holds 2 events
		t.Run(fmt.Sprint(index), func(t *testing.T) {
			h := newHarness(t, func(int, *http.Request, recorded) (*http.Response, error) {
				return newResponse(200, "application/json", fmt.Sprintf(`{"accepted":1,"duplicates":0,"rejected":1,"errors_truncated":false,`+
					`"errors":[{"index":%d,"code":"invalid_value","pointer":"/events/%d","detail":"d"}]}`, index, index), "X-Request-Id", "r-1"), nil
			})
			_, err := h.client.Events.Ingest(context.Background(), smallEvents(2))
			var ingestErr *IngestError
			var apiErr *APIError
			if !errors.As(err, &ingestErr) || !errors.As(err, &apiErr) {
				t.Fatalf("err = %v, want an IngestError wrapping an APIError", err)
			}
			if apiErr.Status != 200 || apiErr.Problem != nil || apiErr.Code != "" || apiErr.RequestID != "r-1" {
				t.Errorf("APIError = %+v, want status 200 and no problem", apiErr)
			}
			if h.transport.count() != 1 || len(ingestErr.Remaining) != 2 || ingestErr.Summary.Batches != 0 {
				t.Errorf("%d requests, %d remaining, %d batches; want 1, 2, 0", h.transport.count(), len(ingestErr.Remaining), ingestErr.Summary.Batches)
			}
		})
	}
}

func TestRetryAfterWithATwoDigitYear(t *testing.T) {
	// fakeNow is in 2026, so "76" is 2076: 50 years ahead, not more, so it
	// stays in this century, and is far over the 60-second ceiling.
	const v = "Wednesday, 28-Oct-76 12:00:05 GMT"
	d, ok := parseRetryAfter(v, fakeNow)
	want := time.Date(2076, 10, 28, 12, 0, 5, 0, time.UTC).Sub(fakeNow)
	if !ok || d != want {
		t.Errorf("parseRetryAfter(%q) = %v, %t; want %v (2076)", v, d, ok, want)
	}

	h := newHarness(t, failThenAccept(func() *http.Response {
		return problemResponse(429, "rate_limited", "r", "Retry-After", v)
	}))
	_, err := h.client.Events.Ingest(context.Background(), smallEvents(1))
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.RetryAfter != want {
		t.Fatalf("err = %v, want the 429 carrying the 2076 wait", err)
	}
	if h.transport.count() != 1 || len(h.recordedWaits()) != 0 {
		t.Errorf("%d attempts, waits %v; want 1 and none", h.transport.count(), h.recordedWaits())
	}

	// "77" would be 2077, more than 50 years ahead, so it is 1977: past,
	// and ignored.
	if _, ok := parseRetryAfter("Thursday, 28-Oct-77 12:00:05 GMT", fakeNow); ok {
		t.Errorf("a 1977 date was honoured")
	}
	// The other two forms are unchanged.
	for _, v := range []string{"Wed, 28 Oct 2026 12:00:07 GMT", "Wed Oct 28 12:00:07 2026"} {
		if d, ok := parseRetryAfter(v, fakeNow); !ok || d != 7*time.Second {
			t.Errorf("parseRetryAfter(%q) = %v, %t; want 7s", v, d, ok)
		}
	}
}
