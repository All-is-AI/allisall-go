package allisall

import (
	"crypto/rand"
	"encoding/hex"
)

// newUUID returns a random UUID version 4 (RFC 9562, section 5.4) in its
// lowercase 8-4-4-4-12 form. It names generated event ids and the
// X-Request-Id of every attempt.
func newUUID() string {
	var b [16]byte
	// crypto/rand.Read never returns an error: it ends the program
	// irrecoverably if the system's source fails.
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10

	var out [36]byte
	hex.Encode(out[0:8], b[0:4])
	out[8] = '-'
	hex.Encode(out[9:13], b[4:6])
	out[13] = '-'
	hex.Encode(out[14:18], b[6:8])
	out[18] = '-'
	hex.Encode(out[19:23], b[8:10])
	out[23] = '-'
	hex.Encode(out[24:36], b[10:16])
	return string(out[:])
}
