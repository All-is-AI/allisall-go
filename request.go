package allisall

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// operations indexes the operation table by operationId. It is built once
// and never written.
var operations = func() map[string]*operation {
	table := operationTable()
	out := make(map[string]*operation, len(table))
	for i := range table {
		out[table[i].id] = &table[i]
	}
	return out
}()

// operationFor returns the table entry of an operation. A missing id is a
// mistake in the SDK, which its tests catch; it is an error, never a panic.
func operationFor(id string) (*operation, error) {
	op, ok := operations[id]
	if !ok {
		return nil, fmt.Errorf("allisall: no operation %q", id)
	}
	return op, nil
}

// opIngestEvents is the ingest operation's id: the one POST retried
// without an Idempotency-Key, because each event's event_id deduplicates.
// TestIngestIsRetryEligible fails if the table no longer holds it.
const opIngestEvents = "ingestEvents"

// opSetValueModelLineItems replaces a value model's line items under a
// version guard. It is a PUT, but never retried: the server refuses a
// stale version and does not replay a success, so a retry after a lost
// response would report a failure for a write that landed.
const opSetValueModelLineItems = "setValueModelLineItems"

// retryEligible reports whether a failed attempt of the operation may be
// retried at all: a request a retry cannot apply twice. That is a method
// RFC 9110 defines as idempotent, an operation that takes an
// Idempotency-Key the SDK reuses on every attempt, or ingest, whose
// event_id deduplicates. A version-guarded write is never retried.
func retryEligible(op *operation) bool {
	if op.id == opSetValueModelLineItems {
		return false
	}
	switch op.method {
	case http.MethodGet, http.MethodHead, http.MethodPut, http.MethodDelete:
		return true
	}
	return op.idempotent || op.id == opIngestEvents
}

// call is one call of an operation, as the caller passed it.
type call struct {
	op *operation
	// path holds the path parameters, in the order the template names
	// them.
	path []string
	// query is a pointer to the operation's Params struct, or nil.
	query any
	// body is a JSON or merge-patch body, encoded once.
	body any
	// raw is a JSON body already encoded, as ingest's batches are.
	raw []byte
	// upload is a multipart body.
	upload *ImportUpload
}

// prepared is a request fixed before its first attempt: every attempt
// sends the same URL, headers and body bytes. Only Authorization and
// X-Request-Id are set per attempt.
type prepared struct {
	op  *operation
	url string
	// header holds User-Agent, the client's and the call's headers, and the
	// Idempotency-Key.
	header      http.Header
	contentType string
	body        []byte
	form        *form
	timeout     time.Duration
	maxRetries  int
}

// prepare builds the request every attempt of a call sends. Each error it
// returns is the caller's mistake, found before anything is sent.
func (c *Client) prepare(cl call, cfg callConfig) (*prepared, error) {
	query, err := encodeQuery(cl.query)
	if err != nil {
		return nil, err
	}
	u, err := buildURL(c.baseURL, cl.op.path, cl.path, query)
	if err != nil {
		return nil, err
	}

	p := &prepared{
		op:         cl.op,
		url:        u,
		header:     http.Header{},
		timeout:    c.timeout,
		maxRetries: c.maxRetries,
	}
	if cfg.timeout != nil {
		p.timeout = *cfg.timeout
	}
	if cfg.maxRetries != nil {
		p.maxRetries = *cfg.maxRetries
	}
	if !retryEligible(cl.op) {
		p.maxRetries = 0
	}

	p.header.Set("User-Agent", c.userAgent)
	setHeaders(p.header, c.headers)
	setHeaders(p.header, cfg.headers)
	if cl.op.idempotent {
		key := newUUID()
		if cfg.idempotencyKey != nil {
			key = *cfg.idempotencyKey
		}
		p.header.Set("Idempotency-Key", key)
	}

	switch cl.op.body {
	case bodyNone:
		if cl.body != nil || cl.raw != nil || cl.upload != nil {
			return nil, fmt.Errorf("allisall: %s takes no body", cl.op.id)
		}
	case bodyJSON, bodyMergePatch:
		p.contentType = "application/json"
		if cl.op.body == bodyMergePatch {
			p.contentType = "application/merge-patch+json"
		}
		if cl.raw != nil {
			p.body = cl.raw
			break
		}
		b, err := encodeJSON(cl.body, "the request body")
		if err != nil {
			return nil, err
		}
		p.body = b
	case bodyMultipart:
		if cl.upload == nil {
			return nil, fmt.Errorf("allisall: %s takes an upload", cl.op.id)
		}
		f, err := newForm(cl.upload)
		if err != nil {
			return nil, err
		}
		p.form = f
		p.contentType = f.contentType
	}
	return p, nil
}

// encodeJSON encodes a request body, or the import's mapping, named what
// in its errors. A string that is not valid UTF-8 is refused, naming its
// JSON Pointer, because encoding/json would replace each invalid byte with
// U+FFFD and send text the caller never wrote.
func encodeJSON(v any, what string) ([]byte, error) {
	if pointer, bad := invalidUTF8At(reflect.ValueOf(v), ""); bad {
		where := "the member " + pointer
		if pointer == "" {
			where = "its value"
		}
		return nil, fmt.Errorf("allisall: %s is not valid UTF-8 at %s", what, where)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("allisall: encode %s: %w", what, err)
	}
	return b, nil
}

// nullableValue is a Nullable: its value, when one is set.
type nullableValue interface{ setValue() (any, bool) }

var jsonMarshaler = reflect.TypeFor[json.Marshaler]()

// invalidUTF8At finds a string that is not valid UTF-8, a member name
// included, in the parts of v that encoding/json writes. It returns the
// string's JSON Pointer, under pointer. It sees into a Nullable's value,
// and skips any other type that writes its own JSON, such as Decimal and
// time.Time, whose text is never the caller's.
func invalidUTF8At(v reflect.Value, pointer string) (string, bool) {
	if !v.IsValid() {
		return "", false
	}
	if v.CanInterface() {
		if n, ok := v.Interface().(nullableValue); ok {
			inner, set := n.setValue()
			if !set {
				return "", false
			}
			return invalidUTF8At(reflect.ValueOf(inner), pointer)
		}
	}
	if v.Kind() != reflect.Interface && v.Kind() != reflect.Pointer && v.Type().Implements(jsonMarshaler) {
		return "", false
	}
	switch v.Kind() {
	case reflect.String:
		if !utf8.ValidString(v.String()) {
			return pointer, true
		}
	case reflect.Interface, reflect.Pointer:
		if !v.IsNil() {
			return invalidUTF8At(v.Elem(), pointer)
		}
	case reflect.Map:
		if v.Type().Key().Kind() != reflect.String {
			return "", false
		}
		keys := make([]string, 0, v.Len())
		for _, k := range v.MapKeys() {
			keys = append(keys, k.String())
		}
		slices.Sort(keys)
		for _, k := range keys {
			at := pointer + "/" + escapePointer(k)
			if !utf8.ValidString(k) {
				return at, true
			}
			if p, bad := invalidUTF8At(v.MapIndex(reflect.ValueOf(k).Convert(v.Type().Key())), at); bad {
				return p, true
			}
		}
	case reflect.Slice, reflect.Array:
		if v.Type().Elem().Kind() == reflect.Uint8 {
			return "", false // written as base64
		}
		for i := 0; i < v.Len(); i++ {
			if p, bad := invalidUTF8At(v.Index(i), pointer+"/"+strconv.Itoa(i)); bad {
				return p, true
			}
		}
	case reflect.Struct:
		for _, f := range reflect.VisibleFields(v.Type()) {
			name, ok := jsonName(f)
			if !ok {
				continue
			}
			fv, err := v.FieldByIndexErr(f.Index)
			if err != nil {
				continue // behind a nil embedded pointer: not written
			}
			if p, bad := invalidUTF8At(fv, pointer+"/"+escapePointer(name)); bad {
				return p, true
			}
		}
	}
	return "", false
}

// do calls an operation that is not a list, and decodes its success
// response into out, a pointer to the response type. For an operation
// whose success status is 204, out is unused and any 2xx is a success.
func (c *Client) do(ctx context.Context, cl call, opts []IdempotentRequestOption, out any) error {
	cfg, err := resolveOptions(opts)
	if err != nil {
		return err
	}
	resp, err := c.run(ctx, cl, cfg)
	if err != nil {
		return err
	}
	if cl.op.status == http.StatusNoContent {
		return nil
	}
	if out == nil {
		// A mistake in the SDK, which its tests catch; never a panic.
		return fmt.Errorf("allisall: %s has no response type", cl.op.id)
	}
	return decodeResponse(resp, out)
}

// run prepares and sends a call. A token provider's own error is returned
// unchanged.
func (c *Client) run(ctx context.Context, cl call, cfg callConfig) (*response, error) {
	p, err := c.prepare(cl, cfg)
	if err != nil {
		return nil, err
	}
	resp, err := c.send(ctx, p)
	if err != nil {
		var pe *providerError
		if errors.As(err, &pe) {
			return nil, pe.err
		}
		return nil, err
	}
	return resp, nil
}

// shapeError is a successful response whose body is not the shape the
// operation documents.
func shapeError(resp *response) *APIError {
	return &APIError{
		Status:    resp.status,
		RequestID: resp.header.Get("X-Request-Id"),
		Headers:   resp.header,
	}
}

// buildURL joins the base URL and an operation's path, with each path
// parameter escaped as one segment, and appends the query.
//
// The path is built escaped and kept as the URL's RawPath, so an id
// holding a slash stays one segment. url.URL.JoinPath would clean "." and
// ".." segments, and joins the unescaped path. A parameter of "", "." or
// ".." cannot travel as one segment, and is refused.
func buildURL(base *url.URL, template string, params []string, query string) (string, error) {
	var b strings.Builder
	rest := template
	n := 0
	for {
		open := strings.IndexByte(rest, '{')
		if open < 0 {
			b.WriteString(rest)
			break
		}
		end := strings.IndexByte(rest[open:], '}')
		if end < 0 {
			return "", fmt.Errorf("allisall: path template %q is malformed", template)
		}
		if n >= len(params) {
			return "", fmt.Errorf("allisall: path %s takes more than %d parameters", template, len(params))
		}
		v := params[n]
		if v == "" || v == "." || v == ".." {
			return "", fmt.Errorf("allisall: a path parameter cannot be %q", v)
		}
		b.WriteString(rest[:open])
		b.WriteString(url.PathEscape(v))
		rest = rest[open+end+1:]
		n++
	}
	if n != len(params) {
		return "", fmt.Errorf("allisall: path %s takes %d parameters, not %d", template, n, len(params))
	}

	// JoinPath with no element cleans the base's path as 0.1.0's join did.
	u := base.JoinPath()
	raw := strings.TrimSuffix(u.EscapedPath(), "/") + b.String()
	unescaped, err := url.PathUnescape(raw)
	if err != nil {
		return "", fmt.Errorf("allisall: build the path: %w", err)
	}
	u.Path = unescaped
	u.RawPath = raw
	if query != "" {
		if u.RawQuery != "" {
			u.RawQuery += "&"
		}
		u.RawQuery += query
	}
	return u.String(), nil
}

var timeType = reflect.TypeFor[time.Time]()

// encodeQuery writes a Params struct as a query string: each field the
// caller set, in field order, which is the API's order. A nil pointer is
// left out; any other field is written. A string is written as it is, an
// integer in decimal, and a time.Time with RFC 3339 and nanoseconds,
// keeping its offset.
func encodeQuery(query any) (string, error) {
	if query == nil {
		return "", nil
	}
	v := reflect.ValueOf(query)
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return "", nil
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return "", fmt.Errorf("allisall: a query is a struct, not %s", v.Type())
	}
	var parts []string
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		name, _, _ := strings.Cut(f.Tag.Get("form"), ",")
		if name == "" || name == "-" {
			continue
		}
		fv := v.Field(i)
		if fv.Kind() == reflect.Pointer {
			if fv.IsNil() {
				continue
			}
			fv = fv.Elem()
		}
		if fv.Kind() == reflect.Slice {
			// A repeatable parameter: one parameter per item, in order. Only
			// a parameter the spec declares as an array is generated as a
			// slice. A nil slice is left out; an empty one has no faithful
			// form, because leaving the parameter out means every value.
			if fv.IsNil() {
				continue
			}
			if fv.Len() == 0 {
				return "", fmt.Errorf("allisall: query parameter %s is an empty list; leave it nil to match every value", name)
			}
			for j := 0; j < fv.Len(); j++ {
				s, err := queryValue(name, fv.Index(j))
				if err != nil {
					return "", err
				}
				parts = append(parts, url.QueryEscape(name)+"="+url.QueryEscape(s))
			}
			continue
		}
		s, err := queryValue(name, fv)
		if err != nil {
			return "", err
		}
		parts = append(parts, url.QueryEscape(name)+"="+url.QueryEscape(s))
	}
	return strings.Join(parts, "&"), nil
}

// queryValue is one query value's text: an instant as RFC 3339, a string
// as it is, an integer in decimal.
func queryValue(name string, fv reflect.Value) (string, error) {
	switch {
	case fv.Type() == timeType:
		tm, _ := fv.Interface().(time.Time) // the case's condition holds
		if y := tm.Year(); y < 0 || y > 9999 {
			return "", fmt.Errorf("allisall: query parameter %s: year %d is outside 0 to 9999", name, y)
		}
		return tm.Format(time.RFC3339Nano), nil
	case fv.Kind() == reflect.String:
		return fv.String(), nil
	case fv.CanInt():
		return strconv.FormatInt(fv.Int(), 10), nil
	default:
		return "", fmt.Errorf("allisall: query parameter %s: a %s cannot be sent", name, fv.Type())
	}
}

// decodeResponse decodes a successful response's body into out, a pointer
// to the operation's response type.
//
// A body that is not a JSON object, or not JSON at all, is an [*APIError]
// with the response's status and no problem: the response is not the shape
// the operation documents. A member whose value does not decode into its
// Go type, such as a string where a number is declared, is a decodeError
// naming the member. A member the type does not know is ignored, and a
// missing one keeps its zero value.
func decodeResponse(resp *response, out any) error {
	if !isJSONObject(resp.body) || !json.Valid(resp.body) {
		return shapeError(resp)
	}
	if err := json.Unmarshal(resp.body, out); err != nil {
		pointer, cause := locate(resp.body, reflect.TypeOf(out).Elem(), "")
		if cause == nil {
			// The walk found no member at fault, so report the decoder's
			// own error, at the body.
			cause = err
		}
		return &decodeError{pointer: pointer, requestID: resp.header.Get("X-Request-Id"), err: cause}
	}
	return nil
}

// decodeError is a successful response with a member that does not decode
// into its Go type. It matches [ErrAllisall].
type decodeError struct {
	// pointer is the member's RFC 6901 JSON Pointer, "" for the whole body.
	pointer string
	// requestID is the response's X-Request-Id, to quote when reporting it.
	requestID string
	err       error
}

func (e *decodeError) Error() string {
	where := "the response body"
	if e.pointer != "" {
		where = "the response member " + e.pointer
	}
	msg := fmt.Sprintf("allisall: %s does not decode: %v", where, e.err)
	if e.requestID != "" {
		msg += fmt.Sprintf(" (request id %s)", e.requestID)
	}
	return msg
}

func (e *decodeError) Unwrap() error { return e.err }

// Is reports whether target is [ErrAllisall].
func (e *decodeError) Is(target error) bool { return target == ErrAllisall }

// locate finds the first value in data, a JSON value already known to be
// valid, that does not decode into a value of type t. It returns that
// value's JSON Pointer, under prefix, and its error, or a nil error when
// every value decodes.
//
// It walks the JSON and the Go type together, as encoding/json does: a
// struct's members by their json tag names, matched exactly and then
// without regard to case; a slice's items; a map's values. Every other
// value, and any type with its own UnmarshalJSON, is decoded alone.
func locate(data []byte, t reflect.Type, prefix string) (string, error) {
	for t.Kind() == reflect.Pointer {
		if string(data) == "null" {
			return "", nil
		}
		t = t.Elem()
	}
	if reflect.PointerTo(t).Implements(reflect.TypeFor[json.Unmarshaler]()) {
		return decodeAlone(data, t, prefix)
	}
	switch t.Kind() {
	case reflect.Struct:
		if string(data) == "null" {
			return "", nil
		}
		members, err := objectMembers(data)
		if err != nil {
			return decodeAlone(data, t, prefix)
		}
		fields := structFields(t)
		// In the body's order, as encoding/json decodes them: every key
		// that matches a field is decoded into it, so a later key of the
		// same field may be the one at fault.
		for _, m := range members {
			typ, ok := fieldFor(fields, m.key)
			if !ok {
				continue
			}
			if pointer, err := locate(m.raw, typ, prefix+"/"+escapePointer(m.key)); err != nil {
				return pointer, err
			}
		}
		return "", nil
	case reflect.Slice, reflect.Array:
		var items []json.RawMessage
		if err := json.Unmarshal(data, &items); err != nil {
			return decodeAlone(data, t, prefix)
		}
		for i, raw := range items {
			if pointer, err := locate(raw, t.Elem(), prefix+"/"+strconv.Itoa(i)); err != nil {
				return pointer, err
			}
		}
		return "", nil
	case reflect.Map:
		var entries map[string]json.RawMessage
		if t.Key().Kind() != reflect.String {
			return decodeAlone(data, t, prefix)
		}
		if err := json.Unmarshal(data, &entries); err != nil {
			return decodeAlone(data, t, prefix)
		}
		// Sorted, so the same body always reports the same member.
		for _, key := range slices.Sorted(maps.Keys(entries)) {
			if pointer, err := locate(entries[key], t.Elem(), prefix+"/"+escapePointer(key)); err != nil {
				return pointer, err
			}
		}
		return "", nil
	}
	return decodeAlone(data, t, prefix)
}

// decodeAlone decodes data into a new value of type t, and reports its
// error at prefix.
func decodeAlone(data []byte, t reflect.Type, prefix string) (string, error) {
	if err := json.Unmarshal(data, reflect.New(t).Interface()); err != nil {
		return prefix, err
	}
	return "", nil
}

// jsonName returns the member name encoding/json gives a struct field, and
// false for a field it skips. An embedded struct's own fields are listed by
// reflect.VisibleFields, so the embedded field itself is skipped.
func jsonName(f reflect.StructField) (string, bool) {
	if !f.IsExported() || f.Anonymous {
		return "", false
	}
	tag := f.Tag.Get("json")
	if tag == "-" {
		return "", false
	}
	name, _, _ := strings.Cut(tag, ",")
	if name == "" {
		name = f.Name
	}
	return name, true
}

// objectMember is one member of a JSON object.
type objectMember struct {
	key string
	raw json.RawMessage
}

// objectMembers returns a JSON object's members in the body's order,
// repeated keys included.
func objectMembers(data []byte) ([]objectMember, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if tok != json.Delim('{') {
		return nil, errors.New("not a JSON object")
	}
	var out []objectMember
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, ok := tok.(string)
		if !ok {
			return nil, errors.New("a member name is not a string")
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, err
		}
		out = append(out, objectMember{key: key, raw: raw})
	}
	return out, nil
}

// structField is a field encoding/json decodes into: its member name and
// its type.
type structField struct {
	name string
	typ  reflect.Type
}

// structFields returns the fields of t that encoding/json decodes into, in
// field order.
func structFields(t reflect.Type) []structField {
	var out []structField
	for _, f := range reflect.VisibleFields(t) {
		if name, ok := jsonName(f); ok {
			out = append(out, structField{name: name, typ: f.Type})
		}
	}
	return out
}

// fieldFor returns the type of the field a member's key decodes into, as
// encoding/json matches it: the field whose name equals the key, and
// otherwise the first field whose name equals it without regard to case.
func fieldFor(fields []structField, key string) (reflect.Type, bool) {
	for _, f := range fields {
		if f.name == key {
			return f.typ, true
		}
	}
	for _, f := range fields {
		if strings.EqualFold(f.name, key) {
			return f.typ, true
		}
	}
	return nil, false
}

// escapePointer escapes one JSON Pointer reference token (RFC 6901).
func escapePointer(token string) string {
	return strings.ReplaceAll(strings.ReplaceAll(token, "~", "~0"), "/", "~1")
}
