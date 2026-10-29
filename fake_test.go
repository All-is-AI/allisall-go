package allisall

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// recorded is one request the fake transport received.
type recorded struct {
	method string
	url    string
	header http.Header
	body   []byte
}

// fakeTransport is an http.RoundTripper that records every request and
// answers with respond, given the request's zero-based number.
type fakeTransport struct {
	mu       sync.Mutex
	requests []recorded
	respond  func(n int, req *http.Request, rec recorded) (*http.Response, error)
}

func (f *fakeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var body []byte
	if req.Body != nil {
		b, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		body = b
	}
	rec := recorded{method: req.Method, url: req.URL.String(), header: req.Header.Clone(), body: body}
	f.mu.Lock()
	n := len(f.requests)
	f.requests = append(f.requests, rec)
	f.mu.Unlock()
	if f.respond == nil {
		return acceptAll(n, req, rec)
	}
	return f.respond(n, req, rec)
}

func (f *fakeTransport) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}

func (f *fakeTransport) get(n int) recorded {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests[n]
}

// harness is a client wired to a fake transport and a fake clock.
type harness struct {
	client    *Client
	transport *fakeTransport
	mu        sync.Mutex
	waits     []time.Duration
}

var fakeNow = time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)

func newHarness(t *testing.T, respond func(n int, req *http.Request, rec recorded) (*http.Response, error), opts ...Option) *harness {
	t.Helper()
	h := &harness{transport: &fakeTransport{respond: respond}}
	all := append([]Option{
		WithAPIKey("aia_testkey"),
		WithBaseURL("https://api.test"),
		WithHTTPClient(&http.Client{Transport: h.transport}),
	}, opts...)
	c, err := NewClient(all...)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	c.now = func() time.Time { return fakeNow }
	c.random = func() float64 { return 0 }
	c.sleep = func(ctx context.Context, d time.Duration) error {
		h.mu.Lock()
		h.waits = append(h.waits, d)
		h.mu.Unlock()
		return ctx.Err()
	}
	h.client = c
	return h
}

func (h *harness) recordedWaits() []time.Duration {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]time.Duration(nil), h.waits...)
}

func newResponse(status int, contentType, body string, header ...string) *http.Response {
	h := http.Header{}
	if contentType != "" {
		h.Set("Content-Type", contentType)
	}
	for i := 0; i+1 < len(header); i += 2 {
		h.Set(header[i], header[i+1])
	}
	return &http.Response{
		StatusCode: status,
		Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Header:     h,
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func problemResponse(status int, code, requestID string, header ...string) *http.Response {
	body := fmt.Sprintf(`{"type":"https://api.allisai.ai/errors/%s","title":"t","status":%d,"detail":"d","code":%q,"request_id":%q}`,
		code, status, code, requestID)
	return newResponse(status, "application/problem+json", body, append([]string{"X-Request-Id", requestID}, header...)...)
}

// sentEvents decodes an ingest body's events.
func sentEvents(t *testing.T, body []byte) []map[string]any {
	t.Helper()
	var b struct {
		Events []map[string]any `json:"events"`
	}
	if err := json.Unmarshal(body, &b); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	return b.Events
}

// countEvents counts an ingest body's events without decoding them.
func countEvents(body []byte) (int, error) {
	var b struct {
		Events []json.RawMessage `json:"events"`
	}
	if err := json.Unmarshal(body, &b); err != nil {
		return 0, err
	}
	return len(b.Events), nil
}

// acceptAll answers an ingest request by accepting every event.
func acceptAll(_ int, _ *http.Request, rec recorded) (*http.Response, error) {
	n, err := countEvents(rec.body)
	if err != nil {
		return nil, err
	}
	return newResponse(200, "application/json",
		fmt.Sprintf(`{"accepted":%d,"duplicates":0,"rejected":0,"errors":[],"errors_truncated":false}`, n)), nil
}

func smallEvent(i int) EventInput {
	return EventInput{
		Timestamp: fakeNow,
		AgentKey:  "agent-" + fmt.Sprint(i),
		Category:  EventCategoryTask,
		EventType: "ticket_resolved",
	}
}

func smallEvents(n int) []EventInput {
	out := make([]EventInput, n)
	for i := range out {
		out[i] = smallEvent(i)
	}
	return out
}

// sizedEvent returns an event whose JSON encoding is exactly size bytes,
// padding metadata.p, which starts with prefix.
func sizedEvent(t *testing.T, id string, size int, prefix string) EventInput {
	t.Helper()
	e := EventInput{
		EventID:   id,
		Timestamp: fakeNow,
		AgentKey:  "agent",
		Category:  EventCategoryTask,
		EventType: "padded",
		Metadata:  map[string]any{"p": prefix},
	}
	base := len(mustMarshal(t, e))
	if size < base {
		t.Fatalf("size %d is below the minimum %d", size, base)
	}
	e.Metadata = map[string]any{"p": prefix + strings.Repeat("x", size-base)}
	if got := len(mustMarshal(t, e)); got != size {
		t.Fatalf("sized event is %d bytes, want %d", got, size)
	}
	return e
}

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}
