package allisall

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

// TestIngestIsRetryEligible holds ingest's eligibility to the table: the
// operation is a POST without an Idempotency-Key, so only its id makes it
// eligible, and a renamed id would silently drop its retries.
func TestIngestIsRetryEligible(t *testing.T) {
	op := mustOp(t, opIngestEvents)
	if op.method != http.MethodPost || op.path != "/v1/events" || op.idempotent {
		t.Fatalf("%s is %s %s, idempotent %t; want the ingest POST without a key", op.id, op.method, op.path, op.idempotent)
	}
	if !retryEligible(op) {
		t.Error("ingest is not retry eligible")
	}
}

// switchable is a file whose Seek can be made to fail.
type switchable struct {
	*strings.Reader
	fail bool
}

func (s *switchable) Seek(offset int64, whence int) (int64, error) {
	if s.fail {
		return 0, errors.New("the file went away")
	}
	return s.Reader.Seek(offset, whence)
}

func TestGetBodyReportsAFailedRewind(t *testing.T) {
	file := &switchable{Reader: strings.NewReader(csvText)}
	f, err := newForm(&ImportUpload{File: file})
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, "https://api.test/v1/imports", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.attach(req); err != nil {
		t.Fatal(err)
	}
	first, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatal(err)
	}

	// While the file seeks, GetBody gives the same bytes again.
	again, err := req.GetBody()
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := io.ReadAll(again); !bytes.Equal(b, first) || int64(len(b)) != req.ContentLength {
		t.Fatalf("GetBody gave %d bytes, want the first body's %d", len(b), len(first))
	}

	file.fail = true
	body, err := req.GetBody()
	if !isPlain(err) || !strings.Contains(err.Error(), "the file went away") {
		t.Errorf("err = %v, want the rewind's plain error", err)
	}
	if body != nil {
		t.Error("GetBody returned a body with its error")
	}
}
