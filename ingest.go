package allisall

import (
	"bytes"
	"context"
	"encoding"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	// maxBatchEvents is the most events the API takes in one batch.
	maxBatchEvents = 500
	// maxBodyBytes is the API's limit on an ingest body, inclusive.
	maxBodyBytes = 5_000_000
	// envelopeBytes is the length of `{"events":[` plus `]}`.
	envelopeBytes = 13
	// maxEventBytes is the largest event that fits in a batch alone.
	maxEventBytes = maxBodyBytes - envelopeBytes
)

// EventsService groups the operations on events. Reach it as
// client.Events.
type EventsService struct {
	client *Client
}

// batch is a run of events sent in one request.
type batch struct {
	// positions holds each event's index in the caller's list.
	positions []int
}

// Ingest sends events to the API, in as many batches as its limits
// require, and returns their combined outcome.
//
// An empty list sends nothing and returns a summary of zeros. Every event
// without an EventID is given a random one first, so a retried batch
// never stores an event twice; the caller's slice is not modified.
// Batches hold at most 500 events and 5,000,000 bytes of body, and are
// sent one after another, in order, each with the client's retries. An
// event too large to send in any batch is not sent: it is reported in the
// summary as a rejected row with code too_large.
//
// A partially accepted batch is a success: its refused events are in the
// summary's Errors. When a batch fails, Ingest sends nothing after it and
// returns an [*IngestError] holding the summary so far and the events not
// yet stored. An error from the client's token provider fails the batch
// the same way: it is the IngestError's cause, which errors.Is and
// errors.As see through. An event that cannot be encoded as JSON, such
// as one with a NaN Quantity or a string that is not valid UTF-8, ends
// the call before any request is sent, with an error that does not match
// [ErrAllisall].
func (s *EventsService) Ingest(ctx context.Context, events []EventInput) (*IngestSummary, error) {
	summary := &IngestSummary{Errors: []IngestRowError{}}
	if len(events) == 0 {
		return summary, nil
	}

	prepared := make([]EventInput, len(events))
	copy(prepared, events)
	for i := range prepared {
		if prepared[i].EventID == "" {
			prepared[i].EventID = newUUID()
		}
	}

	encoded := make([][]byte, len(prepared))
	for i := range prepared {
		b, err := encodeEvent(&prepared[i])
		if err != nil {
			return nil, fmt.Errorf("allisall: event %d: %w", i, err)
		}
		encoded[i] = b
	}

	batches, tooLarge := split(encoded)
	for _, i := range tooLarge {
		summary.Rejected++
		summary.Errors = append(summary.Errors, IngestRowError{
			Index:   i,
			Code:    FieldErrorCodeTooLarge,
			Pointer: "/events/" + strconv.Itoa(i),
			Detail: fmt.Sprintf("The event is %d bytes as JSON. The SDK sends at most %d bytes in one request, "+
				"so the largest event it can send is %d bytes. The event was not sent.",
				len(encoded[i]), maxBodyBytes, maxEventBytes),
		})
	}

	for n, b := range batches {
		result, err := s.sendBatch(ctx, b, encoded)
		if err != nil {
			// A token provider's error fails the batch like any other, with
			// the provider's error itself as the cause.
			var pe *providerError
			if errors.As(err, &pe) {
				err = pe.err
			}
			sortRowErrors(summary.Errors)
			return nil, &IngestError{
				Summary:       summary,
				Remaining:     remaining(prepared, batches[n:]),
				RemainingFrom: b.positions[0],
				Err:           err,
			}
		}
		summary.Batches++
		summary.Accepted += *result.Accepted
		summary.Duplicates += *result.Duplicates
		summary.Rejected += *result.Rejected
		summary.ErrorsTruncated = summary.ErrorsTruncated || *result.ErrorsTruncated
		for _, e := range result.Errors {
			summary.Errors = append(summary.Errors, rebase(e, b.positions))
		}
	}
	sortRowErrors(summary.Errors)
	return summary, nil
}

// sendBatch sends one batch and decodes its result.
func (s *EventsService) sendBatch(ctx context.Context, b batch, encoded [][]byte) (*ingestResult, error) {
	size := envelopeBytes + len(b.positions) - 1
	for _, i := range b.positions {
		size += len(encoded[i])
	}
	body := make([]byte, 0, size)
	body = append(body, `{"events":[`...)
	for n, i := range b.positions {
		if n > 0 {
			body = append(body, ',')
		}
		body = append(body, encoded[i]...)
	}
	body = append(body, `]}`...)

	resp, err := s.client.send(ctx, http.MethodPost, "/v1/events", body)
	if err != nil {
		return nil, err
	}
	var result ingestResult
	if err := json.Unmarshal(resp.body, &result); err != nil ||
		result.Accepted == nil || result.Duplicates == nil || result.Rejected == nil ||
		result.Errors == nil || result.ErrorsTruncated == nil || !indicesInBatch(result.Errors, len(b.positions)) {
		return nil, &APIError{
			Status:    resp.status,
			RequestID: resp.header.Get("X-Request-Id"),
			Headers:   resp.header,
		}
	}
	return &result, nil
}

// indicesInBatch reports whether every row error names a row of a batch
// of n events. One that does not cannot be mapped to the caller's list,
// so the response is not the shape the API documents.
func indicesInBatch(errs []IngestRowError, n int) bool {
	for _, e := range errs {
		if e.Index < 0 || e.Index >= n {
			return false
		}
	}
	return true
}

// split packs the encoded events into batches in input order. A batch is
// closed when it holds 500 events, or before an event that would take its
// body over 5,000,000 bytes. An event over maxEventBytes is in no batch:
// its index is returned in tooLarge.
func split(encoded [][]byte) (batches []batch, tooLarge []int) {
	var cur batch
	size := 0
	for i, e := range encoded {
		if len(e) > maxEventBytes {
			tooLarge = append(tooLarge, i)
			continue
		}
		if len(cur.positions) > 0 && (len(cur.positions) == maxBatchEvents || size+1+len(e) > maxBodyBytes) {
			batches = append(batches, cur)
			cur = batch{}
		}
		if len(cur.positions) == 0 {
			size = envelopeBytes + len(e)
		} else {
			size += 1 + len(e)
		}
		cur.positions = append(cur.positions, i)
	}
	if len(cur.positions) > 0 {
		batches = append(batches, cur)
	}
	return batches, tooLarge
}

// rebase rewrites a batch's row error to the caller's list: its index,
// and the index segment of its pointer. sendBatch has checked that the
// index is within the batch.
func rebase(e IngestRowError, positions []int) IngestRowError {
	to := positions[e.Index]
	prefix := "/events/" + strconv.Itoa(e.Index)
	if rest, ok := strings.CutPrefix(e.Pointer, prefix); ok && (rest == "" || rest[0] == '/') {
		e.Pointer = "/events/" + strconv.Itoa(to) + rest
	}
	e.Index = to
	return e
}

// remaining returns the events of the given batches, in order.
func remaining(prepared []EventInput, batches []batch) []EventInput {
	var out []EventInput
	for _, b := range batches {
		for _, i := range b.positions {
			out = append(out, prepared[i])
		}
	}
	return out
}

// sortRowErrors orders row errors by index, keeping each row's entries in
// the order the API gave them.
func sortRowErrors(errs []IngestRowError) {
	slices.SortStableFunc(errs, func(a, b IngestRowError) int { return a.Index - b.Index })
}

// encodeEvent returns the event's compact JSON encoding, refusing a value
// the API could not receive as the caller meant it.
func encodeEvent(e *EventInput) ([]byte, error) {
	if math.IsNaN(e.Quantity) || math.IsInf(e.Quantity, 0) {
		return nil, fmt.Errorf("quantity %v is not a finite number", e.Quantity)
	}
	b, err := json.Marshal(e)
	if err != nil {
		return nil, err
	}
	// encoding/json replaces invalid UTF-8 in a string with U+FFFD rather
	// than failing, so the strings are checked before trusting the bytes.
	// Marshal has already refused a cyclic value, so the walk ends.
	if path, ok := invalidUTF8(reflect.ValueOf(e).Elem(), ""); ok {
		return nil, fmt.Errorf("%s is not valid UTF-8", strings.TrimPrefix(path, "."))
	}
	if !utf8.Valid(b) {
		return nil, errors.New("the event's JSON is not valid UTF-8")
	}
	return bytes.Clone(b), nil
}

var (
	jsonMarshalerType = reflect.TypeFor[json.Marshaler]()
	textMarshalerType = reflect.TypeFor[encoding.TextMarshaler]()
)

// invalidUTF8 finds a string that is not valid UTF-8 in the parts of v
// that encoding/json writes, and returns where it is.
func invalidUTF8(v reflect.Value, path string) (string, bool) {
	if !v.IsValid() {
		return "", false
	}
	if v.Kind() != reflect.Interface && v.Kind() != reflect.Pointer &&
		(v.Type().Implements(jsonMarshalerType) || v.Type().Implements(textMarshalerType)) {
		return "", false
	}
	switch v.Kind() {
	case reflect.String:
		if !utf8.ValidString(v.String()) {
			return path, true
		}
	case reflect.Interface, reflect.Pointer:
		if !v.IsNil() {
			return invalidUTF8(v.Elem(), path)
		}
	case reflect.Map:
		iter := v.MapRange()
		for iter.Next() {
			k := iter.Key()
			if k.Kind() == reflect.String && !utf8.ValidString(k.String()) {
				return path + " key", true
			}
			if p, ok := invalidUTF8(iter.Value(), path+"."+fmt.Sprint(k.Interface())); ok {
				return p, true
			}
		}
	case reflect.Slice, reflect.Array:
		if v.Type().Elem().Kind() == reflect.Uint8 {
			return "", false // encoded as base64
		}
		for i := 0; i < v.Len(); i++ {
			if p, ok := invalidUTF8(v.Index(i), path+"["+strconv.Itoa(i)+"]"); ok {
				return p, true
			}
		}
	case reflect.Struct:
		t := v.Type()
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if !f.IsExported() || f.Tag.Get("json") == "-" {
				continue
			}
			name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
			if name == "" {
				name = f.Name
			}
			if p, ok := invalidUTF8(v.Field(i), path+"."+name); ok {
				return p, true
			}
		}
	}
	return "", false
}
