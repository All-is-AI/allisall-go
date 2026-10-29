package allisall

import "time"

// EventCategory is an event's category.
type EventCategory string

// The event categories the API accepts. Any other value is refused by the
// API as a row error, invalid_value.
const (
	EventCategorySession   EventCategory = "session"
	EventCategoryTask      EventCategory = "task"
	EventCategoryModel     EventCategory = "model"
	EventCategoryOutcome   EventCategory = "outcome"
	EventCategoryException EventCategory = "exception"
)

// EventInput is one event to ingest.
//
// An empty optional string means absent, and is not sent. When EventID is
// empty, [EventsService.Ingest] generates one before the first attempt, so
// a retry never stores the event twice. A caller whose own delivery is
// at-least-once, such as a queue that may redeliver a message, should set
// EventID itself, for example to the message's id: an id the SDK
// generates does not survive the call.
type EventInput struct {
	// EventID deduplicates the event within the workspace.
	EventID string `json:"event_id,omitempty"`
	// Timestamp is when the event happened. It is sent in RFC 3339 form
	// with its offset. The zero time is sent, and refused by the API.
	Timestamp time.Time `json:"timestamp"`
	// AgentKey names the agent that acted. An agent not seen before is
	// created.
	AgentKey string `json:"agent_key"`
	// ClientKey names the client the event belongs to.
	ClientKey string `json:"client_key,omitempty"`
	// Category is the event's category.
	Category EventCategory `json:"category"`
	// EventType is what the agent did.
	EventType string `json:"event_type"`
	// OutcomeType is required when Category is EventCategoryOutcome.
	OutcomeType string `json:"outcome_type,omitempty"`
	// Quantity is the event's quantity. Zero means 1. It must be finite.
	Quantity float64 `json:"quantity,omitempty"`
	// Cost is in Currency, or the workspace's currency. Nil means absent.
	Cost *Decimal `json:"cost,omitempty"`
	// TokensIn is the input token count. Nil means absent.
	TokensIn *int64 `json:"tokens_in,omitempty"`
	// TokensOut is the output token count. Nil means absent.
	TokensOut *int64 `json:"tokens_out,omitempty"`
	// Model is the model the agent invoked.
	Model string `json:"model,omitempty"`
	// Currency is the ISO 4217 code of Cost. Empty means the workspace's
	// currency.
	Currency string `json:"currency,omitempty"`
	// Metadata is free-form. Empty means absent.
	Metadata map[string]any `json:"metadata,omitempty"`
}

// IngestSummary is the combined outcome of one [EventsService.Ingest]
// call, over the caller's whole list of events.
//
// Accepted + Duplicates + Rejected equals the number of events passed.
type IngestSummary struct {
	// Accepted is the number of events stored.
	Accepted int
	// Duplicates is the number of events already stored, by event_id or
	// content hash. After a retry, an event the first attempt stored is
	// counted here.
	Duplicates int
	// Rejected is the number of events refused, each counted once.
	Rejected int
	// Errors holds one entry per problem, ascending by Index, which is the
	// event's position in the caller's list.
	Errors []IngestRowError
	// ErrorsTruncated reports whether any rejected event had more problems
	// than Errors holds.
	ErrorsTruncated bool
	// Batches is the number of batches sent.
	Batches int
}

// IngestRowError is one problem with a rejected event.
type IngestRowError struct {
	// Index is the event's zero-based position in the caller's list.
	Index int `json:"index"`
	// Code names the problem. It may be a code this SDK does not list.
	Code FieldErrorCode `json:"code"`
	// Pointer is an RFC 6901 JSON Pointer that starts /events/<Index>.
	Pointer string `json:"pointer"`
	// Detail is human text. Its wording may change at any time.
	Detail string `json:"detail"`
}

// ingestResult is the API's response to one batch.
type ingestResult struct {
	Accepted        *int             `json:"accepted"`
	Duplicates      *int             `json:"duplicates"`
	Rejected        *int             `json:"rejected"`
	Errors          []IngestRowError `json:"errors"`
	ErrorsTruncated *bool            `json:"errors_truncated"`
}

// Problem is an RFC 9457 problem document, the body of every error
// response from the API.
type Problem struct {
	Type   string `json:"type"`
	Title  string `json:"title"`
	Status int    `json:"status"`
	Detail string `json:"detail"`
	// Code names the error. It may be a code this SDK does not list; fall
	// back on Status for one you do not recognise.
	Code ErrorCode `json:"code"`
	// RequestID is the response's X-Request-Id, to quote when reporting
	// the problem.
	RequestID string `json:"request_id"`
	// Errors is present only with ErrorCodeValidationFailed.
	Errors []FieldError `json:"errors,omitempty"`
}

// FieldError is one problem in a request. It carries exactly one of
// Pointer, Parameter, Header and Part; with Part, it may also carry
// Pointer or Column.
type FieldError struct {
	// Code names the problem. It may be a code this SDK does not list.
	Code      FieldErrorCode `json:"code"`
	Pointer   string         `json:"pointer,omitempty"`
	Parameter string         `json:"parameter,omitempty"`
	Header    string         `json:"header,omitempty"`
	Part      string         `json:"part,omitempty"`
	Column    string         `json:"column,omitempty"`
	Detail    string         `json:"detail"`
}

// ErrorCode is the code of an error response. The set is open: the API
// may send a code this SDK does not list, and it is kept as it arrived.
type ErrorCode string

// The error codes the API sends.
const (
	ErrorCodeInvalidJSON                  ErrorCode = "invalid_json"
	ErrorCodeValidationFailed             ErrorCode = "validation_failed"
	ErrorCodeUnauthenticated              ErrorCode = "unauthenticated"
	ErrorCodeInsufficientScope            ErrorCode = "insufficient_scope"
	ErrorCodeSessionRequired              ErrorCode = "session_required"
	ErrorCodeAccountNotProvisioned        ErrorCode = "account_not_provisioned"
	ErrorCodeNotFound                     ErrorCode = "not_found"
	ErrorCodeNoActiveValueModel           ErrorCode = "no_active_value_model"
	ErrorCodeMethodNotAllowed             ErrorCode = "method_not_allowed"
	ErrorCodeIdempotencyKeyInUse          ErrorCode = "idempotency_key_in_use"
	ErrorCodeClientKeyInUse               ErrorCode = "client_key_in_use"
	ErrorCodeAgentKeyInUse                ErrorCode = "agent_key_in_use"
	ErrorCodeClientHasEvents              ErrorCode = "client_has_events"
	ErrorCodeClientHasCalculations        ErrorCode = "client_has_calculations"
	ErrorCodeAgentHasEvents               ErrorCode = "agent_has_events"
	ErrorCodeClientKeyHasEvents           ErrorCode = "client_key_has_events"
	ErrorCodeValueModelExists             ErrorCode = "value_model_exists"
	ErrorCodeValueModelVersionStale       ErrorCode = "value_model_version_stale"
	ErrorCodeCalculationTransitionInvalid ErrorCode = "calculation_transition_invalid"
	ErrorCodeCalculationNotDraft          ErrorCode = "calculation_not_draft"
	ErrorCodeCalculationNotPublished      ErrorCode = "calculation_not_published"
	ErrorCodeReportShared                 ErrorCode = "report_shared"
	ErrorCodeReportNotShared              ErrorCode = "report_not_shared"
	ErrorCodeAPIKeyRevoked                ErrorCode = "api_key_revoked"
	ErrorCodePayloadTooLarge              ErrorCode = "payload_too_large"
	ErrorCodeURITooLong                   ErrorCode = "uri_too_long"
	ErrorCodeUnsupportedMediaType         ErrorCode = "unsupported_media_type"
	ErrorCodeIdempotencyKeyReused         ErrorCode = "idempotency_key_reused"
	ErrorCodeRateLimited                  ErrorCode = "rate_limited"
	ErrorCodeInternalError                ErrorCode = "internal_error"
)

// FieldErrorCode is the code of one field-level problem, or of an ingest
// row error. The set is open: the API may send a code this SDK does not
// list, and it is kept as it arrived.
type FieldErrorCode string

// The field-level codes the API sends. The SDK itself reports
// FieldErrorCodeTooLarge for an event too large to send in any batch.
const (
	FieldErrorCodeRequired          FieldErrorCode = "required"
	FieldErrorCodeUnknownField      FieldErrorCode = "unknown_field"
	FieldErrorCodeImmutable         FieldErrorCode = "immutable"
	FieldErrorCodeInvalidType       FieldErrorCode = "invalid_type"
	FieldErrorCodeInvalidFormat     FieldErrorCode = "invalid_format"
	FieldErrorCodeInvalidValue      FieldErrorCode = "invalid_value"
	FieldErrorCodeTooLong           FieldErrorCode = "too_long"
	FieldErrorCodeTooMany           FieldErrorCode = "too_many"
	FieldErrorCodeOutOfRange        FieldErrorCode = "out_of_range"
	FieldErrorCodeTooPrecise        FieldErrorCode = "too_precise"
	FieldErrorCodeDuplicate         FieldErrorCode = "duplicate"
	FieldErrorCodeTooLarge          FieldErrorCode = "too_large"
	FieldErrorCodeMutuallyExclusive FieldErrorCode = "mutually_exclusive"
	FieldErrorCodeUnknownReference  FieldErrorCode = "unknown_reference"
	FieldErrorCodeClientKeyRequired FieldErrorCode = "client_key_required"
	FieldErrorCodeCurrencyMismatch  FieldErrorCode = "currency_mismatch"
)
