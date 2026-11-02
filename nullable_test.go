package allisall

import (
	"encoding/json"
	"testing"
)

func TestNullableMarshalsItsThreeStates(t *testing.T) {
	tests := []struct {
		name  string
		patch ClientPatch
		want  string
	}{
		{"absent", ClientPatch{}, `{}`},
		{"null", ClientPatch{Notes: Null[string]()}, `{"notes":null}`},
		{"value", ClientPatch{Notes: NewNullable("renews in March")}, `{"notes":"renews in March"}`},
		{"an empty value is a value", ClientPatch{Notes: NewNullable("")}, `{"notes":""}`},
		{
			"each member on its own",
			ClientPatch{ClientKey: NewNullable("acme"), Notes: Null[string](), Status: new(ClientStatusChurned)},
			`{"client_key":"acme","notes":null,"status":"churned"}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, err := json.Marshal(tt.patch)
			if err != nil {
				t.Fatal(err)
			}
			if string(b) != tt.want {
				t.Errorf("JSON = %s, want %s", b, tt.want)
			}
		})
	}
}

func TestNullableAccessors(t *testing.T) {
	tests := []struct {
		name           string
		n              Nullable[string]
		value          string
		set, null, abs bool
	}{
		{"absent", Nullable[string]{}, "", false, false, true},
		{"null", Null[string](), "", false, true, false},
		{"value", NewNullable("x"), "x", true, false, false},
		{"zero value", NewNullable(""), "", true, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, ok := tt.n.Get()
			if v != tt.value || ok != tt.set {
				t.Errorf("Get() = %q, %v; want %q, %v", v, ok, tt.value, tt.set)
			}
			if tt.n.IsNull() != tt.null {
				t.Errorf("IsNull() = %v, want %v", tt.n.IsNull(), tt.null)
			}
			if tt.n.IsZero() != tt.abs {
				t.Errorf("IsZero() = %v, want %v", tt.n.IsZero(), tt.abs)
			}
		})
	}
}

// TestAnAbsentNullableOutsideOmitzeroIsNull pins what an absent Nullable
// writes when no omitzero tag leaves it out.
func TestAnAbsentNullableOutsideOmitzeroIsNull(t *testing.T) {
	b, err := json.Marshal(struct {
		N Nullable[int] `json:"n"`
	}{})
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"n":null}` {
		t.Errorf("JSON = %s, want {\"n\":null}", b)
	}
}

func TestNullableUnmarshal(t *testing.T) {
	tests := []struct {
		name, in   string
		value      string
		set, null  bool
		wantAbsent bool
	}{
		{"null", `{"notes":null}`, "", false, true, false},
		{"value", `{"notes":"x"}`, "x", true, false, false},
		{"empty string", `{"notes":""}`, "", true, false, false},
		{"absent", `{}`, "", false, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var p ClientPatch
			if err := json.Unmarshal([]byte(tt.in), &p); err != nil {
				t.Fatal(err)
			}
			v, ok := p.Notes.Get()
			if v != tt.value || ok != tt.set || p.Notes.IsNull() != tt.null || p.Notes.IsZero() != tt.wantAbsent {
				t.Errorf("Notes = %q, set %v, null %v, absent %v; want %q, %v, %v, %v",
					v, ok, p.Notes.IsNull(), p.Notes.IsZero(), tt.value, tt.set, tt.null, tt.wantAbsent)
			}
		})
	}
	var n Nullable[int]
	if err := json.Unmarshal([]byte(`"x"`), &n); err == nil {
		t.Error("a string decoded into a Nullable[int]")
	}
}
