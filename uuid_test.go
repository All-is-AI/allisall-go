package allisall

import "testing"

func TestNewUUID(t *testing.T) {
	seen := make(map[string]bool)
	for range 10_000 {
		u := newUUID()
		if !uuidV4.MatchString(u) {
			t.Fatalf("%q is not a lowercase UUID version 4", u)
		}
		// The version nibble is 4 and the variant bits are 10.
		if u[14] != '4' {
			t.Fatalf("%q: version nibble %c, want 4", u, u[14])
		}
		switch u[19] {
		case '8', '9', 'a', 'b':
		default:
			t.Fatalf("%q: variant nibble %c, want 10xx", u, u[19])
		}
		if seen[u] {
			t.Fatalf("%q generated twice", u)
		}
		seen[u] = true
	}
}
