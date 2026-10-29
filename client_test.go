package allisall

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestCredentialResolution(t *testing.T) {
	provider := func(context.Context) (string, error) { return "aia_fromprovider", nil }
	tests := []struct {
		name    string
		env     string
		opts    []Option
		wantKey string
		wantErr bool
	}{
		{"the option wins over AIA_API_KEY", "aia_fromenv", []Option{WithAPIKey("aia_fromoption")}, "aia_fromoption", false},
		{"AIA_API_KEY when no option", "aia_fromenv", nil, "aia_fromenv", false},
		{"a provider wins over AIA_API_KEY", "aia_fromenv", []Option{WithTokenProvider(provider)}, "", false},
		{"both a key and a provider", "", []Option{WithAPIKey("k"), WithTokenProvider(provider)}, "", true},
		{"neither, and AIA_API_KEY empty", "", nil, "", true},
		{"an empty key", "aia_fromenv", []Option{WithAPIKey("")}, "", true},
		{"a nil provider", "aia_fromenv", []Option{WithTokenProvider(nil)}, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(envAPIKey, tt.env)
			c, err := NewClient(tt.opts...)
			if tt.wantErr {
				if err == nil || c != nil {
					t.Fatalf("NewClient = %v, %v; want nil and an error", c, err)
				}
				if !errors.Is(err, ErrAllisall) {
					t.Errorf("errors.Is(err, ErrAllisall) = false for %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("NewClient: %v", err)
			}
			if c.apiKey != tt.wantKey {
				t.Errorf("apiKey = %q, want %q", c.apiKey, tt.wantKey)
			}
			if (c.tokenProvider != nil) != (tt.wantKey == "") {
				t.Errorf("tokenProvider set = %t", c.tokenProvider != nil)
			}
		})
	}
}

func TestBaseURLResolution(t *testing.T) {
	tests := []struct {
		name string
		env  string
		opts []Option
		want string
	}{
		{"the default", "", nil, "https://api.allisai.ai/v1/events"},
		{"AIA_BASE_URL", "http://localhost:8081", nil, "http://localhost:8081/v1/events"},
		{"the option wins over AIA_BASE_URL", "http://localhost:8081", []Option{WithBaseURL("https://example.test")}, "https://example.test/v1/events"},
		{"a path is kept", "", []Option{WithBaseURL("https://example.test/api")}, "https://example.test/api/v1/events"},
		{"a trailing slash gives one slash", "", []Option{WithBaseURL("https://example.test/api/")}, "https://example.test/api/v1/events"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(envBaseURL, tt.env)
			ft := &fakeTransport{}
			c, err := NewClient(append([]Option{WithAPIKey("aia_k"), WithHTTPClient(&http.Client{Transport: ft})}, tt.opts...)...)
			if err != nil {
				t.Fatalf("NewClient: %v", err)
			}
			if _, err := c.Events.Ingest(context.Background(), smallEvents(1)); err != nil {
				t.Fatalf("Ingest: %v", err)
			}
			if got := ft.get(0).url; got != tt.want {
				t.Errorf("URL = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestConstructionErrors(t *testing.T) {
	t.Setenv(envBaseURL, "")
	tests := []struct {
		name string
		opt  Option
	}{
		{"a relative base URL", WithBaseURL("/api")},
		{"a base URL without a scheme", WithBaseURL("api.allisai.ai")},
		{"a base URL of another scheme", WithBaseURL("ftp://example.test")},
		{"a base URL without a host", WithBaseURL("https://")},
		{"an empty base URL", WithBaseURL("")},
		{"a negative retry count", WithMaxRetries(-1)},
		{"a zero timeout", WithTimeout(0)},
		{"a negative timeout", WithTimeout(-time.Second)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := NewClient(WithAPIKey("aia_k"), tt.opt)
			if err == nil || c != nil {
				t.Fatalf("NewClient = %v, %v; want nil and an error", c, err)
			}
			if !errors.Is(err, ErrAllisall) {
				t.Errorf("errors.Is(err, ErrAllisall) = false for %v", err)
			}
		})
	}

	t.Run("an invalid AIA_BASE_URL", func(t *testing.T) {
		t.Setenv(envBaseURL, "localhost:8081")
		if _, err := NewClient(WithAPIKey("aia_k")); !errors.Is(err, ErrAllisall) {
			t.Errorf("err = %v, want an ErrAllisall error", err)
		}
	})
}

func TestDefaults(t *testing.T) {
	t.Setenv(envBaseURL, "")
	c, err := NewClient(WithAPIKey("aia_k"))
	if err != nil {
		t.Fatal(err)
	}
	if c.timeout != 60*time.Second || c.maxRetries != 2 {
		t.Errorf("timeout %v, retries %d; want 60s and 2", c.timeout, c.maxRetries)
	}
	if c.httpClient == nil || c.httpClient == http.DefaultClient {
		t.Errorf("the default client must be the SDK's own, not http.DefaultClient")
	}
}

func TestTheCallersHTTPClientIsUsedUnchanged(t *testing.T) {
	ft := &fakeTransport{}
	hc := &http.Client{Transport: ft, Timeout: 5 * time.Second}
	c, err := NewClient(WithAPIKey("aia_k"), WithHTTPClient(hc))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Events.Ingest(context.Background(), smallEvents(1)); err != nil {
		t.Fatal(err)
	}
	if ft.count() != 1 {
		t.Errorf("the caller's client sent %d requests, want 1", ft.count())
	}
	if hc.Transport != ft || hc.Timeout != 5*time.Second || hc.CheckRedirect != nil || hc.Jar != nil {
		t.Errorf("the caller's http.Client was modified")
	}
}

func TestRequestHeaders(t *testing.T) {
	h := newHarness(t, nil)
	if _, err := h.client.Events.Ingest(context.Background(), smallEvents(1)); err != nil {
		t.Fatal(err)
	}
	rec := h.transport.get(0)
	if rec.method != http.MethodPost {
		t.Errorf("method = %s, want POST", rec.method)
	}
	want := map[string]string{
		"Authorization": "Bearer aia_testkey",
		"Content-Type":  "application/json",
		"User-Agent":    "allisall-go/" + version + " go/" + strings.TrimPrefix(runtime.Version(), "go"),
	}
	for k, v := range want {
		if got := rec.header.Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
	if !regexp.MustCompile(`^allisall-go/0\.1\.0 go/\d`).MatchString(rec.header.Get("User-Agent")) {
		t.Errorf("User-Agent = %q", rec.header.Get("User-Agent"))
	}
	if !uuidV4.MatchString(rec.header.Get("X-Request-Id")) {
		t.Errorf("X-Request-Id = %q, want a UUID version 4", rec.header.Get("X-Request-Id"))
	}
}
