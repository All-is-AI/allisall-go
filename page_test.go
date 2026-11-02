package allisall

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"
)

func clientsPage(ids []string, cursor string) string {
	var items []string
	for _, id := range ids {
		items = append(items, fmt.Sprintf(`{"id":%q,"name":"n-%s"}`, id, id))
	}
	next := "null"
	if cursor != "" {
		next = fmt.Sprintf("%q", cursor)
	}
	return fmt.Sprintf(`{"data":[%s],"has_more":%t,"next_cursor":%s}`, strings.Join(items, ","), cursor != "", next)
}

func clientData(b *ClientList) []ClientRecord { return b.Data }

// threePages answers listClients with three pages, each with its own
// cursor, so a walk that resent an earlier cursor gets the wrong page.
func threePages(_ int, req *http.Request, _ recorded) (*http.Response, error) {
	switch req.URL.Query().Get("cursor") {
	case "":
		return newResponse(200, "application/json", clientsPage([]string{"a", "b"}, "c1")), nil
	case "c1":
		return newResponse(200, "application/json", clientsPage([]string{"c", "d"}, "c2")), nil
	case "c2":
		return newResponse(200, "application/json", clientsPage([]string{"e"}, "")), nil
	}
	return problemResponse(400, "invalid_value", "r"), nil
}

func listClients(t *testing.T, h *harness, q *ListClientsParams, opts ...RequestOption) *Page[ClientRecord, ClientList] {
	t.Helper()
	page, err := list(context.Background(), h.client, call{op: mustOp(t, "listClients")}, q, opts, clientData)
	if err != nil {
		t.Fatal(err)
	}
	return page
}

func TestAllWalksEveryPage(t *testing.T) {
	h := newHarness(t, threePages)
	page := listClients(t, h, &ListClientsParams{Limit: new(2)})
	var ids []string
	for c, err := range page.All(context.Background()) {
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, c.ID)
	}
	if got := strings.Join(ids, ""); got != "abcde" {
		t.Errorf("items %q, want abcde", got)
	}
	want := []string{"limit=2", "limit=2&cursor=c1", "limit=2&cursor=c2"}
	if h.transport.count() != len(want) {
		t.Fatalf("%d requests, want %d", h.transport.count(), len(want))
	}
	for n, q := range want {
		if got := h.transport.get(n).url; got != "https://api.test/v1/clients?"+q {
			t.Errorf("request %d = %s, want query %s", n, got, q)
		}
	}
}

func TestTheEventsWalkDropsPageAndKeepsTheFilters(t *testing.T) {
	h := newHarness(t, func(n int, _ *http.Request, _ recorded) (*http.Response, error) {
		more, next := "true", `"off-300"`
		if n > 0 {
			more, next = "false", "null"
		}
		return newResponse(200, "application/json", fmt.Sprintf(
			`{"client_id":"c1","currency":"USD","data":[{"id":"e%d"}],"has_more":%s,"next_cursor":%s,"page":3,"period":{"from":null,"to":null},"total":321}`,
			n, more, next)), nil
	})
	q := ListClientEventsParams{
		From:  time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		To:    time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		Sort:  new(ListClientEventsParamsSortCost),
		Limit: new(100),
		Page:  new(3),
	}
	page, err := list(context.Background(), h.client, call{op: mustOp(t, "listClientEvents"), path: []string{"c1"}}, &q, nil,
		func(b *ClientEventList) []AttributedEvent { return b.Data })
	if err != nil {
		t.Fatal(err)
	}
	if page.Body.Total != 321 || len(page.Data) != 1 {
		t.Errorf("Body.Total = %d, %d items; want the response's 321 and 1", page.Body.Total, len(page.Data))
	}
	for range page.All(context.Background()) {
	}
	first, err := url.ParseQuery(strings.SplitN(h.transport.get(0).url, "?", 2)[1])
	if err != nil {
		t.Fatal(err)
	}
	second, err := url.ParseQuery(strings.SplitN(h.transport.get(1).url, "?", 2)[1])
	if err != nil {
		t.Fatal(err)
	}
	if first.Get("page") != "3" || first.Has("cursor") {
		t.Errorf("first request %v, want page=3 and no cursor", first)
	}
	if second.Has("page") || second.Get("cursor") != "off-300" {
		t.Errorf("second request %v, want the cursor and no page", second)
	}
	first.Del("page")
	second.Del("cursor")
	if !reflect.DeepEqual(first, second) {
		t.Errorf("the filters changed between pages: %v then %v", first, second)
	}
	if q.Page == nil || *q.Page != 3 || q.Cursor != nil {
		t.Errorf("the caller's query was changed: %+v", q)
	}
}

func TestAnEnvelopeBreakingItsRuleIsAnAPIError(t *testing.T) {
	for _, body := range []string{
		`{"data":[],"has_more":true,"next_cursor":null}`,
		`{"data":[],"has_more":true,"next_cursor":""}`,
		`{"data":[],"next_cursor":null}`,
		`{"data":[],"has_more":null,"next_cursor":null}`,
		`{"data":[],"has_more":"false","next_cursor":null}`,
		`{"data":{},"has_more":false,"next_cursor":null}`,
		`{"has_more":false,"next_cursor":null}`,
		`{"data":[],"has_more":false,"next_cursor":5}`,
		`{"data":[],"has_more":false}`,
		`[]`,
	} {
		h := newHarness(t, func(int, *http.Request, recorded) (*http.Response, error) {
			return newResponse(200, "application/json", body), nil
		})
		_, err := list(context.Background(), h.client, call{op: mustOp(t, "listClients")}, (*ListClientsParams)(nil), nil, clientData)
		var apiErr *APIError
		if !errors.As(err, &apiErr) || apiErr.Status != 200 || apiErr.Problem != nil {
			t.Errorf("body %s: err = %v, want an APIError with status 200 and no problem", body, err)
		}
	}
	// The rule's own case: a last page whose cursor is null is fine.
	h := newHarness(t, func(int, *http.Request, recorded) (*http.Response, error) {
		return newResponse(200, "application/json", `{"data":[],"has_more":false,"next_cursor":null}`), nil
	})
	if _, err := list(context.Background(), h.client, call{op: mustOp(t, "listClients")}, (*ListClientsParams)(nil), nil, clientData); err != nil {
		t.Errorf("a valid last page: %v", err)
	}
}

func TestAWalkEndsAtAFailedPage(t *testing.T) {
	h := newHarness(t, func(n int, req *http.Request, rec recorded) (*http.Response, error) {
		if n == 1 {
			return problemResponse(404, "not_found", "r"), nil
		}
		return threePages(n, req, rec)
	})
	page := listClients(t, h, nil)
	var ids []string
	var errs []error
	for c, err := range page.All(context.Background()) {
		if err != nil {
			errs = append(errs, err)
			continue
		}
		ids = append(ids, c.ID)
	}
	if strings.Join(ids, "") != "ab" || len(errs) != 1 || !errors.As(errs[0], new(*APIError)) {
		t.Errorf("items %v, errors %v; want a and b, then one APIError", ids, errs)
	}
	if h.transport.count() != 2 {
		t.Errorf("%d requests, want 2", h.transport.count())
	}
}

func TestBreakingOutOfAWalkSendsNothingMore(t *testing.T) {
	h := newHarness(t, threePages)
	page := listClients(t, h, nil)
	for range page.All(context.Background()) {
		break
	}
	for c := range page.All(context.Background()) {
		if c.ID == "c" {
			break
		}
	}
	if h.transport.count() != 2 {
		t.Errorf("%d requests, want the first page and the one the second walk reached", h.transport.count())
	}
}

func TestNextPage(t *testing.T) {
	h := newHarness(t, threePages)
	page := listClients(t, h, nil, WithRequestHeader("X-Call", "call"), WithRequestHeader("X-Both", "call"))
	if !page.HasNextPage() {
		t.Fatal("the first page has no next page")
	}
	second, err := page.NextPage(context.Background(), WithRequestHeader("X-Next", "once"), WithRequestHeader("x-both", "next"))
	if err != nil {
		t.Fatal(err)
	}
	third, err := second.NextPage(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if third.HasNextPage() || third.Data[0].ID != "e" {
		t.Fatalf("third page %+v", third.Data)
	}
	type sent struct{ call, next, both string }
	want := []sent{{"call", "", "call"}, {"call", "once", "next"}, {"call", "", "call"}}
	for n, w := range want {
		hd := h.transport.get(n).header
		got := sent{hd.Get("X-Call"), hd.Get("X-Next"), hd.Get("X-Both")}
		if got != w || len(hd.Values("X-Both")) != 1 {
			t.Errorf("request %d sent %+v (X-Both %q), want %+v", n, got, hd.Values("X-Both"), w)
		}
	}

	before := h.transport.count()
	if _, err := third.NextPage(context.Background()); !errors.Is(err, ErrAllisall) || errors.As(err, new(*APIError)) {
		t.Errorf("NextPage on the last page: err = %v, want an ErrAllisall error", err)
	}
	if h.transport.count() != before {
		t.Error("NextPage on the last page sent a request")
	}
}

func TestPageFormatHidesTheClient(t *testing.T) {
	h := newHarness(t, threePages, WithAPIKey("aia_0123SECRET"))
	page := listClients(t, h, nil)
	for _, verb := range []string{"%v", "%+v", "%#v", "%s"} {
		for _, v := range []any{page, *page} {
			if out := fmt.Sprintf(verb, v); strings.Contains(out, "SECRET") || strings.Contains(out, "aia_") {
				t.Errorf("Sprintf(%q) = %q, which holds the key", verb, out)
			}
		}
	}
}

// listParams is every list operation's query type.
var listParams = map[string]reflect.Type{
	"listAgents":             reflect.TypeFor[ListAgentsParams](),
	"listCalculationReports": reflect.TypeFor[ListCalculationReportsParams](),
	"listClientCalculations": reflect.TypeFor[ListClientCalculationsParams](),
	"listClientEvents":       reflect.TypeFor[ListClientEventsParams](),
	"listClientOutcomeTypes": reflect.TypeFor[ListClientOutcomeTypesParams](),
	"listClients":            reflect.TypeFor[ListClientsParams](),
	"listCostLineItems":      reflect.TypeFor[ListCostLineItemsParams](),
	"listOutcomeTypes":       reflect.TypeFor[ListOutcomeTypesParams](),
	"listRenewals":           reflect.TypeFor[ListRenewalsParams](),
	"listValueModels":        reflect.TypeFor[ListValueModelsParams](),
	"listValueTemplates":     reflect.TypeFor[ListValueTemplatesParams](),
}

func TestEveryListQueryHasACursor(t *testing.T) {
	var lists []string
	for _, op := range operationTable() {
		if op.list != listNone {
			lists = append(lists, op.id)
			if _, ok := listParams[op.id]; !ok {
				t.Errorf("list %s has no entry in listParams", op.id)
			}
		}
	}
	if len(lists) != len(listParams) || len(lists) != 11 {
		t.Errorf("%d lists in the table, %d in listParams; want 11", len(lists), len(listParams))
	}
	for id, typ := range listParams {
		if f, ok := typ.FieldByName("Cursor"); !ok || f.Type != stringPointerType {
			t.Errorf("%s's query %s has no Cursor *string", id, typ.Name())
		}
	}
	if err := checkCursorField[struct{ Limit *int }](); err == nil {
		t.Error("a query without Cursor was accepted")
	}
	h := newHarness(t, ok200)
	if _, err := list(context.Background(), h.client, call{op: mustOp(t, "listClients")}, &struct{ Limit *int }{}, nil,
		clientData); err == nil || h.transport.count() != 0 {
		t.Errorf("a list with no Cursor in its query: err = %v after %d requests", err, h.transport.count())
	}
}
