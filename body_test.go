package allisall

import (
	"context"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

// These tests hold the refusals the request layer makes before sending,
// beyond the options': a body string that is not UTF-8, a header net/http
// would drop, and the import's file. And two rules it keeps: the base
// URL's query, and ingest checking its options first.

func TestAStringThatIsNotUTF8IsRefusedWithItsPointer(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		path    []string
		body    any
		upload  *ImportUpload
		pointer string
	}{
		{"a JSON member", "createClient", nil, ClientCreate{Name: "Ac\xffme"}, nil, "/name"},
		{"a nested array item", "createClient", nil, map[string]any{"a": map[string]any{"b/c": []any{"ok", "x\xfe"}}}, nil, "/a/b~1c/1"},
		{"a member name", "createClient", nil, map[string]string{"k\xff": "v"}, nil, "/k\xff"},
		{"a set Nullable in a merge patch", "updateClient", []string{"c1"}, ClientPatch{ClientKey: Null[string](), Notes: NewNullable("n\xff")}, nil, "/notes"},
		{"the import's mapping", "commitImport", nil, nil,
			&ImportUpload{Mapping: ImportMapping{Constants: &map[string]string{"agent_key": "\xfe"}}, File: strings.NewReader("a\n")},
			"/constants/agent_key"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t, ok200)
			err := h.client.do(context.Background(), call{op: mustOp(t, tt.id), path: tt.path, body: tt.body, upload: tt.upload}, nil, new(map[string]any))
			if !isPlain(err) || h.transport.count() != 0 {
				t.Fatalf("err = %v after %d requests, want a plain error and none", err, h.transport.count())
			}
			if !strings.Contains(err.Error(), "UTF-8 at the member "+tt.pointer) {
				t.Errorf("err = %q, want it to name %s", err, tt.pointer)
			}
		})
	}
}

func TestValidTextIsSentAsWritten(t *testing.T) {
	h := newHarness(t, ok200)
	body := ClientPatch{Name: new("Café ☕"), Notes: Null[string](), ClientKey: NewNullable("ключ")}
	if err := h.client.do(context.Background(), call{op: mustOp(t, "updateClient"), path: []string{"c1"}, body: body}, nil, new(map[string]any)); err != nil {
		t.Fatal(err)
	}
	if got, want := string(h.transport.get(0).body), `{"client_key":"ключ","name":"Café ☕","notes":null}`; got != want {
		t.Errorf("body = %s, want %s", got, want)
	}
}

func TestAHeaderNetHTTPWouldDropIsRefused(t *testing.T) {
	for _, name := range []string{"Host", "host", "Content-Length", "CONTENT-LENGTH", "Transfer-Encoding", "transfer-encoding"} {
		h := newHarness(t, ok200)
		err := h.client.do(context.Background(), call{op: mustOp(t, "getPriceTable")},
			[]IdempotentRequestOption{WithRequestHeader(name, "x")}, new(map[string]any))
		if !isPlain(err) || h.transport.count() != 0 {
			t.Errorf("WithRequestHeader(%q): err = %v after %d requests, want a plain error and none", name, err, h.transport.count())
		}
		if _, err := NewClient(WithAPIKey("aia_k"), WithHeader(name, "x")); !errors.Is(err, ErrAllisall) {
			t.Errorf("WithHeader(%q): NewClient err = %v, want ErrAllisall", name, err)
		}
	}
}

func TestTheBaseURLsQueryIsKept(t *testing.T) {
	h := newHarness(t, ok200, WithBaseURL("https://api.test/api?tenant=x"))
	from := fakeNow
	if err := h.client.do(context.Background(), call{op: mustOp(t, "getAgent"), path: []string{"a1"}, query: &GetAgentParams{From: &from}}, nil, new(map[string]any)); err != nil {
		t.Fatal(err)
	}
	if err := h.client.do(context.Background(), call{op: mustOp(t, "getPriceTable")}, nil, new(map[string]any)); err != nil {
		t.Fatal(err)
	}
	for n, want := range []string{
		"https://api.test/api/v1/agents/a1?tenant=x&from=2026-10-28T12%3A00%3A00Z",
		"https://api.test/api/v1/price-table?tenant=x",
	} {
		if got := h.transport.get(n).url; got != want {
			t.Errorf("request %d = %s, want %s", n, got, want)
		}
	}
}

func TestIngestChecksItsOptionsBeforeTheEmptyListShortcut(t *testing.T) {
	h := newHarness(t, nil)
	if _, err := h.client.Events.Ingest(context.Background(), nil, WithRequestTimeout(0)); !errors.Is(err, ErrAllisall) || errors.As(err, new(*IngestError)) {
		t.Errorf("an invalid option with no events: err = %v, want an ErrAllisall error", err)
	}
	s, err := h.client.Events.Ingest(context.Background(), nil, WithRequestMaxRetries(0))
	if err != nil || s.Batches != 0 || h.transport.count() != 0 {
		t.Errorf("a valid option with no events: %+v, %v after %d requests; want an empty summary and none", s, err, h.transport.count())
	}
}

// An io.Seeker may seek past its end, which leaves no length to send.
func TestAFileEndingBeforeItsPositionIsRefused(t *testing.T) {
	r := strings.NewReader(csvText)
	if _, err := r.Seek(int64(len(csvText))+10, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	h, _, err := commit(t, ImportUpload{File: r}, ok200)
	if !isPlain(err) || h.transport.count() != 0 {
		t.Errorf("err = %v after %d requests, want a plain error and none", err, h.transport.count())
	}
}

// rewindFails rewinds once, then fails: the first attempt is sent, and the
// retry cannot rewind.
type rewindFails struct {
	*strings.Reader
	rewinds int
}

func (r *rewindFails) Seek(offset int64, whence int) (int64, error) {
	if whence == io.SeekStart {
		r.rewinds++
		if r.rewinds > 1 {
			return 0, errors.New("the file went away")
		}
	}
	return r.Reader.Seek(offset, whence)
}

func TestAFailedRewindEndsTheCall(t *testing.T) {
	h, _, err := commit(t, ImportUpload{File: &rewindFails{Reader: strings.NewReader(csvText)}},
		func(int, *http.Request, recorded) (*http.Response, error) {
			return problemResponse(503, "service_unavailable", "r"), nil
		})
	if !isPlain(err) || errors.As(err, new(*ConnectionError)) || !strings.Contains(err.Error(), "the file went away") {
		t.Errorf("err = %v, want the rewind's plain error", err)
	}
	if h.transport.count() != 1 {
		t.Errorf("%d attempts, want the first alone", h.transport.count())
	}
}

// named is a reader with a Name, as *os.File has.
type named struct {
	*strings.Reader
	name string
}

func (n named) Name() string { return n.name }

func TestAFileNameWithNoBaseFallsBack(t *testing.T) {
	for _, name := range []string{"", ".", string(filepath.Separator), "dir" + string(filepath.Separator)} {
		h, _, err := commit(t, ImportUpload{File: named{strings.NewReader(csvText), name}}, ok200)
		if err != nil {
			t.Fatal(err)
		}
		want := "upload.csv"
		if name == "dir"+string(filepath.Separator) {
			want = "dir" // filepath.Base drops a trailing separator
		}
		if got := parseForm(t, h.transport.get(0))[1].filename; got != want {
			t.Errorf("Name() %q: file name %q, want %q", name, got, want)
		}
	}
}
