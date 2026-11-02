package allisall

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

// cursorServer answers listClients from a map of cursor ("" for none) to
// that page's item id and next_cursor ("" for the last page). Each page
// answers with its own status, so an error can be traced to the page
// that caused it.
func cursorServer(pages map[string][2]string, status map[string]int) func(int, *http.Request, recorded) (*http.Response, error) {
	return func(n int, req *http.Request, _ recorded) (*http.Response, error) {
		if n >= 10 {
			// A walk that never ends fails here rather than hanging.
			return problemResponse(400, "invalid_value", "too-many"), nil
		}
		cur := req.URL.Query().Get("cursor")
		p, ok := pages[cur]
		if !ok {
			return problemResponse(400, "invalid_value", "r"), nil
		}
		code := 200
		if s, ok := status[cur]; ok {
			code = s
		}
		return newResponse(code, "application/json", clientsPage([]string{p[0]}, p[1])), nil
	}
}

// walk collects a walk's item ids and errors.
func walk(page *Page[ClientRecord, ClientList]) (ids string, errs []error) {
	for c, err := range page.All(context.Background()) {
		if err != nil {
			errs = append(errs, err)
			continue
		}
		ids += c.ID
	}
	return ids, errs
}

// wantRepeat checks that err is the repeated cursor's APIError, with the
// offending page's status and no problem.
func wantRepeat(t *testing.T, err error, status int) {
	t.Helper()
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != status || apiErr.Problem != nil {
		t.Errorf("err = %v, want an APIError with status %d and no problem", err, status)
	}
}

func TestAWalkRefusesARepeatedCursor(t *testing.T) {
	tests := []struct {
		name  string
		pages map[string][2]string
		// ids are the items yielded before the error; requests the
		// requests sent in all.
		ids      string
		requests int
	}{
		{"a page that names itself, A to A", map[string][2]string{"": {"1", "A"}, "A": {"2", "A"}}, "1", 2},
		{"a cycle of two pages, A to B to A", map[string][2]string{"": {"1", "A"}, "A": {"2", "B"}, "B": {"3", "A"}}, "12", 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// The page that repeats a cursor answers 203, so the error must
			// carry that page's status.
			last := map[string]int{"A": 203}
			if len(tt.pages) == 3 {
				last = map[string]int{"B": 203}
			}
			h := newHarness(t, cursorServer(tt.pages, last))
			ids, errs := walk(listClients(t, h, nil))
			if ids != tt.ids || len(errs) != 1 {
				t.Fatalf("items %q and %d errors, want %q then one error", ids, len(errs), tt.ids)
			}
			wantRepeat(t, errs[0], 203)
			if n := h.transport.count(); n != tt.requests {
				t.Errorf("%d requests, want %d", n, tt.requests)
			}
		})
	}
}

func TestNextPageRefusesARepeatedCursor(t *testing.T) {
	h := newHarness(t, cursorServer(map[string][2]string{"": {"1", "A"}, "A": {"2", "A"}}, nil))
	page := listClients(t, h, nil)
	_, err := page.NextPage(context.Background())
	wantRepeat(t, err, 200)
}

func TestTheCallersOwnCursorCountsAsSent(t *testing.T) {
	h := newHarness(t, cursorServer(map[string][2]string{"A": {"1", "A"}}, nil))
	_, err := list(context.Background(), h.client, call{op: mustOp(t, "listClients")},
		&ListClientsParams{Cursor: new("A")}, nil, clientData)
	wantRepeat(t, err, 200)
}

// TestALastPageMayCarryASentCursor holds that only a page with has_more
// true is checked: the walk never follows a last page's cursor, so one
// already sent, the caller's own included, is no repeat.
func TestALastPageMayCarryASentCursor(t *testing.T) {
	last := func(cursor string) string {
		return `{"data":[{"id":"2"}],"has_more":false,"next_cursor":"` + cursor + `"}`
	}
	t.Run("a cursor the walk sent", func(t *testing.T) {
		h := newHarness(t, func(_ int, req *http.Request, _ recorded) (*http.Response, error) {
			if req.URL.Query().Get("cursor") == "A" {
				return newResponse(200, "application/json", last("A")), nil
			}
			return newResponse(200, "application/json", clientsPage([]string{"1"}, "A")), nil
		})
		if ids, errs := walk(listClients(t, h, nil)); ids != "12" || len(errs) != 0 {
			t.Errorf("items %q, errors %v; want 12 and none", ids, errs)
		}
	})
	t.Run("the caller's own cursor", func(t *testing.T) {
		h := newHarness(t, func(int, *http.Request, recorded) (*http.Response, error) {
			return newResponse(200, "application/json", last("A")), nil
		})
		page, err := list(context.Background(), h.client, call{op: mustOp(t, "listClients")},
			&ListClientsParams{Cursor: new("A")}, nil, clientData)
		if err != nil || page.HasNextPage() || len(page.Data) != 1 {
			t.Errorf("page %+v, err %v; want the last page and no error", page, err)
		}
	})
}

// TestEachListCallKeepsItsOwnCursorRecord walks two separate list calls
// that both send cursor A, the second one page later than the first. Were
// the record shared across calls, the second would read A as a repeat.
func TestEachListCallKeepsItsOwnCursorRecord(t *testing.T) {
	// The limit tells the two calls apart: limit 1 reaches A on its second
	// page, limit 2 on its third.
	pages := map[string]map[string][2]string{
		"1": {"": {"1", "A"}, "A": {"2", ""}},
		"2": {"": {"x", "Z"}, "Z": {"y", "A"}, "A": {"z", ""}},
	}
	h := newHarness(t, func(_ int, req *http.Request, _ recorded) (*http.Response, error) {
		q := req.URL.Query()
		p, ok := pages[q.Get("limit")][q.Get("cursor")]
		if !ok {
			return problemResponse(400, "invalid_value", "r"), nil
		}
		return newResponse(200, "application/json", clientsPage([]string{p[0]}, p[1])), nil
	})
	for _, c := range []struct {
		limit int
		want  string
	}{{1, "12"}, {2, "xyz"}} {
		if ids, errs := walk(listClients(t, h, &ListClientsParams{Limit: new(c.limit)})); ids != c.want || len(errs) != 0 {
			t.Errorf("limit %d: items %q, errors %v; want %q and none", c.limit, ids, errs, c.want)
		}
	}
	if n := h.transport.count(); n != 5 {
		t.Errorf("%d requests, want 5", n)
	}
}

func TestWalkingAgainFromAPageIsNoRepeat(t *testing.T) {
	h := newHarness(t, cursorServer(map[string][2]string{"": {"1", "A"}, "A": {"2", "B"}, "B": {"3", ""}}, nil))
	page := listClients(t, h, nil)
	for run := range 2 {
		if ids, errs := walk(page); ids != "123" || len(errs) != 0 {
			t.Errorf("walk %d: items %q, errors %v; want 123 and none", run, ids, errs)
		}
	}
	second, err := page.NextPage(context.Background())
	if err != nil {
		t.Fatalf("NextPage after two walks: %v", err)
	}
	if ids, errs := walk(second); ids != "23" || len(errs) != 0 {
		t.Errorf("walk from the second page: %q, %v", ids, errs)
	}
	// The first page, two walks of two pages each, NextPage, then one page.
	if n := h.transport.count(); n != 7 {
		t.Errorf("%d requests, want 7", n)
	}
}
