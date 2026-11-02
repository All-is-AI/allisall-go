package allisall

import (
	"slices"
	"strings"
	"testing"
)

// optionsProgram calls the client's methods as a caller would: call is the
// statement under test, with ctx and c in scope.
func optionsProgram(call string) string {
	return `package main

import (
	"context"
	"time"

	"github.com/All-is-AI/allisall-go"
)

func main() {
	ctx := context.Background()
	c, _ := allisall.NewClient()
	_ = time.Second
	` + call + `
}
`
}

// TestWithIdempotencyKeyCompilesOnlyWhereAKeyIsTaken type-checks calls of
// the real methods against this package: the key is accepted by
// Clients.Create, which takes an Idempotency-Key, and refused at compile
// time by Clients.Update, which does not, while the general options work
// on both.
func TestWithIdempotencyKeyCompilesOnlyWhereAKeyIsTaken(t *testing.T) {
	c := newChecker()
	tests := []struct {
		call    string
		compile bool
	}{
		{`_, _ = c.Clients.Create(ctx, allisall.ClientCreate{}, allisall.WithIdempotencyKey("k"))`, true},
		{`_, _ = c.Clients.Create(ctx, allisall.ClientCreate{}, allisall.WithRequestTimeout(time.Second), allisall.WithRequestMaxRetries(0), allisall.WithRequestHeader("X", "y"))`, true},
		{`_, _ = c.Clients.Update(ctx, "id", allisall.ClientPatch{}, allisall.WithRequestTimeout(time.Second), allisall.WithRequestMaxRetries(0), allisall.WithRequestHeader("X", "y"))`, true},
		{`_, _ = c.Clients.Update(ctx, "id", allisall.ClientPatch{}, allisall.WithIdempotencyKey("k"))`, false},
		// No type outside the package can be an option.
		{`type mine struct{}; var _ allisall.IdempotentRequestOption = mine{}; _, _ = ctx, c`, false},
	}
	for _, tt := range tests {
		problems := c.typeCheck(optionsProgram(tt.call), false)
		if compiled := len(problems) == 0; compiled != tt.compile {
			t.Errorf("%s: compiled %t, want %t: %v", tt.call, compiled, tt.compile, problems)
		}
		if !tt.compile && strings.Contains(tt.call, "Update(") && !slices.ContainsFunc(problems, func(p string) bool {
			return strings.Contains(p, "RequestOption")
		}) {
			t.Errorf("%s: problems %v do not name RequestOption", tt.call, problems)
		}
	}
}
