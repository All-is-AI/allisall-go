package allisall

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"reflect"
	"sync"
)

// Page is one page of a list: its items, and the whole response they came
// in. T is the item type and B the list's response type, such as
// Page[ClientRecord, ClientList].
//
// The options of the call that fetched the first page apply to every page
// fetched from it.
type Page[T, B any] struct {
	// Data holds the page's items.
	Data []T
	// Body is the whole decoded response, with any members the list
	// carries beside its items, such as a ClientEventList's Total.
	Body B

	hasMore bool
	// next fetches the following page, with these options laid over the
	// call's. It is nil on the last page.
	next func(ctx context.Context, opts []RequestOption) (*Page[T, B], error)
}

// HasNextPage reports whether another page follows this one.
func (p *Page[T, B]) HasNextPage() bool { return p.hasMore }

// NextPage fetches the page after this one. Its options apply to that one
// request, laid over the options of the call that fetched the first page:
// an option set here replaces the call's, and a header set here replaces
// the call's header of the same name. The page it returns goes back to
// the call's options.
//
// On the last page it returns an error matching [ErrAllisall] and sends
// nothing.
func (p *Page[T, B]) NextPage(ctx context.Context, opts ...RequestOption) (*Page[T, B], error) {
	if !p.hasMore || p.next == nil {
		return nil, fmt.Errorf("%w: there is no page after this one", ErrAllisall)
	}
	return p.next(ctx, opts)
}

// All yields every item of this page, then of each page after it,
// fetching each page when the loop reaches it. A failed request yields
// one pair of the zero item and the error, and the walk ends; the items
// already yielded stay yielded. Leaving the loop early fetches nothing
// more.
//
// A page whose next_cursor was already sent earlier in the walk would
// repeat pages without end, so it ends the walk with an [*APIError]
// carrying that page's status and no problem. NextPage refuses it the
// same way.
//
// The API does not remove duplicates: a keyset list can show a row twice
// when its sort value changes during the walk, and an offset list can skip
// or repeat rows when rows are added.
func (p *Page[T, B]) All(ctx context.Context) iter.Seq2[T, error] {
	return func(yield func(T, error) bool) {
		page := p
		for {
			for _, item := range page.Data {
				if !yield(item, nil) {
					return
				}
			}
			if !page.HasNextPage() {
				return
			}
			next, err := page.NextPage(ctx)
			if err != nil {
				var zero T
				yield(zero, err)
				return
			}
			page = next
		}
	}
}

// Format prints the page for every fmt verb without the client it fetches
// the next page with, which holds the credential.
func (p Page[T, B]) Format(f fmt.State, _ rune) {
	_, _ = fmt.Fprintf(f, "allisall.Page{Data: %d items, HasNextPage: %t}", len(p.Data), p.hasMore)
}

// list calls a list operation and returns its first page. query points to
// the operation's Params struct, or is nil for none; data returns a
// response's items.
//
// Each following page is requested with the same query, its Cursor set to
// the previous page's next_cursor and its Page, on the one list that has
// one, cleared: a cursor is valid only with the filters it was issued
// under, and the API refuses a page and a cursor together.
func list[T, B, Q any](ctx context.Context, c *Client, cl call, query *Q, opts []RequestOption, data func(*B) []T) (*Page[T, B], error) {
	if err := checkCursorField[Q](); err != nil {
		return nil, err
	}
	var q Q
	if query != nil {
		q = *query
	}
	l := &lister[T, B, Q]{client: c, call: cl, opts: opts, data: data, sent: map[string]int{}}
	if first := reflect.ValueOf(q).FieldByName("Cursor"); !first.IsNil() {
		l.sent[first.Elem().String()] = 0
	}
	return l.fetch(ctx, q, 0, nil)
}

// lister fetches the pages of one list call.
type lister[T, B, Q any] struct {
	client *Client
	call   call
	// opts are the call's options, which apply to every page.
	opts []RequestOption
	data func(*B) []T

	mu sync.Mutex
	// sent maps each cursor the walk has sent, or is about to send, to the
	// position of the page it asks for: 0 for the first page, 1 for the
	// next. Walking again from an earlier page sends the same cursor for
	// the same position, which is no repeat.
	sent map[string]int
}

// repeated reports whether cursor was already sent for a page other than
// the one at position at, and otherwise records it for that position.
func (l *lister[T, B, Q]) repeated(cursor string, at int) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if prev, ok := l.sent[cursor]; ok && prev != at {
		return true
	}
	l.sent[cursor] = at
	return false
}

// fetch requests the page at position at with query q, and the call's
// options with extra laid over them. The page it returns fetches its
// successor with the call's options alone.
//
// A page whose next_cursor the walk has already sent, for an earlier page,
// would start a cycle: it ends the walk with an [*APIError] carrying the
// page's status and no problem, as an envelope that breaks the rule does.
func (l *lister[T, B, Q]) fetch(ctx context.Context, q Q, at int, extra []RequestOption) (*Page[T, B], error) {
	all := make([]IdempotentRequestOption, 0, len(l.opts)+len(extra))
	all = append(all, idempotentOptions(l.opts)...)
	all = append(all, idempotentOptions(extra)...)
	cfg, err := resolveOptions(all)
	if err != nil {
		return nil, err
	}
	cl := l.call
	cl.query = &q
	resp, err := l.client.run(ctx, cl, cfg)
	if err != nil {
		return nil, err
	}
	hasMore, cursor, ok := envelope(resp.body)
	if !ok || (hasMore && l.repeated(cursor, at+1)) {
		return nil, shapeError(resp)
	}
	page := &Page[T, B]{hasMore: hasMore}
	if err := decodeResponse(resp, &page.Body); err != nil {
		return nil, err
	}
	page.Data = l.data(&page.Body)
	if hasMore {
		nq := nextQuery(q, cursor)
		if l.call.op != nil && l.call.op.id == pinnedEchoOperation {
			pinEcho(&nq, &page.Body)
		}
		page.next = func(ctx context.Context, extra []RequestOption) (*Page[T, B], error) {
			return l.fetch(ctx, nq, at+1, extra)
		}
	}
	return page, nil
}

// envelope checks a list response's envelope: data an array, has_more a
// boolean, and next_cursor a string or null, which must be a string that
// is not empty when has_more is true. It returns has_more and the cursor.
func envelope(body []byte) (hasMore bool, cursor string, ok bool) {
	var members map[string]json.RawMessage
	if !isJSONObject(body) || json.Unmarshal(body, &members) != nil {
		return false, "", false
	}
	data := bytes.TrimSpace(members["data"])
	if len(data) == 0 || data[0] != '[' {
		return false, "", false
	}
	switch string(bytes.TrimSpace(members["has_more"])) {
	case "true":
		hasMore = true
	case "false":
	default:
		return false, "", false
	}
	raw := bytes.TrimSpace(members["next_cursor"])
	switch {
	case string(raw) == "null":
	case len(raw) > 0 && raw[0] == '"':
		if err := json.Unmarshal(raw, &cursor); err != nil {
			return false, "", false
		}
	default:
		return false, "", false
	}
	if hasMore && cursor == "" {
		return false, "", false
	}
	return hasMore, cursor, true
}

var stringPointerType = reflect.TypeFor[*string]()

// checkCursorField refuses a query type without a Cursor *string field,
// which a list's next request sets. Every list's Params struct has one.
func checkCursorField[Q any]() error {
	t := reflect.TypeFor[Q]()
	if t.Kind() == reflect.Struct {
		if f, ok := t.FieldByName("Cursor"); ok && f.Type == stringPointerType {
			return nil
		}
	}
	return fmt.Errorf("allisall: list query %s has no Cursor field", t)
}

// pinnedEchoOperation and pinnedEchoField name the one query member a walk
// pins to its first page's answer: on the renewals list, Today. When the
// caller's query leaves it nil, every later page sends the Today the first
// page's body echoed, so a walk that crosses UTC midnight reads every page
// under one date. A test holds both names to the spec.
const (
	pinnedEchoOperation = "listRenewals"
	pinnedEchoField     = "Today"
)

// pinEcho sets q's pinned field, a *string, to the body's field of the same
// name when q leaves it nil and the body holds a value. A later page's
// query already holds it, so every page after the first sends the first
// page's value.
func pinEcho[Q, B any](q *Q, body *B) {
	field := reflect.ValueOf(q).Elem().FieldByName(pinnedEchoField)
	if !field.IsValid() || field.Type() != stringPointerType || !field.IsNil() {
		return
	}
	echoed := reflect.ValueOf(body).Elem().FieldByName(pinnedEchoField)
	if !echoed.IsValid() || echoed.Kind() != reflect.String || echoed.String() == "" {
		return
	}
	value := echoed.String()
	field.Set(reflect.ValueOf(&value))
}

// nextQuery returns q with Cursor set to cursor and Page, when q has it,
// cleared. checkCursorField has checked that q has a Cursor *string.
func nextQuery[Q any](q Q, cursor string) Q {
	v := reflect.ValueOf(&q).Elem()
	v.FieldByName("Cursor").Set(reflect.ValueOf(&cursor))
	if f := v.FieldByName("Page"); f.IsValid() && f.CanSet() {
		f.Set(reflect.Zero(f.Type()))
	}
	return q
}
