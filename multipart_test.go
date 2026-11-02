package allisall

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

const csvText = "Time,Agent,Category,Type\n2026-10-01T00:00:00Z,a,task,t\n"

// formPart is one part of a form the fake transport received.
type formPart struct {
	name, filename, contentType string
	content                     []byte
}

func parseForm(t *testing.T, rec recorded) []formPart {
	t.Helper()
	mediaType, params, err := mime.ParseMediaType(rec.header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/form-data" {
		t.Fatalf("Content-Type %q is not multipart/form-data", rec.header.Get("Content-Type"))
	}
	r := multipart.NewReader(bytes.NewReader(rec.body), params["boundary"])
	var out []formPart
	for {
		p, err := r.NextPart()
		if errors.Is(err, io.EOF) {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(p)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, formPart{p.FormName(), p.FileName(), p.Header.Get("Content-Type"), b})
	}
}

// commit calls commitImport, answering with respond, and returns the
// harness and each request's Content-Length.
func commit(t *testing.T, upload ImportUpload, respond func(int, *http.Request, recorded) (*http.Response, error)) (*harness, []int64, error) {
	t.Helper()
	var mu sync.Mutex
	var lengths []int64
	h := newHarness(t, func(n int, req *http.Request, rec recorded) (*http.Response, error) {
		mu.Lock()
		lengths = append(lengths, req.ContentLength)
		mu.Unlock()
		if int64(len(rec.body)) != req.ContentLength {
			t.Errorf("attempt %d: Content-Length %d, body %d bytes", n, req.ContentLength, len(rec.body))
		}
		return respond(n, req, rec)
	})
	var out map[string]any
	err := h.client.do(context.Background(), call{op: mustOp(t, "commitImport"), upload: &upload}, nil, &out)
	return h, lengths, err
}

func TestTheImportFormHoldsTheMappingThenTheFile(t *testing.T) {
	mapping := ImportMapping{
		Columns:   &map[string]string{"timestamp": "Time"},
		Delimiter: new(ImportDelimiterSemicolon),
	}
	h, _, err := commit(t, ImportUpload{Mapping: mapping, File: strings.NewReader(csvText)}, ok200)
	if err != nil {
		t.Fatal(err)
	}
	parts := parseForm(t, h.transport.get(0))
	if len(parts) != 2 {
		t.Fatalf("%d parts, want 2", len(parts))
	}
	m, f := parts[0], parts[1]
	if m.name != "mapping" || m.filename != "mapping.json" || m.contentType != "application/json" ||
		string(m.content) != `{"columns":{"timestamp":"Time"},"delimiter":";"}` {
		t.Errorf("first part = %+v (%s)", m, m.content)
	}
	if f.name != "file" || f.filename != "upload.csv" || f.contentType != "text/csv" || string(f.content) != csvText {
		t.Errorf("second part = %+v (%s)", f, f.content)
	}
}

func TestARetriedImportSendsTheSameBytes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.csv")
	if err := os.WriteFile(path, []byte("skipped\n"+csvText), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	// The file is read from its position at the call.
	if _, err := file.Seek(int64(len("skipped\n")), io.SeekStart); err != nil {
		t.Fatal(err)
	}
	reader := bytes.NewReader([]byte("skipped\n" + csvText))
	if _, err := reader.Seek(int64(len("skipped\n")), io.SeekStart); err != nil {
		t.Fatal(err)
	}

	for name, f := range map[string]io.ReadSeeker{"*os.File": file, "*bytes.Reader": reader} {
		t.Run(name, func(t *testing.T) {
			h, lengths, err := commit(t, ImportUpload{File: f, Filename: "x.csv"}, func(n int, _ *http.Request, _ recorded) (*http.Response, error) {
				if n == 0 {
					return problemResponse(503, "service_unavailable", "r"), nil
				}
				return newResponse(200, "application/json", `{}`), nil
			})
			if err != nil {
				t.Fatal(err)
			}
			first, second := h.transport.get(0), h.transport.get(1)
			if !bytes.Equal(first.body, second.body) || lengths[0] != lengths[1] {
				t.Errorf("attempts differ: %d and %d bytes, Content-Length %v", len(first.body), len(second.body), lengths)
			}
			if first.header.Get("Idempotency-Key") == "" || first.header.Get("Idempotency-Key") != second.header.Get("Idempotency-Key") {
				t.Error("the attempts did not share one Idempotency-Key")
			}
			if got := parseForm(t, second)[1].content; string(got) != csvText {
				t.Errorf("the retry sent the file as %q, want it from the call's position", got)
			}
		})
	}
}

func TestTheImportFileName(t *testing.T) {
	path := filepath.Join(t.TempDir(), "march events.csv")
	if err := os.WriteFile(path, []byte(csvText), 0o600); err != nil {
		t.Fatal(err)
	}
	open := func() *os.File {
		f, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { f.Close() })
		return f
	}
	tests := []struct {
		name   string
		upload ImportUpload
		want   string
	}{
		{"a file keeps its base name", ImportUpload{File: open()}, "march events.csv"},
		{"Filename wins", ImportUpload{File: open(), Filename: "april.csv"}, "april.csv"},
		{"a nameless reader", ImportUpload{File: strings.NewReader(csvText)}, "upload.csv"},
		{"a quote and a line feed stay in the parameter", ImportUpload{File: strings.NewReader(csvText), Filename: "a\"b\nc.csv"}, "a\"b\nc.csv"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, _, err := commit(t, tt.upload, ok200)
			if err != nil {
				t.Fatal(err)
			}
			parts := parseForm(t, h.transport.get(0))
			if len(parts) != 2 {
				t.Fatalf("%d parts, want 2", len(parts))
			}
			// mime/multipart reads a %0A back as itself, not a line feed, so
			// compare the line feed's escaped form.
			if want := strings.ReplaceAll(tt.want, "\n", "%0A"); parts[1].filename != want {
				t.Errorf("file name %q, want %q", parts[1].filename, want)
			}
		})
	}
}

// failingSeeker reads, but cannot seek.
type failingSeeker struct{ io.Reader }

func (failingSeeker) Seek(int64, int) (int64, error) { return 0, errors.New("cannot seek") }

// unmeasurable seeks to a position, but cannot report one or find its end,
// so its length cannot be known.
type unmeasurable struct{ *strings.Reader }

func (u unmeasurable) Seek(offset int64, whence int) (int64, error) {
	if whence != io.SeekStart {
		return 0, errors.New("cannot measure")
	}
	return u.Reader.Seek(offset, whence)
}

func TestAFileThatCannotSeekIsRefused(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	for name, f := range map[string]io.ReadSeeker{
		"a failing Seek": failingSeeker{strings.NewReader(csvText)},
		"no length":      unmeasurable{strings.NewReader(csvText)},
		"a pipe":         r,
		"nil":            nil,
	} {
		h, _, err := commit(t, ImportUpload{File: f}, ok200)
		if !isPlain(err) || h.transport.count() != 0 {
			t.Errorf("%s: err = %v after %d requests, want a plain error and none", name, err, h.transport.count())
		}
	}
}
