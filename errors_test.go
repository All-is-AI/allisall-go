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
