package allisall

import (
	"fmt"
	"math"
	"strconv"
)

// Decimal is an exact decimal number, used for money. It holds the
// number's text and writes it to JSON verbatim, so a cost never passes
// through a binary float on its way to the API.
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
