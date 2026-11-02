package allisall

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"testing"
)

func TestParseDecimal(t *testing.T) {
	valid := []string{"0", "-0", "1", "100", "12.50", "0.3683", "-0.000001", "99999999.999999"}
	for _, s := range valid {
		d, err := ParseDecimal(s)
		if err != nil {
			t.Errorf("ParseDecimal(%q): %v", s, err)
			continue
		}
		if d.String() != s {
			t.Errorf("ParseDecimal(%q).String() = %q", s, d.String())
		}
	}
	invalid := []string{"", "-", "+1", "01", "00", ".5", "1.", "1e2", "1E+2", "NaN", "Infinity", " 1", "1 ", "1,5",
		`1,"agent_key":"x"`, "0x10", "1.2.3", "--1"}
	for _, s := range invalid {
		if _, err := ParseDecimal(s); err == nil {
			t.Errorf("ParseDecimal(%q) succeeded, want an error", s)
		}
	}
}

func TestMustParseDecimalPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("MustParseDecimal(\"1e2\") did not panic")
		}
	}()
	MustParseDecimal("1e2")
}

func TestDecimalFromFloat(t *testing.T) {
	tests := []struct {
		in   float64
		want string
	}{
		{0.3683, "0.3683"},
		{12.5, "12.5"},
		{100, "100"},
		{-0.000001, "-0.000001"},
		{math.Nextafter(0.3, 1), "0.30000000000000004"},
		{1e21, "1000000000000000000000"},
		{1e-7, "0.0000001"},
	}
	for _, tt := range tests {
		d, err := DecimalFromFloat(tt.in)
		if err != nil || d.String() != tt.want {
			t.Errorf("DecimalFromFloat(%v) = %q, %v; want %q", tt.in, d.String(), err, tt.want)
		}
		if _, err := ParseDecimal(d.String()); err != nil {
			t.Errorf("DecimalFromFloat(%v) wrote %q, which ParseDecimal refuses", tt.in, d.String())
		}
	}
	for _, f := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		if _, err := DecimalFromFloat(f); err == nil {
			t.Errorf("DecimalFromFloat(%v) succeeded, want an error", f)
		}
	}
}

func TestDecimalMarshalJSONIsVerbatim(t *testing.T) {
	v := struct {
		Cost  Decimal  `json:"cost"`
		Other *Decimal `json:"other"`
		Zero  Decimal  `json:"zero"`
	}{Cost: MustParseDecimal("0.3683"), Other: new(MustParseDecimal("100.500000"))}
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(b), `{"cost":0.3683,"other":100.500000,"zero":0}`; got != want {
		t.Errorf("JSON = %s, want %s", got, want)
	}
}

func TestDecimalUnmarshalJSONIsExact(t *testing.T) {
	const text = "0.1234567890123456789"
	// The fixture must be one a float cannot carry, or the test could not
	// tell an exact decoder from one that reads through a float64.
	f, err := strconv.ParseFloat(text, 64)
	if err != nil {
		t.Fatal(err)
	}
	if strconv.FormatFloat(f, 'f', -1, 64) == text {
		t.Fatalf("a float64 carries %s exactly; pick a longer fixture", text)
	}
	var d Decimal
	if err := json.Unmarshal([]byte(text), &d); err != nil {
		t.Fatal(err)
	}
	if d.String() != text {
		t.Errorf("decoded %s, want %s", d, text)
	}
}

func TestDecimalUnmarshalJSONWritesOutExponents(t *testing.T) {
	tests := []struct{ in, want string }{
		{"1e-7", "0.0000001"},
		{"1.5E+3", "1500"},
		{"-2.50e1", "-25.0"},
		// The same eleven texts the other SDKs pin.
		{"0", "0"},
		{"-0", "-0"},
		{"12.50", "12.50"},
		{"1e0", "1"},
		{"1E2", "100"},
		{"0.5e1", "5"},
		{"0.05e1", "0.5"},
		{"123.456e-2", "1.23456"},
		{"123.456e+5", "12345600"},
		{"9e-9", "0.000000009"},
		{"-1.25e-3", "-0.00125"},
		// At the bound, written out exactly.
		{"1e400", "1" + strings.Repeat("0", 400)},
		{"1e-400", "0." + strings.Repeat("0", 399) + "1"},
		{"1e0400", "1" + strings.Repeat("0", 400)},
	}
	for _, tt := range tests {
		var d Decimal
		if err := json.Unmarshal([]byte(tt.in), &d); err != nil {
			t.Errorf("%s: %v", tt.in, err)
			continue
		}
		if d.String() != tt.want {
			t.Errorf("%s decoded to %s, want %s", tt.in, d, tt.want)
		}
		if _, err := ParseDecimal(d.String()); err != nil {
			t.Errorf("%s decoded to %s, which ParseDecimal refuses", tt.in, d)
		}
	}
}

func TestDecimalUnmarshalJSONRefuses(t *testing.T) {
	tests := []struct{ in, want string }{
		{"1e401", "beyond ±400"},
		{"1e-401", "beyond ±400"},
		{"-1e401", "beyond ±400"},
		{"1e999999999", "beyond ±400"},
		{"1e100000000", "beyond ±400"},
		{"1e99999999999999999999999", "beyond ±400"},
		{`"12.50"`, "string"},
		{"true", "bool"},
		{"{}", "object"},
		{"[]", "array"},
	}
	for _, tt := range tests {
		d := MustParseDecimal("7")
		err := d.UnmarshalJSON([]byte(tt.in))
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("UnmarshalJSON(%s) = %v, want an error saying %q", tt.in, err, tt.want)
		}
		if d.String() != "7" {
			t.Errorf("UnmarshalJSON(%s) changed the Decimal to %s", tt.in, d)
		}
	}
	// Text a caller hands it directly, which encoding/json would never pass.
	for _, in := range []string{"1e", "1e+", "1.e2", "01e2", "1e2e3", "1ex", "+1e2", "Infinity", ""} {
		var d Decimal
		if err := d.UnmarshalJSON([]byte(in)); err == nil {
			t.Errorf("UnmarshalJSON(%q) succeeded as %s, want an error", in, d)
		}
	}
}

func TestDecimalUnmarshalJSONNull(t *testing.T) {
	v := struct {
		Value Decimal  `json:"value"`
		Ptr   *Decimal `json:"ptr"`
	}{Value: MustParseDecimal("3"), Ptr: new(MustParseDecimal("4"))}
	if err := json.Unmarshal([]byte(`{"value":null,"ptr":null}`), &v); err != nil {
		t.Fatal(err)
	}
	if v.Value.String() != "3" {
		t.Errorf("null changed a Decimal to %s, want it left unchanged", v.Value)
	}
	if v.Ptr != nil {
		t.Errorf("null at a *Decimal gave %s, want nil", v.Ptr)
	}
}

// TestAPriceTableIsSentBackWithItsDigits decodes a price table holding a
// rate in exponent form, as the server writes 0.0000001, and encodes it as
// the body that sets the table: each rate keeps the digits it arrived with.
func TestAPriceTableIsSentBackWithItsDigits(t *testing.T) {
	var table PriceTable
	in := `{"currency":"USD","models":[{"model":"a","input_per_1k":1e-7,"output_per_1k":0.30},` +
		`{"model":"b","input_per_1k":0.1234567890123456789,"output_per_1k":12}]}`
	if err := json.Unmarshal([]byte(in), &table); err != nil {
		t.Fatal(err)
	}
	set := PriceTableSet{Currency: &table.Currency}
	for _, m := range table.Models {
		set.Models = append(set.Models, ModelRateInput(m))
	}
	b, err := json.Marshal(set)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"currency":"USD","models":[{"input_per_1k":0.0000001,"model":"a","output_per_1k":0.30},` +
		`{"input_per_1k":0.1234567890123456789,"model":"b","output_per_1k":12}]}`
	if string(b) != want {
		t.Errorf("body = %s\nwant   %s", b, want)
	}
}
