package allisall

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
)

// Decimal is an exact decimal number, used for money. It holds the
// number's text and writes it to JSON verbatim, and reads a JSON number
// from its text, so money never passes through a binary float on its way
// to or from the API.
//
// Build one with [ParseDecimal], [MustParseDecimal] or [DecimalFromFloat].
// The zero Decimal is 0.
type Decimal struct {
	text string
}

// ParseDecimal returns the Decimal written by s: an optional "-", then
// either "0" or digits without a leading zero, then optionally "." and
// one or more digits. "12.50", "-0.3683" and "100" are accepted;
// "1e2", "+1", "01", ".5" and "1." are not.
//
// The API refuses a cost with more than 6 decimal places, as a row error.
// ParseDecimal does not check that, so the rule lives in one place.
func ParseDecimal(s string) (Decimal, error) {
	if !isDecimalText(s) {
		return Decimal{}, fmt.Errorf("allisall: %q is not a decimal number", s)
	}
	return Decimal{text: s}, nil
}

// MustParseDecimal is like [ParseDecimal] but panics if s is not a decimal
// number. It is meant for constants in source code, such as
// MustParseDecimal("0.25").
func MustParseDecimal(s string) Decimal {
	d, err := ParseDecimal(s)
	if err != nil {
		panic(err)
	}
	return d
}

// DecimalFromFloat returns the Decimal written by the shortest plain
// decimal text that reads back as f. For any value of up to 15
// significant digits, that is the literal the caller wrote: 0.3683 gives
// "0.3683". It refuses NaN and the infinities.
func DecimalFromFloat(f float64) (Decimal, error) {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return Decimal{}, fmt.Errorf("allisall: %v is not a finite number", f)
	}
	return Decimal{text: strconv.FormatFloat(f, 'f', -1, 64)}, nil
}

// String returns the Decimal's text.
func (d Decimal) String() string {
	if d.text == "" {
		return "0"
	}
	return d.text
}

// MarshalJSON writes the Decimal's text as a JSON number, verbatim.
func (d Decimal) MarshalJSON() ([]byte, error) {
	return []byte(d.String()), nil
}

// maxExponent is the largest exponent magnitude [Decimal.UnmarshalJSON]
// writes out in digits. Money is far smaller; the bound keeps a hostile
// exponent from building a huge string.
const maxExponent = 400

// UnmarshalJSON reads a JSON number from its text, before any binary float
// exists, so the Decimal holds exactly the digits the API sent. A number in
// exponent form is written out in plain digits by moving the decimal point,
// which never rounds: 1e-7 becomes 0.0000001 and 1.5E+3 becomes 1500. An
// exponent whose magnitude is over 400 is refused.
//
// Any JSON value other than a number, a string included, is refused. A
// JSON null leaves the Decimal unchanged.
func (d *Decimal) UnmarshalJSON(data []byte) error {
	text := string(bytes.TrimSpace(data))
	if text == "null" {
		return nil
	}
	if kind := jsonKind(text); kind != "number" {
		return &json.UnmarshalTypeError{Value: kind, Type: reflect.TypeFor[Decimal]()}
	}
	plain, err := plainDecimal(text)
	if err != nil {
		return err
	}
	d.text = plain
	return nil
}

// jsonKind names the kind of JSON value text starts with, as
// encoding/json's errors name it.
func jsonKind(text string) string {
	if text == "" {
		return "empty"
	}
	switch text[0] {
	case '"':
		return "string"
	case '{':
		return "object"
	case '[':
		return "array"
	case 't', 'f':
		return "bool"
	case 'n':
		return "null"
	}
	return "number"
}

// plainDecimal returns the decimal text equal to the JSON number text, with
// any exponent written out in digits.
func plainDecimal(text string) (string, error) {
	if isDecimalText(text) {
		return text, nil
	}
	mantissa, exponent, found := strings.Cut(strings.ToLower(text), "e")
	if !found || !isDecimalText(mantissa) {
		return "", fmt.Errorf("allisall: %q is not a JSON number", text)
	}
	shift, ok := parseExponent(exponent)
	if !ok {
		return "", fmt.Errorf("allisall: %q is not a JSON number", text)
	}
	if shift < -maxExponent || shift > maxExponent {
		return "", fmt.Errorf("allisall: the number %q has an exponent beyond ±%d", text, maxExponent)
	}
	sign := ""
	if strings.HasPrefix(mantissa, "-") {
		sign, mantissa = "-", mantissa[1:]
	}
	whole, fraction, _ := strings.Cut(mantissa, ".")
	digits := whole + fraction
	point := len(whole) + shift
	var out string
	switch {
	case point <= 0:
		out = "0." + strings.Repeat("0", -point) + digits
	case point >= len(digits):
		out = digits + strings.Repeat("0", point-len(digits))
	default:
		out = digits[:point] + "." + digits[point:]
	}
	// Moving the point can leave leading zeros in the whole part, as 0.5e1
	// becomes 05.
	for len(out) > 1 && out[0] == '0' && isDigit(out[1]) {
		out = out[1:]
	}
	if out = sign + out; !isDecimalText(out) {
		// Every branch above writes decimal text; the check keeps
		// MarshalJSON's verbatim output safe if one ever does not.
		return "", fmt.Errorf("allisall: %q is not a JSON number", text)
	}
	return out, nil
}

// parseExponent reads an exponent's text: an optional sign, then one or
// more digits. A value too large for an int reports the bound's side, so
// the caller refuses it as beyond the bound.
func parseExponent(s string) (int, bool) {
	sign := 1
	switch {
	case strings.HasPrefix(s, "-"):
		sign, s = -1, s[1:]
	case strings.HasPrefix(s, "+"):
		s = s[1:]
	}
	if s == "" {
		return 0, false
	}
	n := 0
	for i := 0; i < len(s); i++ {
		if !isDigit(s[i]) {
			return 0, false
		}
		if n <= maxExponent {
			n = n*10 + int(s[i]-'0')
		}
	}
	return sign * n, true
}

// isDecimalText reports whether s matches -?(0|[1-9][0-9]*)(\.[0-9]+)?.
func isDecimalText(s string) bool {
	i := 0
	if i < len(s) && s[i] == '-' {
		i++
	}
	switch {
	case i < len(s) && s[i] == '0':
		i++
	case i < len(s) && s[i] >= '1' && s[i] <= '9':
		for i < len(s) && isDigit(s[i]) {
			i++
		}
	default:
		return false
	}
	if i == len(s) {
		return true
	}
	if s[i] != '.' {
		return false
	}
	i++
	start := i
	for i < len(s) && isDigit(s[i]) {
		i++
	}
	return i > start && i == len(s)
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }
