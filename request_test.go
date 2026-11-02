package allisall

import (
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

func okResponse(body string) *response {
	return &response{status: 200, header: http.Header{"X-Request-Id": {"req-1"}}, body: []byte(body)}
}

const costLineItem = `{"id":"6f1c2d3e-4b5a-4c6d-8e7f-90a1b2c3d4e5","label":"Hosting",` +
	`"monthly_amount":0.1234567890123456789,"currency":"USD","start_date":"2026-01-01","end_date":null,` +
	`"target":{"kind":"client","id":"6f1c2d3e-4b5a-4c6d-8e7f-90a1b2c3d4e6"},` +
	`"created_at":"2026-08-01T09:30:00.123456Z","updated_at":"2026-08-01T09:30:00Z"}`

func TestDecodeResponseReadsMoneyExactly(t *testing.T) {
	var item CostLineItem
	if err := decodeResponse(okResponse(costLineItem), &item); err != nil {
		t.Fatal(err)
	}
	if item.MonthlyAmount.String() != "0.1234567890123456789" {
		t.Errorf("monthly_amount = %s, want 0.1234567890123456789", item.MonthlyAmount)
	}
	if item.EndDate != nil {
		t.Errorf("end_date = %v, want nil for null", *item.EndDate)
	}
	if want := time.Date(2026, 8, 1, 9, 30, 0, 123456000, time.UTC); !item.CreatedAt.Equal(want) {
		t.Errorf("created_at = %v, want %v", item.CreatedAt, want)
	}
}

func TestDecodeResponseKeepsWhatItDoesNotKnow(t *testing.T) {
	body := `{"id":"c1","name":"Acme","status":"frozen","client_key":null,"renewal_date":null,` +
		`"notes":"","created_at":"2026-08-01T09:30:00Z","added_later":{"a":[1,2]}}`
	var c ClientRecord
	if err := decodeResponse(okResponse(body), &c); err != nil {
		t.Fatalf("an unknown member or enum value failed the decoding: %v", err)
	}
	if c.Status != "frozen" || c.Status.Valid() {
		t.Errorf("status = %q, valid %v; want the unknown value kept, and not valid", c.Status, c.Status.Valid())
	}
	if !ClientStatusActive.Valid() {
		t.Error("ClientStatusActive is not valid")
	}
	if c.Name != "Acme" {
		t.Errorf("name = %q, want Acme", c.Name)
	}
}

func TestDecodeResponseNamesAMemberOfTheWrongType(t *testing.T) {
	tests := []struct {
		name, body, pointer string
		into                any
	}{
		{
			"a string at a Money member",
			strings.Replace(costLineItem, `0.1234567890123456789`, `"12.50"`, 1),
			"/monthly_amount", new(CostLineItem),
		},
		{
			"an exponent beyond the bound at a Money member",
			strings.Replace(costLineItem, `0.1234567890123456789`, `1e401`, 1),
			"/monthly_amount", new(CostLineItem),
		},
		{
			"a string where an integer is declared",
			`{"client_id":"c1","currency":"USD","data":[],"has_more":false,"next_cursor":null,"page":null,` +
				`"period":{"from":"2026-08-01T00:00:00Z","to":"2026-09-01T00:00:00Z"},"total":"12"}`,
			"/total", new(ClientEventList),
		},
		{
			"a malformed instant",
			strings.Replace(costLineItem, `"2026-08-01T09:30:00.123456Z"`, `"yesterday"`, 1),
			"/created_at", new(CostLineItem),
		},
		{
			"a money member inside a list item",
			`{"data":[` + costLineItem + `,` + strings.Replace(costLineItem, `0.1234567890123456789`, `true`, 1) +
				`],"has_more":false,"next_cursor":null}`,
			"/data/1/monthly_amount", new(CostLineItemList),
		},
		{
			"an object where a list is declared",
			`{"data":{},"has_more":false,"next_cursor":null}`,
			"/data", new(CostLineItemList),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := decodeResponse(okResponse(tt.body), tt.into)
			if err == nil {
				t.Fatal("decoded, want an error")
			}
			if !errors.Is(err, ErrAllisall) {
				t.Errorf("%v does not match ErrAllisall", err)
			}
			var apiErr *APIError
			var connErr *ConnectionError
			var ingestErr *IngestError
			if errors.As(err, &apiErr) || errors.As(err, &connErr) || errors.As(err, &ingestErr) {
				t.Errorf("%T is one of the three error types, want none of them", err)
			}
			if !strings.Contains(err.Error(), "member "+tt.pointer+" ") {
				t.Errorf("%q does not name the member %s", err, tt.pointer)
			}
			var decErr *decodeError
			if !errors.As(err, &decErr) || decErr.requestID != "req-1" {
				t.Errorf("%v does not carry the response's request id req-1", err)
			}
			if !strings.HasSuffix(err.Error(), " (request id req-1)") {
				t.Errorf("%q does not quote the request id", err)
			}
		})
	}
}

func TestADecodeErrorWithoutARequestID(t *testing.T) {
	var c ClientEventList
	err := decodeResponse(&response{status: 200, header: http.Header{}, body: []byte(`{"total":"12"}`)}, &c)
	if err == nil || strings.Contains(err.Error(), "request id") {
		t.Errorf("err = %v, want one that quotes no request id", err)
	}
}

func TestDecodeResponseRefusesABodyThatIsNotAnObject(t *testing.T) {
	for _, body := range []string{`[]`, `null`, `"text"`, `not json`, ``, `{"a":1} trailing`, `{"a":`} {
		t.Run(body, func(t *testing.T) {
			var c ClientRecord
			err := decodeResponse(&response{status: 201, header: http.Header{"X-Request-Id": {"req-9"}}, body: []byte(body)}, &c)
			var apiErr *APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("err = %v, want an *APIError", err)
			}
			if apiErr.Status != 201 || apiErr.Problem != nil || apiErr.Code != "" || apiErr.RequestID != "req-9" {
				t.Errorf("APIError = %+v, want status 201, no problem, and the response's request id", apiErr)
			}
		})
	}
}

func TestLocateFollowsEncodingJSON(t *testing.T) {
	type inner struct {
		N int `json:"n"`
	}
	type embedded struct {
		E int `json:"e"`
	}
	type outer struct {
		embedded
		Items  []inner           `json:"items"`
		ByKey  map[string]inner  `json:"by_key"`
		Upper  int               `json:"upper"`
		Opt    *inner            `json:"opt"`
		Spaced map[string]string `json:"a/b~c"`
	}
	tests := []struct{ body, pointer string }{
		{`{"items":[{"n":1},{"n":"x"}]}`, "/items/1/n"},
		{`{"by_key":{"b":{"n":1},"a/z":{"n":"x"}}}`, "/by_key/a~1z/n"},
		{`{"by_key":{"z":{"n":"x"},"a":{"n":"y"}}}`, "/by_key/a/n"},
		{`{"UPPER":"x"}`, "/UPPER"},
		{`{"e":"x"}`, "/e"},
		{`{"opt":null,"items":null}`, ""},
		{`{"a/b~c":{"k":1}}`, "/a~1b~0c/k"},
	}
	for _, tt := range tests {
		var v outer
		pointer, err := locate([]byte(tt.body), reflect.TypeOf(v), "")
		if pointer != tt.pointer || (tt.pointer != "") != (err != nil) {
			t.Errorf("locate(%s) = %q, %v; want %q", tt.body, pointer, err, tt.pointer)
		}
	}
}

// TestLocateWithFoldEqualKeys pins which member is reported when several
// keys match one field without regard to case. encoding/json decodes every
// such key into the field, in the body's order, so the member at fault is
// the one whose value does not decode, wherever it sits.
func TestLocateWithFoldEqualKeys(t *testing.T) {
	type total struct {
		Total int `json:"total"`
	}
	type exact struct {
		Lower int    `json:"a"`
		Upper string `json:"A"`
	}
	tests := []struct {
		body, pointer string
		typ           reflect.Type
	}{
		{`{"Total":"x","TOTAL":1}`, "/Total", reflect.TypeFor[total]()},
		{`{"TOTAL":1,"Total":"x"}`, "/Total", reflect.TypeFor[total]()},
		{`{"total":1,"TOTAL":"x"}`, "/TOTAL", reflect.TypeFor[total]()},
		// "A" is a field's exact name, so it never folds onto "a".
		{`{"A":"text","a":1}`, "", reflect.TypeFor[exact]()},
		{`{"a":1,"A":"text","a":"x"}`, "/a", reflect.TypeFor[exact]()},
	}
	for _, tt := range tests {
		// The fixture must be one encoding/json itself refuses, or accepts.
		want := json.Unmarshal([]byte(tt.body), reflect.New(tt.typ).Interface())
		if (want != nil) != (tt.pointer != "") {
			t.Fatalf("encoding/json gives %v for %s; the fixture is wrong", want, tt.body)
		}
		pointer, err := locate([]byte(tt.body), tt.typ, "")
		if pointer != tt.pointer || (err != nil) != (tt.pointer != "") {
			t.Errorf("locate(%s) = %q, %v; want %q", tt.body, pointer, err, tt.pointer)
		}
	}
}
