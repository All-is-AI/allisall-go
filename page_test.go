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

// pageAndTotal reads a numbered page's Body.Page and Body.Total.
type pageAndTotal struct{ page, total *int }

// walkFromPage reads the first page and the next, and walks the list again
// with All, returning each page's Body.Page and Body.Total and the number
// of items the walk yields.
func walkFromPage[T, B any](t *testing.T, first func() (*Page[T, B], error), read func(*B) pageAndTotal) (one, two pageAndTotal, items int) {
	t.Helper()
	page, err := first()
	if err != nil {
		t.Fatal(err)
	}
	next, err := page.NextPage(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	again, err := first()
	if err != nil {
		t.Fatal(err)
	}
	for _, err := range again.All(context.Background()) {
		if err != nil {
			t.Fatal(err)
		}
		items++
	}
	return read(&page.Body), read(&next.Body), items
}

// TestAWalkStartedAtAPageContinuesByCursor holds the lists that take page,
// with q on the first four, and Limit standing in for a filter on the
// imports, which take none. Each first page answers in page
// mode, with a total; the next in cursor mode, with none, as the server
// does. A walk that kept Page would send it twice, and one that dropped a
// filter would lose it on the second request.
func TestAWalkStartedAtAPageContinuesByCursor(t *testing.T) {
	const someID = "7b0e8c1a-2f3d-4e5a-9b6c-1d2e3f4a5b6c"
	respond := func(_ int, req *http.Request, _ recorded) (*http.Response, error) {
		if req.URL.Query().Has("cursor") {
			return newResponse(200, "application/json",
				`{"data":[{"id":"b"}],"has_more":false,"next_cursor":null,"page":null,"total":null}`), nil
		}
		return newResponse(200, "application/json",
			`{"data":[{"id":"a"}],"has_more":true,"next_cursor":"after-a","page":3,"total":7}`), nil
	}
	ctx := context.Background()
	cases := []struct {
		name    string
		path    string
		filters string
		run     func(*Client) (one, two pageAndTotal, items int)
	}{
		{"Clients.List", "/v1/clients", "q=acme&status=active", func(c *Client) (pageAndTotal, pageAndTotal, int) {
			q := &ListClientsParams{Q: new("acme"), Status: new(ClientStatusActive), Page: new(3)}
			return walkFromPage(t, func() (*Page[ClientRecord, ClientList], error) { return c.Clients.List(ctx, q) },
				func(b *ClientList) pageAndTotal { return pageAndTotal{b.Page, b.Total} })
		}},
		{"Agents.List", "/v1/agents", "q=bot&status=paused", func(c *Client) (pageAndTotal, pageAndTotal, int) {
			q := &ListAgentsParams{Q: new("bot"), Status: new(AgentStatusPaused), Page: new(3)}
			return walkFromPage(t, func() (*Page[AgentListItem, AgentList], error) { return c.Agents.List(ctx, q) },
				func(b *AgentList) pageAndTotal { return pageAndTotal{b.Page, b.Total} })
		}},
		{"ValueModels.List", "/v1/value-models", "q=base&status=archived&client_id=" + someID, func(c *Client) (pageAndTotal, pageAndTotal, int) {
			q := &ListValueModelsParams{Q: new("base"), Status: new(ValueModelStatusArchived), ClientID: new(someID), Page: new(3)}
			return walkFromPage(t, func() (*Page[ValueModelSummary, ValueModelList], error) { return c.ValueModels.List(ctx, q) },
				func(b *ValueModelList) pageAndTotal { return pageAndTotal{b.Page, b.Total} })
		}},
		{"Cost.ListLineItems", "/v1/cost-line-items", "q=gpu&target_kind=agent&target_id=" + someID, func(c *Client) (pageAndTotal, pageAndTotal, int) {
			q := &ListCostLineItemsParams{Q: new("gpu"), TargetKind: new(CostTargetKindAgent), TargetID: new(someID), Page: new(3)}
			return walkFromPage(t, func() (*Page[CostLineItem, CostLineItemList], error) { return c.Cost.ListLineItems(ctx, q) },
				func(b *CostLineItemList) pageAndTotal { return pageAndTotal{b.Page, b.Total} })
		}},
		{"Calculations.List", "/v1/calculations", "client_id=" + someID + "&state=published&month=2026-08", func(c *Client) (pageAndTotal, pageAndTotal, int) {
			q := &ListCalculationsParams{ClientID: new(someID), State: new(CalculationStatePublished), Month: new("2026-08"), Page: new(3)}
			return walkFromPage(t, func() (*Page[CalculationSummary, WorkspaceCalculationList], error) {
				return c.Calculations.List(ctx, q)
			},
				func(b *WorkspaceCalculationList) pageAndTotal { return pageAndTotal{b.Page, b.Total} })
		}},
		{"Reports.List", "/v1/reports", "client_id=" + someID + "&state=shared", func(c *Client) (pageAndTotal, pageAndTotal, int) {
			q := &ListReportsParams{ClientID: new(someID), State: new(ReportStateShared), Page: new(3)}
			return walkFromPage(t, func() (*Page[ReportSummary, WorkspaceReportList], error) { return c.Reports.List(ctx, q) },
				func(b *WorkspaceReportList) pageAndTotal { return pageAndTotal{b.Page, b.Total} })
		}},
		{"Imports.List", "/v1/imports", "limit=1", func(c *Client) (pageAndTotal, pageAndTotal, int) {
			q := &ListImportsParams{Limit: new(1), Page: new(3)}
			return walkFromPage(t, func() (*Page[ImportSummary, ImportList], error) { return c.Imports.List(ctx, q) },
				func(b *ImportList) pageAndTotal { return pageAndTotal{b.Page, b.Total} })
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, respond)
			one, two, items := tc.run(h.client)
			if one.page == nil || *one.page != 3 || one.total == nil || *one.total != 7 {
				t.Errorf("first page's Body.Page, Body.Total = %v, %v; want 3 and 7", one.page, one.total)
			}
			if two.page != nil || two.total != nil {
				t.Errorf("second page's Body.Page, Body.Total = %v, %v; want nil", two.page, two.total)
			}
			if items != 2 {
				t.Errorf("All yielded %d items, want 2", items)
			}
			base := "https://api.test" + tc.path + "?" + tc.filters
			want := []string{base + "&page=3", base + "&cursor=after-a", base + "&page=3", base + "&cursor=after-a"}
			if h.transport.count() != len(want) {
				t.Fatalf("%d requests, want %d", h.transport.count(), len(want))
			}
			for n, w := range want {
				if got := h.transport.get(n).url; got != w {
					t.Errorf("request %d = %s, want %s", n, got, w)
				}
			}
		})
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
	"listCalculations":       reflect.TypeFor[ListCalculationsParams](),
	"listClientCalculations": reflect.TypeFor[ListClientCalculationsParams](),
	"listReports":            reflect.TypeFor[ListReportsParams](),
	"listClientEvents":       reflect.TypeFor[ListClientEventsParams](),
	"listClientOutcomeTypes": reflect.TypeFor[ListClientOutcomeTypesParams](),
	"listClients":            reflect.TypeFor[ListClientsParams](),
	"listCostLineItems":      reflect.TypeFor[ListCostLineItemsParams](),
	"listEventArrivals":      reflect.TypeFor[ListEventArrivalsParams](),
	"listEvents":             reflect.TypeFor[ListEventsParams](),
	"listImports":            reflect.TypeFor[ListImportsParams](),
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
	if len(lists) != len(listParams) || len(lists) != 16 {
		t.Errorf("%d lists in the table, %d in listParams; want 16", len(lists), len(listParams))
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

// eventsPageJSON is one page of the workspace events list or the arrivals
// feed, as the API sends it, with extra members written in.
func eventsPageJSON(ids []string, cursor, extra string) string {
	var items []string
	for _, id := range ids {
		items = append(items, fmt.Sprintf(`{"id":%q,"category":"task","source":"api"}`, id))
	}
	next := "null"
	if cursor != "" {
		next = fmt.Sprintf("%q", cursor)
	}
	return fmt.Sprintf(`{"as_of":"2026-09-02T03:04:05.678901Z","client_id":null,"currency":"JPY","data":[%s],"has_more":%t,"next_cursor":%s,"period":{"from":null,"to":null}%s}`,
		strings.Join(items, ","), cursor != "", next, extra)
}

// queries returns each request's decoded query, in order.
func queries(t *testing.T, h *harness) []url.Values {
	t.Helper()
	var out []url.Values
	for n := range h.transport.count() {
		q, err := url.ParseQuery(strings.SplitN(h.transport.get(n).url, "?", 2)[1])
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, q)
	}
	return out
}

func TestAnEventsWalkFollowsTheCursorWhichCarriesAsOf(t *testing.T) {
	asOf := time.Date(2026, 9, 2, 3, 4, 5, 678901000, time.UTC)
	for _, tc := range []struct {
		name   string
		asOf   *time.Time
		wantAt []string
	}{
		{"without as_of", nil, []string{"", ""}},
		{"with the caller's as_of", &asOf, []string{"2026-09-02T03:04:05.678901Z", "2026-09-02T03:04:05.678901Z"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, func(n int, _ *http.Request, _ recorded) (*http.Response, error) {
				if n == 0 {
					return newResponse(200, "application/json", eventsPageJSON([]string{"a", "b"}, "v2-after-b", "")), nil
				}
				return newResponse(200, "application/json", eventsPageJSON([]string{"c"}, "", "")), nil
			})
			q := &ListEventsParams{From: new(periodFrom), To: new(periodTo), AsOf: tc.asOf, Category: new(EventCategoryTask)}
			page, err := h.client.Events.List(context.Background(), q)
			if err != nil {
				t.Fatal(err)
			}
			if !page.Body.AsOf.Equal(asOf) {
				t.Errorf("Body.AsOf = %v, want %v", page.Body.AsOf, asOf)
			}
			var ids []string
			for e, err := range page.All(context.Background()) {
				if err != nil {
					t.Fatal(err)
				}
				ids = append(ids, e.ID)
			}
			if got := strings.Join(ids, ""); got != "abc" {
				t.Errorf("walked %q, want abc", got)
			}
			sent := queries(t, h)
			if len(sent) != 2 {
				t.Fatalf("%d requests, want 2", len(sent))
			}
			for n, q := range sent {
				if got := q.Get("as_of"); got != tc.wantAt[n] {
					t.Errorf("request %d as_of = %q, want %q", n, got, tc.wantAt[n])
				}
				if q.Get("category") != "task" || q.Get("from") == "" || q.Get("to") == "" {
					t.Errorf("request %d dropped a filter: %v", n, q)
				}
			}
			if sent[0].Has("cursor") || sent[1].Get("cursor") != "v2-after-b" {
				t.Errorf("cursors %q then %q, want none then v2-after-b", sent[0].Get("cursor"), sent[1].Get("cursor"))
			}
		})
	}
}

func TestASortedEventsPageReadsTotalCappedAndContinuesByOffset(t *testing.T) {
	h := newHarness(t, func(n int, _ *http.Request, _ recorded) (*http.Response, error) {
		if n == 0 {
			return newResponse(200, "application/json", eventsPageJSON([]string{"a"}, "offset-1", `,"page":1,"total":10000,"total_capped":true`)), nil
		}
		return newResponse(200, "application/json", eventsPageJSON([]string{"b"}, "", `,"page":null,"total":null,"total_capped":null`)), nil
	})
	q := &ListEventsParams{From: new(periodFrom), To: new(periodTo), Sort: new(ListEventsParamsSortCost), Dir: new(ListEventsParamsDirAsc), Limit: new(1), Page: new(1)}
	first, err := h.client.Events.List(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	if first.Body.Total == nil || *first.Body.Total != 10000 || first.Body.TotalCapped == nil || !*first.Body.TotalCapped {
		t.Errorf("first page Total, TotalCapped = %v, %v; want 10000 and true", first.Body.Total, first.Body.TotalCapped)
	}
	second, err := first.NextPage(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if second.Body.Total != nil || second.Body.TotalCapped != nil {
		t.Errorf("second page Total, TotalCapped = %v, %v; want nil", second.Body.Total, second.Body.TotalCapped)
	}
	sent := queries(t, h)
	if sent[0].Get("page") != "1" || sent[0].Has("cursor") {
		t.Errorf("first request %v, want page=1 and no cursor", sent[0])
	}
	if sent[1].Has("page") || sent[1].Get("cursor") != "offset-1" {
		t.Errorf("second request %v, want the offset cursor and no page", sent[1])
	}
	for n, q := range sent {
		if q.Get("sort") != "cost" || q.Get("dir") != "asc" {
			t.Errorf("request %d dropped the order: %v", n, q)
		}
	}
}

func TestArrivalsArePolledBySendingThePreviousAsOfAsSince(t *testing.T) {
	const window = `,"since":"2026-09-02T03:04:05.678901Z","count":7,"count_capped":false`
	h := newHarness(t, func(n int, _ *http.Request, _ recorded) (*http.Response, error) {
		if n == 0 {
			return newResponse(200, "application/json", eventsPageJSON([]string{"n1"}, "arr-2", window)), nil
		}
		return newResponse(200, "application/json", eventsPageJSON([]string{"n2"}, "", window)), nil
	})
	since := time.Date(2026, 9, 2, 3, 4, 5, 678901000, time.UTC)
	poll, err := h.client.Events.ListArrivals(context.Background(), &ListEventArrivalsParams{Since: &since, Limit: new(1)})
	if err != nil {
		t.Fatal(err)
	}
	if poll.Body.Count == nil || *poll.Body.Count != 7 || poll.Body.CountCapped == nil || *poll.Body.CountCapped {
		t.Errorf("Count, CountCapped = %v, %v; want 7 and false", poll.Body.Count, poll.Body.CountCapped)
	}
	if _, err := poll.NextPage(context.Background()); err != nil {
		t.Fatal(err)
	}
	sent := queries(t, h)
	for n, q := range sent {
		if q.Get("since") != "2026-09-02T03:04:05.678901Z" || q.Get("limit") != "1" {
			t.Errorf("request %d = %v, want since and limit kept", n, q)
		}
	}
	if sent[0].Has("cursor") || sent[1].Get("cursor") != "arr-2" {
		t.Errorf("cursors %q then %q, want none then arr-2", sent[0].Get("cursor"), sent[1].Get("cursor"))
	}
}

func TestAnEventCategoryAndSourceTheSDKDoesNotListAreKept(t *testing.T) {
	h := newHarness(t, func(int, *http.Request, recorded) (*http.Response, error) {
		body := strings.Replace(eventsPageJSON([]string{"e1"}, "", ""), `"category":"task","source":"api"`, `"category":"audit","source":"sftp"`, 1)
		return newResponse(200, "application/json", body), nil
	})
	page, err := h.client.Events.List(context.Background(), &ListEventsParams{From: new(periodFrom), To: new(periodTo)})
	if err != nil {
		t.Fatal(err)
	}
	e := page.Data[0]
	if e.Category != EventCategory("audit") || e.Source != EventSource("sftp") || e.Source.Valid() {
		t.Errorf("Category, Source = %q, %q; want the unlisted audit and sftp, kept", e.Category, e.Source)
	}
}
