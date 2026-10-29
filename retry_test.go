package allisall

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"syscall"
	"testing"
	"time"
)

func TestRetriedOutcomes(t *testing.T) {
	tests := []struct {
		name     string
		respond  func() (*http.Response, error)
		attempts int
	}{
		{"408", func() (*http.Response, error) { return newResponse(408, "text/plain", "timeout"), nil }, 3},
		{"429", func() (*http.Response, error) { return problemResponse(429, "rate_limited", "r"), nil }, 3},
		{"500", func() (*http.Response, error) { return problemResponse(500, "internal_error", "r"), nil }, 3},
		{"502", func() (*http.Response, error) { return newResponse(502, "text/html", "<html>"), nil }, 3},
		{"503", func() (*http.Response, error) { return newResponse(503, "text/html", "<html>"), nil }, 3},
		{"504", func() (*http.Response, error) { return newResponse(504, "text/html", "<html>"), nil }, 3},
		{"connection reset", func() (*http.Response, error) { return nil, syscall.ECONNRESET }, 3},
		{"400", func() (*http.Response, error) { return problemResponse(400, "invalid_json", "r"), nil }, 1},
		{"401", func() (*http.Response, error) { return problemResponse(401, "unauthenticated", "r"), nil }, 1},
		{"403", func() (*http.Response, error) { return problemResponse(403, "insufficient_scope", "r"), nil }, 1},
		{"413", func() (*http.Response, error) { return problemResponse(413, "payload_too_large", "r"), nil }, 1},
		{"415", func() (*http.Response, error) { return problemResponse(415, "unsupported_media_type", "r"), nil }, 1},
		{"501", func() (*http.Response, error) { return newResponse(501, "text/plain", "no"), nil }, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t, func(int, *http.Request, recorded) (*http.Response, error) { return tt.respond() })
			_, err := h.client.Events.Ingest(context.Background(), smallEvents(1))
			if err == nil {
				t.Fatal("Ingest succeeded, want an error")
			}
			if got := h.transport.count(); got != tt.attempts {
				t.Errorf("sent %d attempts, want %d", got, tt.attempts)
			}
			if got := len(h.recordedWaits()); got != tt.attempts-1 {
				t.Errorf("waited %d times, want %d", got, tt.attempts-1)
			}
		})
	}
}

func TestConnectionErrorCarriesTheLastRequestID(t *testing.T) {
	h := newHarness(t, func(int, *http.Request, recorded) (*http.Response, error) { return nil, syscall.ECONNRESET })
	_, err := h.client.Events.Ingest(context.Background(), smallEvents(1))
	var connErr *ConnectionError
	if !errors.As(err, &connErr) {
		t.Fatalf("err = %v, want a ConnectionError", err)
	}
	if want := h.transport.get(2).header.Get("X-Request-Id"); connErr.RequestID != want {
		t.Errorf("RequestID = %q, want the last attempt's %q", connErr.RequestID, want)
	}
	if !errors.Is(err, syscall.ECONNRESET) || !errors.Is(err, ErrAllisall) {
		t.Errorf("err = %v, want it to match ECONNRESET and ErrAllisall", err)
	}
}

// failThenAccept answers the first attempt with first, and accepts after.
func failThenAccept(first func() *http.Response) func(int, *http.Request, recorded) (*http.Response, error) {
	return func(n int, req *http.Request, rec recorded) (*http.Response, error) {
		if n == 0 {
			return first(), nil
		}
		return acceptAll(n, req, rec)
	}
}

func TestRetryAfter(t *testing.T) {
	tests := []struct {
		name       string
		retryAfter string
		attempts   int
		waits      []time.Duration
	}{
		{"seconds", "2", 2, []time.Duration{2 * time.Second}},
		{"60 seconds is honoured", "60", 2, []time.Duration{60 * time.Second}},
		{"HTTP-date", fakeNow.Add(3 * time.Second).Format(http.TimeFormat), 2, []time.Duration{3 * time.Second}},
		{"61 seconds is not retried", "61", 1, nil},
		{"HTTP-date over 60 seconds is not retried", fakeNow.Add(2 * time.Minute).Format(http.TimeFormat), 1, nil},
		// With the fake jitter of 0, the backoff of retry 1 is 0.5 s.
		{"unparsable falls back on the backoff", "soon", 2, []time.Duration{500 * time.Millisecond}},
		{"a date in the past falls back on the backoff", fakeNow.Add(-time.Hour).Format(http.TimeFormat), 2, []time.Duration{500 * time.Millisecond}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t, failThenAccept(func() *http.Response {
				return problemResponse(429, "rate_limited", "r", "Retry-After", tt.retryAfter)
			}))
			_, err := h.client.Events.Ingest(context.Background(), smallEvents(1))
			if got := h.transport.count(); got != tt.attempts {
				t.Fatalf("sent %d attempts, want %d", got, tt.attempts)
			}
			if got := h.recordedWaits(); !reflect.DeepEqual(got, tt.waits) {
				t.Errorf("waits = %v, want %v", got, tt.waits)
			}
			if tt.attempts == 1 {
				var apiErr *APIError
				if !errors.As(err, &apiErr) || apiErr.RetryAfter <= maxRetryAfter {
					t.Errorf("err = %v, want an APIError carrying the Retry-After", err)
				}
			}
		})
	}
}

func TestBackoffWithoutRetryAfter(t *testing.T) {
	// The jitter factor is 1 - r×0.25 for r in [0, 1).
	tests := []struct {
		r            float64
		retry1, last time.Duration
	}{
		{0, 500 * time.Millisecond, time.Second},
		{0.5, 437500 * time.Microsecond, 875 * time.Millisecond},
		{1, 375 * time.Millisecond, 750 * time.Millisecond}, // the bound r never reaches
	}
	for _, tt := range tests {
		t.Run(fmt.Sprint(tt.r), func(t *testing.T) {
			h := newHarness(t, func(int, *http.Request, recorded) (*http.Response, error) {
				return newResponse(503, "text/plain", "busy"), nil
			})
			h.client.random = func() float64 { return tt.r }
			_, _ = h.client.Events.Ingest(context.Background(), smallEvents(1))
			if got, want := h.recordedWaits(), []time.Duration{tt.retry1, tt.last}; !reflect.DeepEqual(got, want) {
				t.Errorf("waits = %v, want %v", got, want)
			}
		})
	}

	t.Run("with the real jitter source", func(t *testing.T) {
		c, err := NewClient(WithAPIKey("k"))
		if err != nil {
			t.Fatal(err)
		}
		for range 1000 {
			if d := c.backoff(1); d < 375*time.Millisecond || d > 500*time.Millisecond {
				t.Fatalf("retry 1 waits %v, want 0.375 s to 0.5 s", d)
			}
			if d := c.backoff(2); d < 750*time.Millisecond || d > time.Second {
				t.Fatalf("retry 2 waits %v, want 0.75 s to 1 s", d)
			}
		}
	})

	t.Run("capped at 8 seconds", func(t *testing.T) {
		h := newHarness(t, nil)
		for n, want := range map[int]time.Duration{4: 4 * time.Second, 5: 8 * time.Second, 6: 8 * time.Second, 70: 8 * time.Second} {
			if got := h.client.backoff(n); got != want {
				t.Errorf("backoff(%d) = %v, want %v", n, got, want)
			}
		}
	})
}

func TestMaxRetries(t *testing.T) {
	for _, tt := range []struct{ retries, attempts int }{{0, 1}, {1, 2}, {4, 5}} {
		t.Run(fmt.Sprint(tt.retries), func(t *testing.T) {
			h := newHarness(t, func(int, *http.Request, recorded) (*http.Response, error) {
				return newResponse(503, "text/plain", "busy"), nil
			}, WithMaxRetries(tt.retries))
			_, _ = h.client.Events.Ingest(context.Background(), smallEvents(1))
			if got := h.transport.count(); got != tt.attempts {
				t.Errorf("sent %d attempts, want %d", got, tt.attempts)
			}
		})
	}
}

func TestEveryAttemptGetsAFreshTokenAndRequestID(t *testing.T) {
	calls := 0
	provider := func(context.Context) (string, error) {
		calls++
		return fmt.Sprintf("aia_token%d", calls), nil
	}
	h := newHarness(t, func(n int, req *http.Request, rec recorded) (*http.Response, error) {
		if n < 2 {
			return newResponse(503, "text/plain", "busy"), nil
		}
		return acceptAll(n, req, rec)
	})
	h.client.apiKey = ""
	h.client.tokenProvider = provider
	if _, err := h.client.Events.Ingest(context.Background(), smallEvents(1)); err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if calls != 3 {
		t.Errorf("provider called %d times, want 3", calls)
	}
	seen := map[string]bool{}
	for n := 0; n < 3; n++ {
		rec := h.transport.get(n)
		if got, want := rec.header.Get("Authorization"), fmt.Sprintf("Bearer aia_token%d", n+1); got != want {
			t.Errorf("attempt %d: Authorization = %q, want %q", n, got, want)
		}
		rid := rec.header.Get("X-Request-Id")
		if !uuidV4.MatchString(rid) || seen[rid] {
			t.Errorf("attempt %d: X-Request-Id %q is not a fresh UUID version 4", n, rid)
		}
		seen[rid] = true
	}
}

func TestPerAttemptTimeout(t *testing.T) {
	h := newHarness(t, func(_ int, req *http.Request, _ recorded) (*http.Response, error) {
		<-req.Context().Done()
		return nil, req.Context().Err()
	}, WithTimeout(20*time.Millisecond), WithMaxRetries(1))
	_, err := h.client.Events.Ingest(context.Background(), smallEvents(1))
	var connErr *ConnectionError
	if !errors.As(err, &connErr) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want a ConnectionError from the attempt's deadline", err)
	}
	if h.transport.count() != 2 {
		t.Errorf("sent %d attempts, want 2: a timed-out attempt is retried", h.transport.count())
	}
}

func TestCallerCancellationIsNotRetried(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	h := newHarness(t, func(_ int, req *http.Request, _ recorded) (*http.Response, error) {
		cancel()
		<-req.Context().Done()
		return nil, req.Context().Err()
	})
	_, err := h.client.Events.Ingest(ctx, smallEvents(1))
	var connErr *ConnectionError
	if !errors.Is(err, context.Canceled) || errors.As(err, &connErr) {
		t.Fatalf("err = %v, want the cancellation, not a ConnectionError", err)
	}
	if h.transport.count() != 1 {
		t.Errorf("sent %d attempts, want 1", h.transport.count())
	}
	if waits := h.recordedWaits(); len(waits) != 0 {
		t.Errorf("waited %v, want no wait: a cancellation is not retried", waits)
	}
}

func TestSleepContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	if err := sleepContext(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
	if time.Since(start) > time.Second {
		t.Errorf("a cancelled wait did not return at once")
	}
	if err := sleepContext(context.Background(), time.Millisecond); err != nil {
		t.Errorf("err = %v, want nil", err)
	}
}

func TestParseRetryAfter(t *testing.T) {
	tests := []struct {
		in   string
		want time.Duration
		ok   bool
	}{
		{"0", 0, true},
		{"1", time.Second, true},
		{" 7 ", 7 * time.Second, true},
		{"99999999999999999999", time.Duration(1<<63 - 1), true},
		{fakeNow.Add(10 * time.Second).Format(http.TimeFormat), 10 * time.Second, true},
		{fakeNow.Add(-time.Second).Format(http.TimeFormat), 0, false},
		{"-1", 0, false},
		{"1.5", 0, false},
		{"", 0, false},
	}
	for _, tt := range tests {
		got, ok := parseRetryAfter(tt.in, fakeNow)
		if got != tt.want || ok != tt.ok {
			t.Errorf("parseRetryAfter(%q) = %v, %t; want %v, %t", tt.in, got, ok, tt.want, tt.ok)
		}
	}
}
