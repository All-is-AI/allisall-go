package allisall

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"

	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// These tests hold the request layer every operation goes through: the
// URL, query, headers and body it sends, its retries, its options, and how
// it reads the answer. Ingest goes through it too, and 0.1.0's ingest
// tests hold that its default behaviour did not change.

func mustOp(t *testing.T, id string) *operation {
	t.Helper()
	op, err := operationFor(id)
	if err != nil {
		t.Fatal(err)
	}
	return op
}

// pathArgs returns one id per path parameter of op.
func pathArgs(op *operation) []string {
	var out []string
	for range strings.Count(op.path, "{") {
		out = append(out, "id-1")
	}
	return out
}

// anyCall builds a call of op with arguments of the right kind.
func anyCall(op *operation) call {
	cl := call{op: op, path: pathArgs(op)}
	switch op.body {
	case bodyJSON, bodyMergePatch:
		if op.id == opIngestEvents {
			cl.raw = []byte(`{"events":[]}`)
		} else {
			cl.body = struct{}{}
		}
	case bodyMultipart:
		cl.upload = &ImportUpload{File: strings.NewReader("a,b\n1,2\n")}
	}
	return cl
}

func ok200(_ int, _ *http.Request, _ recorded) (*http.Response, error) {
	return newResponse(200, "application/json", `{}`), nil
}

// isPlain reports whether err is a caller's mistake: an error that does not
// match ErrAllisall.
func isPlain(err error) bool { return err != nil && !errors.Is(err, ErrAllisall) }

func TestEachBodyKindSendsItsContentType(t *testing.T) {
	notes := "first"
	tests := []struct {
		name        string
		id          string
		path        []string
		body        any
		upload      *ImportUpload
		method      string
		url         string
		contentType string // "" for none
		wantBody    string // checked when not empty
	}{
		{
			name: "JSON", id: "createClient",
			body:   ClientCreate{Name: "Acme", Status: ClientStatusActive, Notes: &notes},
			method: "POST", url: "https://api.test/v1/clients",
			contentType: "application/json",
			wantBody:    `{"name":"Acme","notes":"first","status":"active"}`,
		},
		{
			name: "merge patch", id: "updateClient", path: []string{"c1"},
			body:   ClientPatch{Name: new("Acme 2"), Notes: Null[string](), ClientKey: NewNullable("acme")},
			method: "PATCH", url: "https://api.test/v1/clients/c1",
			contentType: "application/merge-patch+json",
			// renewal_date and status are absent; notes is null.
			wantBody: `{"client_key":"acme","name":"Acme 2","notes":null}`,
		},
		{
			name: "no body, DELETE", id: "deleteClient", path: []string{"c1"},
			method: "DELETE", url: "https://api.test/v1/clients/c1",
		},
		{
			name: "no body, POST", id: "createReport", path: []string{"calc1"},
			method: "POST", url: "https://api.test/v1/calculations/calc1/reports",
		},
		{
			name: "multipart", id: "commitImport",
			upload: &ImportUpload{File: strings.NewReader("a\n")},
			method: "POST", url: "https://api.test/v1/imports",
			contentType: "multipart/form-data; boundary=",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t, func(int, *http.Request, recorded) (*http.Response, error) {
				return newResponse(200, "application/json", `{}`), nil
			})
			op := mustOp(t, tt.id)
			var out map[string]any
			if err := h.client.do(context.Background(), call{op: op, path: tt.path, body: tt.body, upload: tt.upload}, nil, &out); err != nil {
				t.Fatal(err)
			}
			got := h.transport.get(0)
			if got.method != tt.method || got.url != tt.url {
				t.Errorf("sent %s %s, want %s %s", got.method, got.url, tt.method, tt.url)
			}
			ct, has := got.header["Content-Type"]
			switch {
			case tt.contentType == "" && has:
				t.Errorf("Content-Type = %q, want none on a request with no body", ct)
			case tt.contentType == "" && len(got.body) != 0:
				t.Errorf("body = %q, want none", got.body)
			case tt.contentType != "" && (len(ct) != 1 || !strings.HasPrefix(ct[0], tt.contentType)):
				t.Errorf("Content-Type = %q, want %q", ct, tt.contentType)
			}
			if tt.wantBody != "" && string(got.body) != tt.wantBody {
				t.Errorf("body = %s, want %s", got.body, tt.wantBody)
			}
		})
	}
}

func TestAPathParameterIsOneEscapedSegment(t *testing.T) {
	var gotURI string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURI = r.RequestURI
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	c, err := NewClient(WithAPIKey("aia_testkey"), WithBaseURL(srv.URL+"/api/"))
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := c.do(context.Background(), call{op: mustOp(t, "getClient"), path: []string{"a/b%c?d#e"}}, nil, &out); err != nil {
		t.Fatal(err)
	}
	// On the wire: the base's path once, then the id as one segment.
	if want := "/api/v1/clients/a%2Fb%25c%3Fd%23e"; gotURI != want {
		t.Errorf("request URI = %q, want %q", gotURI, want)
	}
}

func TestAPathParameterThatIsNotASegmentIsRefused(t *testing.T) {
	for _, id := range []string{"", ".", ".."} {
		h := newHarness(t, ok200)
		var out map[string]any
		err := h.client.do(context.Background(), call{op: mustOp(t, "deleteClient"), path: []string{id}}, nil, &out)
		if !isPlain(err) {
			t.Errorf("id %q: err = %v, want a plain error", id, err)
		}
		if n := h.transport.count(); n != 0 {
			t.Errorf("id %q: %d requests sent, want none", id, n)
		}
	}
}

func TestTheQueryIsWrittenInFieldOrder(t *testing.T) {
	tokyo := time.FixedZone("JST", 9*60*60)
	h := newHarness(t, func(int, *http.Request, recorded) (*http.Response, error) {
		return newResponse(200, "application/json", `{"data":[],"has_more":false,"next_cursor":null}`), nil
	})
	q := ListClientEventsParams{
		From:        time.Date(2026, 8, 1, 9, 0, 0, 123456789, tokyo),
		To:          time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		OutcomeType: new("a+b c"),
		Limit:       new(50),
		Page:        new(3),
	}
	if _, err := list(context.Background(), h.client, call{op: mustOp(t, "listClientEvents"), path: []string{"c1"}}, &q, nil,
		func(b *ClientEventList) []AttributedEvent { return b.Data }); err != nil {
		t.Fatal(err)
	}
	want := "https://api.test/v1/clients/c1/events?from=2026-08-01T09%3A00%3A00.123456789%2B09%3A00" +
		"&to=2026-09-01T00%3A00%3A00Z&outcome_type=a%2Bb+c&limit=50&page=3"
	if got := h.transport.get(0).url; got != want {
		t.Errorf("url =\n %s\nwant\n %s", got, want)
	}
}

func TestAQueryInstantOutsideRFC3339IsRefused(t *testing.T) {
	for _, year := range []int{10000, -1} {
		h := newHarness(t, ok200)
		q := GetClientCostParams{From: time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC), To: time.Now()}
		var out map[string]any
		err := h.client.do(context.Background(), call{op: mustOp(t, "getClientCost"), path: []string{"c1"}, query: &q}, nil, &out)
		if !isPlain(err) || h.transport.count() != 0 {
			t.Errorf("year %d: err = %v after %d requests, want a plain error and none", year, err, h.transport.count())
		}
	}
}

func TestANilQuerySendsNone(t *testing.T) {
	h := newHarness(t, ok200)
	var out map[string]any
	var q *GetAgentParams
	if err := h.client.do(context.Background(), call{op: mustOp(t, "getAgent"), path: []string{"a1"}, query: q}, nil, &out); err != nil {
		t.Fatal(err)
	}
	if got := h.transport.get(0).url; got != "https://api.test/v1/agents/a1" {
		t.Errorf("url = %s, want no query", got)
	}
}

func TestA204IgnoresTheBody(t *testing.T) {
	for _, status := range []int{200, 204} {
		h := newHarness(t, func(int, *http.Request, recorded) (*http.Response, error) {
			return newResponse(status, "text/plain", "not json"), nil
		})
		if err := h.client.do(context.Background(), call{op: mustOp(t, "deleteClient"), path: []string{"c1"}}, nil, nil); err != nil {
			t.Errorf("status %d: err = %v, want nil for an operation that answers 204", status, err)
		}
	}
}

func TestA2xxBodyOfTheWrongShapeIsAnAPIError(t *testing.T) {
	for _, body := range []string{`[]`, `null`, `not json`, `{"name":`} {
		h := newHarness(t, func(int, *http.Request, recorded) (*http.Response, error) {
			return newResponse(200, "application/json", body, "X-Request-Id", "req-9"), nil
		})
		var out ClientDetail
		err := h.client.do(context.Background(), call{op: mustOp(t, "getClient"), path: []string{"c1"}}, nil, &out)
		var apiErr *APIError
		if !errors.As(err, &apiErr) || apiErr.Status != 200 || apiErr.Problem != nil || apiErr.RequestID != "req-9" {
			t.Errorf("body %s: err = %v, want an APIError with status 200 and no problem", body, err)
		}
		if n := h.transport.count(); n != 1 {
			t.Errorf("body %s: %d attempts, want 1", body, n)
		}
	}
}

func TestAnUnknownResponseMemberIsIgnored(t *testing.T) {
	h := newHarness(t, func(int, *http.Request, recorded) (*http.Response, error) {
		return newResponse(200, "application/json", `{"status":"ok","version":"1.2","added_later":{"x":[1]}}`), nil
	})
	var out Health
	if err := h.client.do(context.Background(), call{op: mustOp(t, "getHealth")}, nil, &out); err != nil {
		t.Fatal(err)
	}
	if out.Status != "ok" || out.Version != "1.2" {
		t.Errorf("decoded %+v", out)
	}
}

func TestAPublicOperationSendsNoCredential(t *testing.T) {
	var calls atomic.Int32
	h := newHarness(t, ok200, func(s *settings) {
		s.apiKey = nil
	}, WithTokenProvider(func(context.Context) (string, error) {
		calls.Add(1)
		return "aia_fromprovider", nil
	}))
	var out map[string]any
	if err := h.client.do(context.Background(), call{op: mustOp(t, "getHealth")}, nil, &out); err != nil {
		t.Fatal(err)
	}
	if _, has := h.transport.get(0).header["Authorization"]; has {
		t.Error("getHealth sent Authorization")
	}
	if calls.Load() != 0 {
		t.Errorf("the token provider was called %d times for getHealth", calls.Load())
	}
	// The same client sends the provider's token on any other operation.
	if err := h.client.do(context.Background(), call{op: mustOp(t, "getPriceTable")}, nil, &out); err != nil {
		t.Fatal(err)
	}
	if got := h.transport.get(1).header.Get("Authorization"); got != "Bearer aia_fromprovider" || calls.Load() != 1 {
		t.Errorf("getPriceTable sent %q after %d provider calls", got, calls.Load())
	}
}

func TestATokenProviderErrorIsReturnedUnchanged(t *testing.T) {
	sentinel := errors.New("vault is sealed")
	var calls atomic.Int32
	h := newHarness(t, ok200, func(s *settings) { s.apiKey = nil }, WithTokenProvider(func(context.Context) (string, error) {
		calls.Add(1)
		return "", sentinel
	}))
	var out map[string]any
	err := h.client.do(context.Background(), call{op: mustOp(t, "getPriceTable")}, nil, &out)
	if err != sentinel { // the provider's own error, unchanged, not wrapped
		t.Errorf("err = %v, want the provider's error itself", err)
	}
	if calls.Load() != 1 || h.transport.count() != 0 {
		t.Errorf("%d provider calls and %d requests, want 1 and none", calls.Load(), h.transport.count())
	}

	empty := newHarness(t, ok200, func(s *settings) { s.apiKey = nil }, WithTokenProvider(func(context.Context) (string, error) {
		return "  ", nil
	}))
	err = empty.client.do(context.Background(), call{op: mustOp(t, "getPriceTable")}, nil, &out)
	if !errors.Is(err, ErrAllisall) || empty.transport.count() != 0 {
		t.Errorf("empty token: err = %v after %d requests, want ErrAllisall and none", err, empty.transport.count())
	}
}

func TestUserAgentCanBeReplaced(t *testing.T) {
	h := newHarness(t, ok200, WithHeader("user-agent", "wrapper/1"))
	var out map[string]any
	if err := h.client.do(context.Background(), call{op: mustOp(t, "getPriceTable")}, nil, &out); err != nil {
		t.Fatal(err)
	}
	if got := h.transport.get(0).header.Values("User-Agent"); !slices.Equal(got, []string{"wrapper/1"}) {
		t.Errorf("User-Agent = %q, want the client's header alone", got)
	}
	opts := []IdempotentRequestOption{WithRequestHeader("USER-AGENT", "call/2")}
	if err := h.client.do(context.Background(), call{op: mustOp(t, "getPriceTable")}, opts, &out); err != nil {
		t.Fatal(err)
	}
	if got := h.transport.get(1).header.Values("User-Agent"); !slices.Equal(got, []string{"call/2"}) {
		t.Errorf("User-Agent = %q, want the call's header alone", got)
	}
}

// ineligible are the operations a failed attempt is never retried on:
// the five PATCHes, the one POST without an Idempotency-Key, and the
// version-guarded PUT of a value model's line items. Listed here, not read
// from the table, so a table change that makes one eligible fails this
// test.
var ineligible = []string{"previewImport", "setValueModelLineItems", "updateAgent", "updateClient", "updateCostLineItem", "updateReport", "updateValueModel"}

// idempotentOps are the operations that send an Idempotency-Key.
var idempotentOps = []string{"commitImport", "createAgent", "createCalculation", "createClient", "createCostLineItem", "createReport", "createValueModel"}

func TestEveryOperationIsRetriedOnlyWhenEligible(t *testing.T) {
	table := operationTable()
	if len(table) != 48 {
		t.Fatalf("the table holds %d operations, want 48", len(table))
	}
	for i := range table {
		op := &table[i]
		t.Run(op.id, func(t *testing.T) {
			h := newHarness(t, func(int, *http.Request, recorded) (*http.Response, error) {
				return problemResponse(500, "internal", "req-1"), nil
			})
			err := h.client.do(context.Background(), anyCall(op), nil, nil)
			var apiErr *APIError
			if !errors.As(err, &apiErr) || apiErr.Status != 500 {
				t.Fatalf("err = %v, want the 500", err)
			}
			want := 3
			if slices.Contains(ineligible, op.id) {
				want = 1
			}
			if n := h.transport.count(); n != want {
				t.Errorf("%d attempts, want %d", n, want)
			}
			keys := map[string]bool{}
			for n := range h.transport.count() {
				keys[h.transport.get(n).header.Get("Idempotency-Key")] = true
			}
			switch {
			case slices.Contains(idempotentOps, op.id) && (len(keys) != 1 || keys[""]):
				t.Errorf("Idempotency-Key across attempts = %v, want one key on every attempt", keys)
			case !slices.Contains(idempotentOps, op.id) && !keys[""]:
				t.Errorf("Idempotency-Key = %v, want none", keys)
			}
		})
	}
}

func TestAnIdempotentCallReusesOneKeyAndOneBody(t *testing.T) {
	h := newHarness(t, func(n int, _ *http.Request, _ recorded) (*http.Response, error) {
		if n%2 == 0 {
			return problemResponse(503, "service_unavailable", "r"), nil
		}
		return newResponse(201, "application/json", `{"id":"c1","name":"Acme"}`), nil
	})
	body := ClientCreate{Name: "Acme", Status: ClientStatusActive}
	for range 2 {
		var out ClientRecord
		if err := h.client.do(context.Background(), call{op: mustOp(t, "createClient"), body: body}, nil, &out); err != nil {
			t.Fatal(err)
		}
		if out.ID != "c1" {
			t.Errorf("decoded %+v", out)
		}
	}
	first, second := h.transport.get(0), h.transport.get(1)
	key := first.header.Get("Idempotency-Key")
	if !uuidV4.MatchString(key) || second.header.Get("Idempotency-Key") != key {
		t.Errorf("keys %q and %q, want one UUID v4 on both attempts", key, second.header.Get("Idempotency-Key"))
	}
	if string(first.body) != string(second.body) {
		t.Errorf("bodies differ: %s and %s", first.body, second.body)
	}
	if first.header.Get("X-Request-Id") == second.header.Get("X-Request-Id") {
		t.Error("both attempts sent the same X-Request-Id")
	}
	if next := h.transport.get(2).header.Get("Idempotency-Key"); next == key || !uuidV4.MatchString(next) {
		t.Errorf("the next call sent %q, want a new key", next)
	}
}

func TestACallersIdempotencyKeyIsSentAsGiven(t *testing.T) {
	h := newHarness(t, func(n int, _ *http.Request, _ recorded) (*http.Response, error) {
		if n == 0 {
			return problemResponse(500, "internal", "r"), nil
		}
		return newResponse(201, "application/json", `{}`), nil
	})
	const key = "job-42 run 7"
	var out map[string]any
	opts := []IdempotentRequestOption{WithIdempotencyKey(key)}
	if err := h.client.do(context.Background(), call{op: mustOp(t, "createClient"), body: ClientCreate{}}, opts, &out); err != nil {
		t.Fatal(err)
	}
	for n := range 2 {
		if got := h.transport.get(n).header.Get("Idempotency-Key"); got != key {
			t.Errorf("attempt %d sent %q, want %q", n, got, key)
		}
	}

	for _, bad := range []string{"a\nb", "a\rb", "a\x00b", "a\x7fb"} {
		h := newHarness(t, ok200)
		err := h.client.do(context.Background(), call{op: mustOp(t, "createClient"), body: ClientCreate{}},
			[]IdempotentRequestOption{WithIdempotencyKey(bad)}, &out)
		if !isPlain(err) || h.transport.count() != 0 {
			t.Errorf("key %q: err = %v after %d requests, want a plain error and none", bad, err, h.transport.count())
		}
	}
}

func TestA409KeyInUseIsRetriedOnlyOnAnIdempotentOperation(t *testing.T) {
	keyInUse := func(n int, _ *http.Request, _ recorded) (*http.Response, error) {
		if n == 0 {
			return problemResponse(409, "idempotency_key_in_use", "r"), nil
		}
		return newResponse(201, "application/json", `{"id":"c1"}`), nil
	}
	h := newHarness(t, keyInUse)
	var out ClientRecord
	if err := h.client.do(context.Background(), call{op: mustOp(t, "createClient"), body: ClientCreate{}}, nil, &out); err != nil {
		t.Fatalf("createClient: %v, want the 201 after the 409", err)
	}
	if h.transport.count() != 2 || out.ID != "c1" ||
		h.transport.get(0).header.Get("Idempotency-Key") != h.transport.get(1).header.Get("Idempotency-Key") {
		t.Errorf("%d attempts, out %+v; want 2 with one key", h.transport.count(), out)
	}

	other := newHarness(t, func(int, *http.Request, recorded) (*http.Response, error) {
		return problemResponse(409, "client_key_in_use", "r"), nil
	})
	err := other.client.do(context.Background(), call{op: mustOp(t, "createClient"), body: ClientCreate{}}, nil, &out)
	if !errors.As(err, new(*APIError)) || other.transport.count() != 1 {
		t.Errorf("client_key_in_use: err = %v after %d attempts, want 1", err, other.transport.count())
	}

	// setClientAgents is eligible, as a PUT, but takes no key.
	put := newHarness(t, keyInUse)
	err = put.client.do(context.Background(), call{op: mustOp(t, "setClientAgents"), path: []string{"c1"}, body: struct{}{}}, nil, &out)
	if err == nil || put.transport.count() != 1 {
		t.Errorf("setClientAgents: err = %v after %d attempts, want the 409 after 1", err, put.transport.count())
	}

	ingest := newHarness(t, keyInUse)
	_, err = ingest.client.Events.Ingest(context.Background(), smallEvents(1))
	if !errors.As(err, new(*IngestError)) || ingest.transport.count() != 1 {
		t.Errorf("Ingest: err = %v after %d attempts, want an IngestError after 1", err, ingest.transport.count())
	}
}

func TestRequestMaxRetries(t *testing.T) {
	fail := func(int, *http.Request, recorded) (*http.Response, error) {
		return problemResponse(500, "internal", "r"), nil
	}
	tests := []struct {
		name   string
		client []Option
		opt    RequestOption
		id     string
		want   int
	}{
		{"lowers the client's 2", nil, WithRequestMaxRetries(0), "getPriceTable", 1},
		{"raises the client's 0", []Option{WithMaxRetries(0)}, WithRequestMaxRetries(2), "getPriceTable", 3},
		{"cannot make a PATCH eligible", nil, WithRequestMaxRetries(5), "updateClient", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t, fail, tt.client...)
			op := mustOp(t, tt.id)
			_ = h.client.do(context.Background(), anyCall(op), []IdempotentRequestOption{tt.opt}, nil)
			if n := h.transport.count(); n != tt.want {
				t.Errorf("%d attempts, want %d", n, tt.want)
			}
		})
	}
}

func TestRequestTimeoutEndsAnAttempt(t *testing.T) {
	h := newHarness(t, func(_ int, req *http.Request, _ recorded) (*http.Response, error) {
		<-req.Context().Done()
		return nil, req.Context().Err()
	}, WithTimeout(time.Minute))
	start := time.Now()
	var out map[string]any
	err := h.client.do(context.Background(), call{op: mustOp(t, "getPriceTable")},
		[]IdempotentRequestOption{WithRequestTimeout(20 * time.Millisecond), WithRequestMaxRetries(0)}, &out)
	var connErr *ConnectionError
	if !errors.As(err, &connErr) {
		t.Fatalf("err = %v, want a ConnectionError", err)
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Errorf("the attempt took %v, so the client's minute bounded it", elapsed)
	}
}

func TestAnInvalidTimeoutOrRetryCountIsRefusedBeforeSending(t *testing.T) {
	for _, opt := range []RequestOption{WithRequestTimeout(0), WithRequestTimeout(-time.Second), WithRequestMaxRetries(-1)} {
		h := newHarness(t, ok200)
		var out map[string]any
		err := h.client.do(context.Background(), call{op: mustOp(t, "getPriceTable")}, []IdempotentRequestOption{opt}, &out)
		if !errors.Is(err, ErrAllisall) || errors.As(err, new(*APIError)) || h.transport.count() != 0 {
			t.Errorf("err = %v after %d requests, want an ErrAllisall error and none", err, h.transport.count())
		}
	}
}

func TestACallsHeaderReplacesTheClientsOfTheSameName(t *testing.T) {
	h := newHarness(t, ok200, WithHeader("X-Trace", "client"), WithHeader("X-Tenant-Hint", "kept"))
	var out map[string]any
	if err := h.client.do(context.Background(), call{op: mustOp(t, "getPriceTable")}, nil, &out); err != nil {
		t.Fatal(err)
	}
	if got := h.transport.get(0).header.Values("X-Trace"); !slices.Equal(got, []string{"client"}) {
		t.Errorf("X-Trace = %q, want the client's", got)
	}
	opts := []IdempotentRequestOption{WithRequestHeader("x-trace", "call")}
	if err := h.client.do(context.Background(), call{op: mustOp(t, "getPriceTable")}, opts, &out); err != nil {
		t.Fatal(err)
	}
	got := h.transport.get(1).header
	if !slices.Equal(got.Values("X-Trace"), []string{"call"}) || got.Get("X-Tenant-Hint") != "kept" {
		t.Errorf("headers = %v, want the call's X-Trace alone and the client's other header", got)
	}
}

var badHeaders = []struct{ name, value string }{
	{"Authorization", "Bearer x"},
	{"content-type", "text/plain"},
	{"IDEMPOTENCY-KEY", "k"},
	{"X-Request-Id", "r"},
	{"X Trace", "v"},
	{"", "v"},
	{"X-Trace", "a\nb"},
	{"X-Trace", "a\rb"},
}

func TestAHeaderTheSDKOwnsOrMalformedIsRefused(t *testing.T) {
	for _, bad := range badHeaders {
		h := newHarness(t, ok200)
		var out map[string]any
		err := h.client.do(context.Background(), call{op: mustOp(t, "getPriceTable")},
			[]IdempotentRequestOption{WithRequestHeader(bad.name, bad.value)}, &out)
		if !isPlain(err) || h.transport.count() != 0 {
			t.Errorf("WithRequestHeader(%q, %q): err = %v after %d requests, want a plain error and none",
				bad.name, bad.value, err, h.transport.count())
		}
		if _, err := NewClient(WithAPIKey("aia_k"), WithHeader(bad.name, bad.value)); !errors.Is(err, ErrAllisall) {
			t.Errorf("WithHeader(%q, %q): NewClient err = %v, want ErrAllisall", bad.name, bad.value, err)
		}
	}
	// A tab is a legal header byte.
	if _, err := NewClient(WithAPIKey("aia_k"), WithHeader("X-Trace", "a\tb")); err != nil {
		t.Errorf("a tab in a value: %v", err)
	}
}

func TestIngestWithoutOptionsSendsWhat010Sent(t *testing.T) {
	h := newHarness(t, nil)
	events := []EventInput{{EventID: "e1", Timestamp: fakeNow, AgentKey: "a", Category: EventCategoryTask, EventType: "t"}}
	if _, err := h.client.Events.Ingest(context.Background(), events); err != nil {
		t.Fatal(err)
	}
	got := h.transport.get(0)
	if got.method != "POST" || got.url != "https://api.test/v1/events" {
		t.Errorf("sent %s %s", got.method, got.url)
	}
	var names []string
	for name := range got.header {
		names = append(names, name)
	}
	slices.Sort(names)
	if want := []string{"Authorization", "Content-Type", "User-Agent", "X-Request-Id"}; !slices.Equal(names, want) {
		t.Errorf("headers %v, want exactly %v", names, want)
	}
	if got.header.Get("Content-Type") != "application/json" || got.header.Get("Authorization") != "Bearer aia_testkey" ||
		got.header.Get("User-Agent") != userAgent() {
		t.Errorf("headers = %v", got.header)
	}
	want := `{"events":[{"event_id":"e1","timestamp":"2026-10-28T12:00:00Z","agent_key":"a","category":"task","event_type":"t"}]}`
	if string(got.body) != want {
		t.Errorf("body =\n %s\nwant\n %s", got.body, want)
	}
}

func TestIngestTakesTheRequestOptions(t *testing.T) {
	fail := func(int, *http.Request, recorded) (*http.Response, error) {
		return problemResponse(500, "internal", "r"), nil
	}
	h := newHarness(t, fail)
	_, err := h.client.Events.Ingest(context.Background(), smallEvents(1), WithRequestMaxRetries(0))
	if !errors.As(err, new(*IngestError)) || h.transport.count() != 1 {
		t.Errorf("WithRequestMaxRetries(0): err = %v after %d attempts, want 1", err, h.transport.count())
	}

	h = newHarness(t, nil)
	if _, err := h.client.Events.Ingest(context.Background(), smallEvents(501), WithRequestHeader("X-Batch-Source", "job")); err != nil {
		t.Fatal(err)
	}
	for n := range h.transport.count() {
		if got := h.transport.get(n).header.Get("X-Batch-Source"); got != "job" {
			t.Errorf("batch %d sent X-Batch-Source %q", n, got)
		}
		if _, has := h.transport.get(n).header["Idempotency-Key"]; has {
			t.Errorf("batch %d sent an Idempotency-Key", n)
		}
	}
	if h.transport.count() != 2 {
		t.Errorf("%d batches, want 2", h.transport.count())
	}

	h = newHarness(t, nil)
	_, err = h.client.Events.Ingest(context.Background(), smallEvents(1), WithRequestHeader("Content-Type", "text/plain"))
	if !isPlain(err) || errors.As(err, new(*IngestError)) || h.transport.count() != 0 {
		t.Errorf("an owned header: err = %v after %d requests, want a plain error and none", err, h.transport.count())
	}
}
