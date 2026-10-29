package allisall

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// ErrAllisall is the base of every SDK error. errors.Is(err, ErrAllisall)
// holds for an [*APIError], a [*ConnectionError], an [*IngestError], and
// an error from [NewClient].
var ErrAllisall = errors.New("allisall")

// APIError is an error response from the API, or a successful response
// whose body is not the shape the operation documents.
//
// Branch on Code. For a code you do not recognise, fall back on Status.
type APIError struct {
	// Status is the HTTP status.
	Status int
	// Code is the problem's code. It is empty when the body was not a
	// problem document.
	Code ErrorCode
	// Problem is the whole problem document, or nil when the body was not
	// one.
	Problem *Problem
	// RequestID is the problem's request_id or, when there is no problem,
	// the response's X-Request-Id.
	RequestID string
	// RetryAfter is the wait the response's Retry-After asked for,
	// measured from when the response arrived. It is zero when the
	// response carried none, or one that does not parse or names a time
	// in the past. A Retry-After of 0 also reads as zero; the API itself
	// never sends less than 1.
	RetryAfter time.Duration
	// Headers are the response headers.
	Headers http.Header

	hasRetryAfter bool
}

// Error describes the error.
func (e *APIError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "allisall: API error: status %d", e.Status)
	if e.Code != "" {
		fmt.Fprintf(&b, ", code %s", e.Code)
	}
	if e.Problem != nil && e.Problem.Detail != "" {
		fmt.Fprintf(&b, ": %s", e.Problem.Detail)
	}
	if e.RequestID != "" {
		fmt.Fprintf(&b, " (request id %s)", e.RequestID)
	}
	return b.String()
}

// Is reports whether target is [ErrAllisall].
func (e *APIError) Is(target error) bool { return target == ErrAllisall }

// ConnectionError is a request that got no response: the connection
// failed or was reset, or the attempt timed out.
type ConnectionError struct {
	// RequestID is the X-Request-Id the SDK sent on the last attempt, to
	// quote even though no response arrived.
	RequestID string
	// Err is the underlying cause.
	Err error
}

// Error describes the error.
func (e *ConnectionError) Error() string {
	return fmt.Sprintf("allisall: connection error (request id %s): %v", e.RequestID, e.Err)
}

// Unwrap returns the underlying cause.
func (e *ConnectionError) Unwrap() error { return e.Err }

// Is reports whether target is [ErrAllisall].
func (e *ConnectionError) Is(target error) bool { return target == ErrAllisall }

// IngestError is a batch that failed inside [EventsService.Ingest]. The
// batches before it succeeded; it and every batch after it were not
// stored by this call, except that the failed batch may have been stored
// if its response was lost.
//
// To recover, send Remaining again: each event in it carries the event_id
// it was first sent with, so any the failed batch stored come back as
// duplicates.
type IngestError struct {
	// Summary is the combined outcome of the batches that succeeded, and
	// of every event too large to send.
	Summary *IngestSummary
	// Remaining holds copies of every event from the failed batch onward,
	// each with the event_id it was given. It never holds an event too
	// large to send.
	Remaining []EventInput
	// RemainingFrom is the index, in the caller's list, of the first event
	// in Remaining.
	RemainingFrom int
	// Err is the cause: an [*APIError], a [*ConnectionError], the
	// context's error when the caller cancelled, or the error the token
	// provider returned, unchanged.
	Err error
}

// Error describes the error.
func (e *IngestError) Error() string {
	return fmt.Sprintf("allisall: ingest stopped at event %d: %v", e.RemainingFrom, e.Err)
}

// Unwrap returns the cause.
func (e *IngestError) Unwrap() error { return e.Err }

// Is reports whether target is [ErrAllisall].
func (e *IngestError) Is(target error) bool { return target == ErrAllisall }
