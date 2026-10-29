package allisall

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"
)

var uuidV4 = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func id(i int) string { return fmt.Sprintf("id-%033d", i) } // 36 bytes, like a UUID

func TestIngestSplitsAt500Events(t *testing.T) {
	tests := []struct {
		events int
		want   []int
	}{
		{1, []int{1}},
		{499, []int{499}},
		{500, []int{500}},
		{501, []int{500, 1}},
		{1200, []int{500, 500, 200}},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprint(tt.events), func(t *testing.T) {
			h := newHarness(t, nil)
			summary, err := h.client.Events.Ingest(context.Background(), smallEvents(tt.events))
			if err != nil {
				t.Fatalf("Ingest: %v", err)
			}
			var got []int
			for n := 0; n < h.transport.count(); n++ {
				c, _ := countEvents(h.transport.get(n).body)
				got = append(got, c)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("batches = %v, want %v", got, tt.want)
			}
			if summary.Accepted != tt.events || summary.Batches != len(tt.want) {
				t.Errorf("summary = %+v, want %d accepted in %d batches", summary, tt.events, len(tt.want))
			}
		})
	}
}

// Five events whose single batch is exactly 5,000,000 bytes:
// 13 + 4×999,997 + 999,995 + 4 commas.
func exactLimitEvents(t *testing.T) []EventInput {
	t.Helper()
	sizes := []int{999_997, 999_997, 999_997, 999_997, 999_995}
	events := make([]EventInput, len(sizes))
	for i, s := range sizes {
		events[i] = sizedEvent(t, id(i), s, "")
	}
	return events
}

func TestIngestSplitsAtTheBodyLimit(t *testing.T) {
	t.Run("exactly 5,000,000 bytes is one batch", func(t *testing.T) {
		h := newHarness(t, nil)
		if _, err := h.client.Events.Ingest(context.Background(), exactLimitEvents(t)); err != nil {
			t.Fatalf("Ingest: %v", err)
		}
		if h.transport.count() != 1 {
			t.Fatalf("sent %d requests, want 1", h.transport.count())
		}
		if got := len(h.transport.get(0).body); got != 5_000_000 {
			t.Errorf("body is %d bytes, want 5,000,000", got)
		}
	})
	t.Run("one more byte splits before the event that crosses", func(t *testing.T) {
		events := exactLimitEvents(t)
		events[2] = sizedEvent(t, id(2), 999_998, "")
		h := newHarness(t, nil)
		if _, err := h.client.Events.Ingest(context.Background(), events); err != nil {
			t.Fatalf("Ingest: %v", err)
		}
		if h.transport.count() != 2 {
			t.Fatalf("sent %d requests, want 2", h.transport.count())
		}
		first, second := h.transport.get(0).body, h.transport.get(1).body
		if n, _ := countEvents(first); n != 4 {
			t.Errorf("first batch has %d events, want 4", n)
		}
		if want := 13 + 3*999_997 + 999_998 + 3; len(first) != want {
			t.Errorf("first body is %d bytes, want %d", len(first), want)
		}
		if want := 13 + 999_995; len(second) != want {
			t.Errorf("second body is %d bytes, want %d", len(second), want)
		}
	})
}

func TestIngestEventTooLargeToSend(t *testing.T) {
	t.Run("4,999,987 bytes is sent alone", func(t *testing.T) {
		h := newHarness(t, nil)
		summary, err := h.client.Events.Ingest(context.Background(), []EventInput{sizedEvent(t, id(0), 4_999_987, "")})
		if err != nil {
			t.Fatalf("Ingest: %v", err)
		}
		if h.transport.count() != 1 || len(h.transport.get(0).body) != 5_000_000 {
			t.Fatalf("want one body of 5,000,000 bytes")
		}
		if summary.Accepted != 1 || summary.Rejected != 0 {
			t.Errorf("summary = %+v", summary)
		}
	})
	t.Run("4,999,988 bytes is a too_large row and the rest are sent", func(t *testing.T) {
		events := []EventInput{smallEvent(0), sizedEvent(t, id(1), 4_999_988, ""), smallEvent(2)}
		// The batch holds the caller's events 0 and 2, with a gap at 1. The
		// fake rejects the batch's event 1, which is the caller's event 2:
		// a rewrite that added the batch's first index would leave it at 1.
		h := newHarness(t, func(int, *http.Request, recorded) (*http.Response, error) {
			return newResponse(200, "application/json", `{"accepted":1,"duplicates":0,"rejected":1,"errors_truncated":false,`+
				`"errors":[{"index":1,"code":"invalid_value","pointer":"/events/1/model","detail":"d"}]}`), nil
		})
		summary, err := h.client.Events.Ingest(context.Background(), events)
		if err != nil {
			t.Fatalf("Ingest: %v", err)
		}
		if h.transport.count() != 1 {
			t.Fatalf("sent %d requests, want 1", h.transport.count())
		}
		sent := sentEvents(t, h.transport.get(0).body)
		if len(sent) != 2 || sent[0]["agent_key"] != "agent-0" || sent[1]["agent_key"] != "agent-2" {
			t.Fatalf("sent %d events, want the two small ones", len(sent))
		}
		if summary.Accepted != 1 || summary.Rejected != 2 || len(summary.Errors) != 2 {
			t.Fatalf("summary = %+v", summary)
		}
		e := summary.Errors[0]
		if e.Index != 1 || e.Code != FieldErrorCodeTooLarge || e.Pointer != "/events/1" || !strings.Contains(e.Detail, "4999987") {
			t.Errorf("row error = %+v", e)
		}
		want := IngestRowError{Index: 2, Code: FieldErrorCodeInvalidValue, Pointer: "/events/2/model", Detail: "d"}
		if summary.Errors[1] != want {
			t.Errorf("row error after the gap = %+v, want %+v", summary.Errors[1], want)
		}
	})
}

func TestIngestMeasuresUTF8Bytes(t *testing.T) {
	// "é😀" is 2 characters and 6 bytes. The pair sits one byte over the
	// limit in bytes, and three under it in characters.
	const first = 2_500_000
	second := 5_000_001 - 13 - 1 - first
	events := []EventInput{
		sizedEvent(t, id(0), first, ""),
		sizedEvent(t, id(1), second, "é😀"),
	}
	h := newHarness(t, nil)
	if _, err := h.client.Events.Ingest(context.Background(), events); err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if h.transport.count() != 2 {
		t.Fatalf("sent %d requests, want 2: the pair is over the limit in bytes", h.transport.count())
	}
}

func TestIngestCombinesBatches(t *testing.T) {
	// 600 events: batches of 500 and 100, each with a rejected row at its
	// own index 3; the second batch's errors are truncated.
	respond := func(n int, _ *http.Request, rec recorded) (*http.Response, error) {
		count, _ := countEvents(rec.body)
		truncated := n == 1
		body := fmt.Sprintf(`{"accepted":%d,"duplicates":1,"rejected":1,"errors_truncated":%t,"new_member":{"x":1},`+
			`"errors":[{"index":3,"code":"invalid_format","pointer":"/events/3/timestamp","detail":"d"},`+
			`{"index":3,"code":"brand_new_code","pointer":"/events/3","detail":"d"}]}`, count-2, truncated)
		return newResponse(200, "application/json", body), nil
	}
	h := newHarness(t, respond)
	summary, err := h.client.Events.Ingest(context.Background(), smallEvents(600))
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	want := &IngestSummary{
		Accepted:   596,
		Duplicates: 2,
		Rejected:   2,
		Errors: []IngestRowError{
			{Index: 3, Code: FieldErrorCodeInvalidFormat, Pointer: "/events/3/timestamp", Detail: "d"},
			{Index: 3, Code: "brand_new_code", Pointer: "/events/3", Detail: "d"},
			{Index: 503, Code: FieldErrorCodeInvalidFormat, Pointer: "/events/503/timestamp", Detail: "d"},
			{Index: 503, Code: "brand_new_code", Pointer: "/events/503", Detail: "d"},
		},
		ErrorsTruncated: true,
		Batches:         2,
	}
	if !reflect.DeepEqual(summary, want) {
		t.Errorf("summary =\n%+v\nwant\n%+v", summary, want)
	}
}

func TestIngestEmptyListSendsNothing(t *testing.T) {
	h := newHarness(t, nil)
	summary, err := h.client.Events.Ingest(context.Background(), nil)
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if h.transport.count() != 0 {
		t.Errorf("sent %d requests, want 0", h.transport.count())
	}
	if !reflect.DeepEqual(summary, &IngestSummary{Errors: []IngestRowError{}}) {
		t.Errorf("summary = %+v, want zeros", summary)
	}
}

func TestIngestAssignsEventIDs(t *testing.T) {
	events := []EventInput{smallEvent(0), smallEvent(0), smallEvent(2)}
	events[2].EventID = "caller-id"
	events[2].Metadata = map[string]any{"k": "v"}
	before := make([]EventInput, len(events))
	copy(before, events)
	before[2].Metadata = map[string]any{"k": "v"}

	h := newHarness(t, nil)
	if _, err := h.client.Events.Ingest(context.Background(), events); err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	sent := sentEvents(t, h.transport.get(0).body)
	for i := 0; i < 2; i++ {
		got, _ := sent[i]["event_id"].(string)
		if !uuidV4.MatchString(got) {
			t.Errorf("event %d: event_id %q is not a lowercase UUID version 4", i, got)
		}
	}
	if sent[0]["event_id"] == sent[1]["event_id"] {
		t.Errorf("two identical events got the same event_id")
	}
	if sent[2]["event_id"] != "caller-id" {
		t.Errorf("caller's event_id sent as %v", sent[2]["event_id"])
	}
	if !reflect.DeepEqual(events, before) {
		t.Errorf("the caller's events changed:\n%+v\nwas\n%+v", events, before)
	}
}

func TestIngestRetrySendsIdenticalBytes(t *testing.T) {
	h := newHarness(t, func(n int, req *http.Request, rec recorded) (*http.Response, error) {
		if n == 0 {
			return problemResponse(503, "internal_error", "r1"), nil
		}
		return acceptAll(n, req, rec)
	})
	if _, err := h.client.Events.Ingest(context.Background(), smallEvents(3)); err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if h.transport.count() != 2 {
		t.Fatalf("sent %d requests, want 2", h.transport.count())
	}
	if !bytes.Equal(h.transport.get(0).body, h.transport.get(1).body) {
		t.Errorf("the retry sent different bytes")
	}
}

func TestIngestStopsAtAFailedBatch(t *testing.T) {
	h := newHarness(t, func(n int, req *http.Request, rec recorded) (*http.Response, error) {
		if n == 1 {
			return problemResponse(401, "unauthenticated", "req-2"), nil
		}
		return acceptAll(n, req, rec)
	})
	events := smallEvents(1200)
	_, err := h.client.Events.Ingest(context.Background(), events)

	var ingestErr *IngestError
	if !errors.As(err, &ingestErr) {
		t.Fatalf("err = %v, want *IngestError", err)
	}
	if h.transport.count() != 2 {
		t.Errorf("sent %d requests, want 2: the third batch must not be sent", h.transport.count())
	}
	if ingestErr.Summary.Accepted != 500 || ingestErr.Summary.Batches != 1 {
		t.Errorf("summary = %+v, want the first batch's", ingestErr.Summary)
	}
	if ingestErr.RemainingFrom != 500 || len(ingestErr.Remaining) != 700 {
		t.Errorf("RemainingFrom = %d, len(Remaining) = %d, want 500 and 700", ingestErr.RemainingFrom, len(ingestErr.Remaining))
	}
	sent := sentEvents(t, h.transport.get(1).body)
	for i, e := range sent {
		if ingestErr.Remaining[i].EventID != e["event_id"] {
			t.Fatalf("Remaining[%d].EventID = %q, sent %v", i, ingestErr.Remaining[i].EventID, e["event_id"])
		}
	}
	for i, e := range ingestErr.Remaining[500:] {
		if !uuidV4.MatchString(e.EventID) || e.AgentKey != events[1000+i].AgentKey {
			t.Fatalf("Remaining[%d] = %+v, want event %d with its id", 500+i, e, 1000+i)
		}
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code != ErrorCodeUnauthenticated {
		t.Errorf("cause = %v, want the 401 APIError", err)
	}
	if !errors.Is(err, ErrAllisall) {
		t.Errorf("errors.Is(err, ErrAllisall) = false")
	}
}

func TestIngestTooLargeEventIsNeverInRemaining(t *testing.T) {
	h := newHarness(t, func(int, *http.Request, recorded) (*http.Response, error) {
		return problemResponse(403, "insufficient_scope", "r"), nil
	})
	events := []EventInput{smallEvent(0), sizedEvent(t, id(1), 4_999_988, ""), smallEvent(2)}
	_, err := h.client.Events.Ingest(context.Background(), events)
	var ingestErr *IngestError
	if !errors.As(err, &ingestErr) {
		t.Fatalf("err = %v, want *IngestError", err)
	}
	if ingestErr.Summary.Rejected != 1 || len(ingestErr.Summary.Errors) != 1 || ingestErr.Summary.Errors[0].Index != 1 {
		t.Errorf("summary = %+v, want the too_large row", ingestErr.Summary)
	}
	if len(ingestErr.Remaining) != 2 || ingestErr.Remaining[1].AgentKey != "agent-2" || ingestErr.RemainingFrom != 0 {
		t.Errorf("Remaining = %d events from %d, want events 0 and 2", len(ingestErr.Remaining), ingestErr.RemainingFrom)
	}
}

func TestIngestCancelledDuringAWait(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	h := newHarness(t, func(int, *http.Request, recorded) (*http.Response, error) {
		return problemResponse(503, "internal_error", "r"), nil
	})
	h.client.sleep = func(ctx context.Context, _ time.Duration) error {
		cancel()
		return ctx.Err()
	}
	_, err := h.client.Events.Ingest(ctx, smallEvents(1))
	var ingestErr *IngestError
	if !errors.As(err, &ingestErr) || !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want an IngestError caused by context.Canceled", err)
	}
	if h.transport.count() != 1 {
		t.Errorf("sent %d requests, want 1", h.transport.count())
	}
}

func TestIngestTokenProviderErrorFailsTheBatch(t *testing.T) {
	// Two batches, 500 and 100. The provider answers for the first batch
	// and fails before the second.
	boom := errors.New("secret store unavailable")
	calls := 0
	h := newHarness(t, nil)
	h.client.apiKey = ""
	h.client.tokenProvider = func(context.Context) (string, error) {
		calls++
		if calls == 2 {
			return "", boom
		}
		return "aia_fromprovider", nil
	}
	_, err := h.client.Events.Ingest(context.Background(), smallEvents(600))

	var ingestErr *IngestError
	if !errors.As(err, &ingestErr) {
		t.Fatalf("err = %v, want an *IngestError", err)
	}
	if !errors.Is(err, boom) || ingestErr.Err != boom {
		t.Errorf("cause = %v, want the provider's error unchanged", ingestErr.Err)
	}
	if !errors.Is(err, ErrAllisall) {
		t.Errorf("errors.Is(err, ErrAllisall) = false")
	}
	if calls != 2 || h.transport.count() != 1 {
		t.Errorf("provider called %d times and %d requests sent, want 2 and 1: the error is not retried", calls, h.transport.count())
	}
	if s := ingestErr.Summary; s.Accepted != 500 || s.Batches != 1 || s.Rejected != 0 {
		t.Errorf("summary = %+v, want the first batch's", s)
	}
	if ingestErr.RemainingFrom != 500 || len(ingestErr.Remaining) != 100 {
		t.Fatalf("RemainingFrom = %d, len(Remaining) = %d, want 500 and 100", ingestErr.RemainingFrom, len(ingestErr.Remaining))
	}
	firstBatch := map[any]bool{}
	for _, e := range sentEvents(t, h.transport.get(0).body) {
		firstBatch[e["event_id"]] = true
	}
	for i, e := range ingestErr.Remaining {
		if !uuidV4.MatchString(e.EventID) || firstBatch[e.EventID] {
			t.Fatalf("Remaining[%d].EventID = %q, want a generated id not sent in the first batch", i, e.EventID)
		}
	}

	// Sending Remaining again sends the same ids: they were generated
	// once, and are kept.
	h.client.tokenProvider = func(context.Context) (string, error) { return "aia_fromprovider", nil }
	if _, err := h.client.Events.Ingest(context.Background(), ingestErr.Remaining); err != nil {
		t.Fatalf("resend: %v", err)
	}
	resent := sentEvents(t, h.transport.get(1).body)
	for i, e := range resent {
		if e["event_id"] != ingestErr.Remaining[i].EventID {
			t.Fatalf("resent event %d has id %v, want %q", i, e["event_id"], ingestErr.Remaining[i].EventID)
		}
	}
}

func TestIngestUnexpectedSuccessBody(t *testing.T) {
	h := newHarness(t, func(int, *http.Request, recorded) (*http.Response, error) {
		return newResponse(200, "application/json", `{"accepted":1}`, "X-Request-Id", "r-1"), nil
	})
	_, err := h.client.Events.Ingest(context.Background(), smallEvents(1))
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != 200 || apiErr.Problem != nil || apiErr.RequestID != "r-1" {
		t.Fatalf("err = %v, want an APIError with status 200 and no problem", err)
	}
	if h.transport.count() != 1 {
		t.Errorf("sent %d requests, want 1: a bad 2xx body is not retried", h.transport.count())
	}
}

func TestIngestEncoding(t *testing.T) {
	t.Run("cost, tokens and timestamp", func(t *testing.T) {
		e := smallEvent(0)
		e.Cost = new(MustParseDecimal("100.50"))
		e.TokensIn = new(int64(0))
		e.Timestamp = time.Date(2026, 8, 1, 9, 30, 0, 123, time.FixedZone("JST", 9*3600))
		h := newHarness(t, nil)
		if _, err := h.client.Events.Ingest(context.Background(), []EventInput{e}); err != nil {
			t.Fatalf("Ingest: %v", err)
		}
		body := string(h.transport.get(0).body)
		for _, want := range []string{`"cost":100.50`, `"tokens_in":0`, `"timestamp":"2026-08-01T09:30:00.000000123+09:00"`} {
			if !strings.Contains(body, want) {
				t.Errorf("body %s lacks %s", body, want)
			}
		}
		for _, absent := range []string{`"client_key"`, `"quantity"`, `"currency"`, `"tokens_out"`, `"metadata"`} {
			if strings.Contains(body, absent) {
				t.Errorf("body %s holds %s, which was not set", body, absent)
			}
		}
	})

	refused := []struct {
		name  string
		event func() EventInput
	}{
		{"quantity NaN", func() EventInput { e := smallEvent(0); e.Quantity = math.NaN(); return e }},
		{"quantity +Inf", func() EventInput { e := smallEvent(0); e.Quantity = math.Inf(1); return e }},
		{"metadata NaN", func() EventInput { e := smallEvent(0); e.Metadata = map[string]any{"x": math.NaN()}; return e }},
		{"agent_key invalid UTF-8", func() EventInput { e := smallEvent(0); e.AgentKey = "a\xffb"; return e }},
		{"nested metadata invalid UTF-8", func() EventInput {
			e := smallEvent(0)
			e.Metadata = map[string]any{"list": []any{"ok", map[string]any{"deep": "\xed\xa0\x80"}}}
			return e
		}},
		{"metadata key invalid UTF-8", func() EventInput { e := smallEvent(0); e.Metadata = map[string]any{"\xff": 1}; return e }},
	}
	for _, tt := range refused {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t, nil)
			events := append(smallEvents(2), tt.event())
			_, err := h.client.Events.Ingest(context.Background(), events)
			if err == nil {
				t.Fatal("Ingest succeeded, want an encoding error")
			}
			if !strings.Contains(err.Error(), "event 2") {
				t.Errorf("err = %v, want it to name event 2", err)
			}
			if h.transport.count() != 0 {
				t.Errorf("sent %d requests, want 0", h.transport.count())
			}
		})
	}
}
