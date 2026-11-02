package allisall

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func ingestOnce(t *testing.T, resp *http.Response) error {
	t.Helper()
	h := newHarness(t, func(int, *http.Request, recorded) (*http.Response, error) { return resp, nil }, WithMaxRetries(0))
	_, err := h.client.Events.Ingest(context.Background(), smallEvents(1))
	if err == nil {
		t.Fatal("Ingest succeeded, want an error")
	}
	return err
}

func TestAPIErrorFromAProblem(t *testing.T) {
	body := `{"type":"https://api.allisai.ai/errors/validation_failed","title":"The request is invalid","status":400,` +
		`"detail":"1 problem","code":"validation_failed","request_id":"req-abc","future_member":true,` +
		`"errors":[{"code":"invalid_value","pointer":"/events","detail":"d"},{"code":"brand_new_field_code","header":"X-Thing","detail":"e"}]}`
	err := ingestOnce(t, newResponse(400, "application/problem+json; charset=utf-8", body, "X-Request-Id", "req-abc"))

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %v, want an APIError", err)
	}
	want := &Problem{
		Type:      "https://api.allisai.ai/errors/validation_failed",
		Title:     "The request is invalid",
		Status:    400,
		Detail:    "1 problem",
		Code:      ErrorCodeValidationFailed,
		RequestID: "req-abc",
		Errors: []FieldError{
			{Code: FieldErrorCodeInvalidValue, Pointer: "/events", Detail: "d"},
			{Code: "brand_new_field_code", Header: "X-Thing", Detail: "e"},
		},
	}
	if !reflect.DeepEqual(apiErr.Problem, want) {
		t.Errorf("Problem = %+v, want %+v", apiErr.Problem, want)
	}
	if apiErr.Status != 400 || apiErr.Code != ErrorCodeValidationFailed || apiErr.RequestID != "req-abc" {
		t.Errorf("APIError = %+v", apiErr)
	}
	if apiErr.Headers.Get("X-Request-Id") != "req-abc" {
		t.Errorf("Headers not carried")
	}
}

func TestAPIErrorKeepsAnUnknownCode(t *testing.T) {
	err := ingestOnce(t, problemResponse(409, "a_code_from_the_future", "r-9"))
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code != "a_code_from_the_future" || apiErr.Status != 409 {
		t.Fatalf("err = %v, want the unknown code kept with status 409", err)
	}
}

func TestAPIErrorFromANonProblemBody(t *testing.T) {
	tests := []struct {
		name string
		resp *http.Response
	}{
		{"an HTML page", newResponse(502, "text/html", "<html>Bad gateway</html>", "X-Request-Id", "proxy-1")},
		{"JSON that is not problem+json", newResponse(400, "application/json", `{"code":"invalid_json"}`, "X-Request-Id", "proxy-1")},
		{"problem+json that does not parse", newResponse(400, "application/problem+json", `<html>`, "X-Request-Id", "proxy-1")},
		{"problem+json that is not an object", newResponse(400, "application/problem+json", `null`, "X-Request-Id", "proxy-1")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var apiErr *APIError
			if err := ingestOnce(t, tt.resp); !errors.As(err, &apiErr) {
				t.Fatalf("err = %v, want an APIError", err)
			}
			if apiErr.Status != tt.resp.StatusCode || apiErr.Code != "" || apiErr.Problem != nil || apiErr.RequestID != "proxy-1" {
				t.Errorf("APIError = %+v, want a status, no code, no problem, the header's request id", apiErr)
			}
		})
	}
}

// A proxy in front of the API may answer 413 with a body that is not a
// problem document. It is an APIError with
// status 413 and no code, and the default retries do not repeat it.
func TestAPIErrorFromAProxy413(t *testing.T) {
	tests := []struct {
		name, contentType, body string
	}{
		{"plain text", "text/plain; charset=utf-8", "Request Entity Too Large"},
		{"JSON of another shape", "application/json", `{"error":"too large"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t, func(int, *http.Request, recorded) (*http.Response, error) {
				return newResponse(413, tt.contentType, tt.body), nil
			})
			_, err := h.client.Events.Ingest(context.Background(), smallEvents(1))
			var ingestErr *IngestError
			if !errors.As(err, &ingestErr) {
				t.Fatalf("err = %v, want an *IngestError", err)
			}
			var apiErr *APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("err = %v, want an APIError", err)
			}
			if apiErr.Status != 413 || apiErr.Code != "" || apiErr.Problem != nil || apiErr.RequestID != "" {
				t.Errorf("APIError = %+v, want status 413, no code, no problem, no request id", apiErr)
			}
			if got, want := apiErr.Error(), "allisall: API error: status 413"; got != want {
				t.Errorf("Error() = %q, want %q", got, want)
			}
			if h.transport.count() != 1 || len(h.recordedWaits()) != 0 {
				t.Errorf("sent %d requests after %d waits, want 1 and none", h.transport.count(), len(h.recordedWaits()))
			}
			if ingestErr.RemainingFrom != 0 || len(ingestErr.Remaining) != 1 {
				t.Errorf("remaining from %d holds %d events, want 0 and 1", ingestErr.RemainingFrom, len(ingestErr.Remaining))
			}
		})
	}
}

func TestEveryErrorTypeMatchesErrAllisall(t *testing.T) {
	cause := errors.New("cause")
	tests := []struct {
		name string
		err  error
	}{
		{"APIError", &APIError{Status: 400}},
		{"ConnectionError", &ConnectionError{RequestID: "r", Err: cause}},
		{"IngestError", &IngestError{Summary: &IngestSummary{}, Err: &APIError{Status: 401, Code: ErrorCodeUnauthenticated}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !errors.Is(tt.err, ErrAllisall) {
				t.Errorf("errors.Is(%T, ErrAllisall) = false", tt.err)
			}
			if !strings.HasPrefix(tt.err.Error(), "allisall: ") {
				t.Errorf("Error() = %q", tt.err.Error())
			}
		})
	}
	if !errors.Is(&ConnectionError{Err: cause}, cause) {
		t.Errorf("a ConnectionError does not unwrap to its cause")
	}
	var apiErr *APIError
	if !errors.As(error(&IngestError{Err: &APIError{Status: 401}}), &apiErr) || apiErr.Status != 401 {
		t.Errorf("an IngestError does not unwrap to its APIError")
	}
	if errors.Is(errors.New("other"), ErrAllisall) {
		t.Errorf("an unrelated error matches ErrAllisall")
	}
}
