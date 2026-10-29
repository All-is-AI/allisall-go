package allisall

import (
	"encoding/json"
	"math"
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
