package allisall

import (
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// renewalsPageJSON is one page of the renewals list, as the API sends it.
func renewalsPageJSON(ids []string, cursor, today, extra string) string {
	var items []string
	for _, id := range ids {
		items = append(items, fmt.Sprintf(`{"client_id":%q,"name":"n-%s","renewal_date":"2027-01-31"%s}`, id, id, extra))
	}
	next := "null"
	if cursor != "" {
		next = fmt.Sprintf("%q", cursor)
	}
	return fmt.Sprintf(`{"today":%q,"page":null,"total":null,"data":[%s],"has_more":%t,"next_cursor":%s}`,
		today, strings.Join(items, ","), cursor != "", next)
}

func TestARepeatableParameterIsSentOncePerItemInOrder(t *testing.T) {
	h := newHarness(t, func(int, *http.Request, recorded) (*http.Response, error) {
		return newResponse(200, "application/json", renewalsPageJSON(nil, "", "2026-11-10", "")), nil
	})
	// Two different items, so a writer that sent only the first, or joined
	// them, fails.
	terms := []RenewalTermFilter{RenewalTermFilterAnnual, RenewalTermFilterNone}
	q := &ListRenewalsParams{RenewalTerm: &terms, Window: new(RenewalWindowNext30Days)}
	if _, err := h.client.Dashboard.ListRenewals(context.Background(), q); err != nil {
		t.Fatal(err)
	}
	got, err := url.ParseQuery(strings.SplitN(h.transport.get(0).url, "?", 2)[1])
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"annual", "none"}; !slices.Equal(got["renewal_term"], want) {
		t.Errorf("renewal_term = %v, want %v", got["renewal_term"], want)
	}
	if !strings.Contains(h.transport.get(0).url, "renewal_term=annual&renewal_term=none") {
		t.Errorf("the items are not sent as one parameter each, in order: %s", h.transport.get(0).url)
	}
}

func TestARepeatableParameterLeavesOutNilAndRefusesEmpty(t *testing.T) {
	empty := []RenewalTermFilter{}
	var nilSlice []RenewalTermFilter
	for _, tc := range []struct {
		name    string
		terms   *[]RenewalTermFilter
		wantErr bool
	}{
		{"a nil pointer", nil, false},
		{"a nil slice", &nilSlice, false},
		{"an empty slice", &empty, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, func(int, *http.Request, recorded) (*http.Response, error) {
				return newResponse(200, "application/json", renewalsPageJSON(nil, "", "2026-11-10", "")), nil
			})
			_, err := h.client.Dashboard.ListRenewals(context.Background(), &ListRenewalsParams{RenewalTerm: tc.terms})
			if !tc.wantErr {
				if err != nil || h.transport.count() != 1 || strings.Contains(h.transport.get(0).url, "renewal_term") {
					t.Errorf("err = %v, %d requests; want one request without renewal_term", err, h.transport.count())
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), "renewal_term") {
				t.Fatalf("err = %v, want an error naming renewal_term", err)
			}
			if errors.Is(err, ErrAllisall) {
				t.Errorf("err matches ErrAllisall; it is a caller's mistake, not the API's: %v", err)
			}
			if h.transport.count() != 0 {
				t.Errorf("%d requests sent, want none", h.transport.count())
			}
		})
	}
}

func TestARenewalsWalkPinsTheFirstPagesToday(t *testing.T) {
	for _, tc := range []struct {
		name  string
		today *string
		want  []string
	}{
		// The first page echoes a date unlike the test's own, and the later
		// pages another, so a walk that sent the local date, or the latest
		// page's, fails.
		{"without today", nil, []string{"", "2026-11-09", "2026-11-09"}},
		{"with the caller's today", new("2026-11-11"), []string{"2026-11-11", "2026-11-11", "2026-11-11"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, func(n int, _ *http.Request, _ recorded) (*http.Response, error) {
				switch n {
				case 0:
					return newResponse(200, "application/json", renewalsPageJSON([]string{"c1"}, "off-1", "2026-11-09", "")), nil
				case 1:
					return newResponse(200, "application/json", renewalsPageJSON([]string{"c2"}, "off-2", "2026-11-10", "")), nil
				}
				return newResponse(200, "application/json", renewalsPageJSON([]string{"c3"}, "", "2026-11-10", "")), nil
			})
			page, err := h.client.Dashboard.ListRenewals(context.Background(), &ListRenewalsParams{Today: tc.today, Limit: new(1)})
			if err != nil {
				t.Fatal(err)
			}
			var ids []string
			for r, err := range page.All(context.Background()) {
				if err != nil {
					t.Fatal(err)
				}
				ids = append(ids, r.ClientID)
			}
			if got := strings.Join(ids, ","); got != "c1,c2,c3" {
				t.Errorf("walked %s, want c1,c2,c3", got)
			}
			for n, want := range tc.want {
				q, err := url.ParseQuery(strings.SplitN(h.transport.get(n).url, "?", 2)[1])
				if err != nil {
					t.Fatal(err)
				}
				if got := q.Get("today"); got != want {
					t.Errorf("request %d sent today %q, want %q", n, got, want)
				}
			}
		})
	}
}

func TestARenewalsWalkPinsNothingWhenTheFirstPageEchoesAnEmptyToday(t *testing.T) {
	h := newHarness(t, func(n int, _ *http.Request, _ recorded) (*http.Response, error) {
		if n == 0 {
			return newResponse(200, "application/json", renewalsPageJSON([]string{"c1"}, "off-1", "", "")), nil
		}
		return newResponse(200, "application/json", renewalsPageJSON([]string{"c2"}, "", "2026-11-10", "")), nil
	})
	page, err := h.client.Dashboard.ListRenewals(context.Background(), &ListRenewalsParams{Limit: new(1)})
	if err != nil {
		t.Fatal(err)
	}
	for _, err := range page.All(context.Background()) {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := h.transport.get(1).url; strings.Contains(got, "today") {
		t.Errorf("the second request pinned an empty today: %s", got)
	}
}

func TestARenewalTermTheSDKDoesNotListIsKept(t *testing.T) {
	h := newHarness(t, func(int, *http.Request, recorded) (*http.Response, error) {
		return newResponse(200, "application/json", renewalsPageJSON([]string{"c1"}, "", "2026-11-10", `,"renewal_term":"quarterly"`)), nil
	})
	page, err := h.client.Dashboard.ListRenewals(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	term := page.Data[0].RenewalTerm
	if term == nil || *term != RenewalTerm("quarterly") || term.Valid() {
		t.Errorf("RenewalTerm = %v, want the unlisted quarterly, kept", term)
	}
}

func TestAClientsRenewalTermIsSentAndReturnedAndNullClearsIt(t *testing.T) {
	answers := []string{"annual", "monthly", "null"}
	h := newHarness(t, func(n int, _ *http.Request, _ recorded) (*http.Response, error) {
		term := answers[n]
		if term != "null" {
			term = fmt.Sprintf("%q", term)
		}
		return newResponse(200, "application/json", `{"id":"client-1","name":"Acme","status":"active","renewal_term":`+term+`}`), nil
	})
	ctx := context.Background()
	created, err := h.client.Clients.Create(ctx, ClientCreate{Name: "Acme", Status: ClientStatusActive, RenewalTerm: new(RenewalTermAnnual)})
	if err != nil {
		t.Fatal(err)
	}
	if created.RenewalTerm == nil || *created.RenewalTerm != RenewalTermAnnual {
		t.Errorf("created RenewalTerm = %v, want annual", created.RenewalTerm)
	}
	patched, err := h.client.Clients.Update(ctx, "client-1", ClientPatch{RenewalTerm: NewNullable(RenewalTermMonthly)})
	if err != nil {
		t.Fatal(err)
	}
	if patched.RenewalTerm == nil || *patched.RenewalTerm != RenewalTermMonthly {
		t.Errorf("patched RenewalTerm = %v, want monthly", patched.RenewalTerm)
	}
	cleared, err := h.client.Clients.Update(ctx, "client-1", ClientPatch{RenewalTerm: Null[RenewalTerm]()})
	if err != nil {
		t.Fatal(err)
	}
	if cleared.RenewalTerm != nil {
		t.Errorf("cleared RenewalTerm = %v, want nil", *cleared.RenewalTerm)
	}
	for n, want := range []string{`"renewal_term":"annual"`, `{"renewal_term":"monthly"}`, `{"renewal_term":null}`} {
		if body := string(h.transport.get(n).body); !strings.Contains(body, want) {
			t.Errorf("request %d body %s, want it to hold %s", n, body, want)
		}
	}
}

// repeatableInSpec returns each operation's query parameters the spec marks
// x-repeatable, by operationId.
func repeatableInSpec(lines []string) map[string][]string {
	out := map[string][]string{}
	for i, line := range lines {
		if strings.TrimSpace(line) != "x-repeatable: true" {
			continue
		}
		var name, op string
		for j := i - 1; j >= 0 && (name == "" || op == ""); j-- {
			trimmed := strings.TrimSpace(lines[j])
			if name == "" && strings.HasPrefix(trimmed, "- name: ") {
				name = strings.TrimPrefix(trimmed, "- name: ")
			}
			if op == "" && strings.HasPrefix(trimmed, "operationId: ") {
				op = strings.TrimPrefix(trimmed, "operationId: ")
			}
		}
		out[op] = append(out[op], name)
	}
	return out
}

// sliceQueryFields returns each Params struct's slice fields, by form name,
// keyed by the operationId the struct is named for.
func sliceQueryFields(t *testing.T) map[string][]string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), "types_gen.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][]string{}
	for _, decl := range f.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range gen.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok || !strings.HasSuffix(ts.Name.Name, "Params") {
				continue
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok {
				continue
			}
			op := strings.ToLower(ts.Name.Name[:1]) + strings.TrimSuffix(ts.Name.Name[1:], "Params")
			for _, field := range st.Fields.List {
				typ := field.Type
				if star, ok := typ.(*ast.StarExpr); ok {
					typ = star.X
				}
				if _, ok := typ.(*ast.ArrayType); !ok || field.Tag == nil {
					continue
				}
				tag := reflect.StructTag(strings.Trim(field.Tag.Value, "`"))
				name, _, _ := strings.Cut(tag.Get("form"), ",")
				out[op] = append(out[op], name)
			}
		}
	}
	return out
}

// TestTheSliceQueryFieldsAreTheRepeatableParameters holds the generated
// query fields that are slices to the parameters the spec marks
// x-repeatable. A field that is a slice cannot take a single value, so this
// is also what holds the refusal of one.
func TestTheSliceQueryFieldsAreTheRepeatableParameters(t *testing.T) {
	spec := repeatableInSpec(readSpec(t))
	if want := map[string][]string{"listRenewals": {"renewal_term"}}; !reflect.DeepEqual(spec, want) {
		t.Errorf("the spec marks %v, want %v", spec, want)
	}
	if got := sliceQueryFields(t); !reflect.DeepEqual(got, spec) {
		t.Errorf("the generated slice query fields are %v, the spec's repeatable parameters %v", got, spec)
	}
}

// TestThePinnedMemberIsInTheQueryAndTheBody holds pinnedEchoOperation and
// pinnedEchoField to the spec: the operation's query takes the field as a
// *string, and its list body echoes it.
func TestThePinnedMemberIsInTheQueryAndTheBody(t *testing.T) {
	if pinnedEchoOperation != "listRenewals" || pinnedEchoField != "Today" {
		t.Fatalf("pinned %s.%s, want listRenewals.Today", pinnedEchoOperation, pinnedEchoField)
	}
	if f, ok := reflect.TypeFor[ListRenewalsParams]().FieldByName(pinnedEchoField); !ok || f.Type != stringPointerType {
		t.Error("ListRenewalsParams has no Today *string")
	}
	if f, ok := reflect.TypeFor[RenewalList]().FieldByName(pinnedEchoField); !ok || f.Type.Kind() != reflect.String {
		t.Error("RenewalList has no Today string")
	}
	if !slices.Contains(specProperties(readSpec(t), "RenewalList"), "today") {
		t.Error("the spec's RenewalList has no today")
	}
}
