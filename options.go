package allisall

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

// IdempotentRequestOption configures one call of an operation that takes
// an Idempotency-Key. Every [RequestOption] is one, and so is
// [WithIdempotencyKey].
type IdempotentRequestOption interface{ applyIdempotent(*callConfig) }

// RequestOption configures one call of any operation. Pass it as a
// method's last argument; it applies to that call alone.
type RequestOption interface {
	IdempotentRequestOption
	apply(*callConfig)
}

// callConfig is what a call's options set. A field left nil takes the
// client's value.
type callConfig struct {
	timeout        *time.Duration
	maxRetries     *int
	headers        []header
	idempotencyKey *string
	// err is the first option that was refused. It is returned before
	// anything is sent.
	err error
}

// header is one header a caller set, as given.
type header struct {
	name, value string
}

// requestOption is a RequestOption, which every call accepts.
type requestOption func(*callConfig)

func (o requestOption) apply(c *callConfig)           { o(c) }
func (o requestOption) applyIdempotent(c *callConfig) { o(c) }

// idempotencyKeyOption is accepted only by an operation that takes an
// Idempotency-Key: it is an IdempotentRequestOption and not a
// RequestOption.
type idempotencyKeyOption string

func (o idempotencyKeyOption) applyIdempotent(c *callConfig) {
	if c.err != nil {
		return
	}
	if err := checkHeaderValue("Idempotency-Key", string(o)); err != nil {
		c.err = fmt.Errorf("allisall: WithIdempotencyKey: %w", err)
		return
	}
	key := string(o)
	c.idempotencyKey = &key
}

// WithRequestTimeout bounds each attempt of this call, in place of the
// client's [WithTimeout]. Bound the whole call with its context. A
// duration of zero or less is refused, before anything is sent, with an
// error matching [ErrAllisall].
func WithRequestTimeout(d time.Duration) RequestOption {
	return requestOption(func(c *callConfig) {
		if c.err != nil {
			return
		}
		if d <= 0 {
			c.err = fmt.Errorf("%w: timeout %v is not positive", ErrAllisall, d)
			return
		}
		c.timeout = &d
	})
}

// WithRequestMaxRetries sets how many times this call is retried, in place
// of the client's [WithMaxRetries]. It cannot make a request eligible that
// the SDK never retries, such as a PATCH. A negative number is refused,
// before anything is sent, with an error matching [ErrAllisall].
func WithRequestMaxRetries(n int) RequestOption {
	return requestOption(func(c *callConfig) {
		if c.err != nil {
			return
		}
		if n < 0 {
			c.err = fmt.Errorf("%w: max retries %d is negative", ErrAllisall, n)
			return
		}
		c.maxRetries = &n
	})
}

// WithRequestHeader sends one header on every attempt of this call. It
// replaces a header of the same name, in any case, from the client's
// [WithHeader] or from the SDK, such as User-Agent, so one header is sent.
//
// It may not name Authorization, Content-Type, Idempotency-Key or
// X-Request-Id, which the SDK sets, nor Host, Content-Length or
// Transfer-Encoding, which net/http sets. That, a name that is not an HTTP
// token, or a value holding a control character such as a line feed, is
// refused before anything is sent, with an error that does not match
// [ErrAllisall].
func WithRequestHeader(name, value string) RequestOption {
	return requestOption(func(c *callConfig) {
		if c.err != nil {
			return
		}
		if err := checkHeader(name, value); err != nil {
			c.err = fmt.Errorf("allisall: WithRequestHeader: %w", err)
			return
		}
		c.headers = append(c.headers, header{name: name, value: value})
	})
}

// WithIdempotencyKey sets the Idempotency-Key this call sends, in place of
// the one the SDK generates for each call. The key is sent as given, on
// every attempt. Set your own to make a create safe to repeat across
// process restarts.
//
// A second call with the same key and the same request, within 24 hours,
// gets the first call's stored response. Only a successful response is
// stored, so a key whose first call failed can be sent again. The same
// key with a different body is refused with 422 idempotency_key_reused.
//
// Only the operations that take an Idempotency-Key accept this option. A
// key holding a control character, such as a line feed, is refused before
// anything is sent, with an error that does not match [ErrAllisall].
func WithIdempotencyKey(key string) IdempotentRequestOption {
	return idempotencyKeyOption(key)
}

// resolveOptions applies a call's options in order. A nil option is
// skipped.
func resolveOptions(opts []IdempotentRequestOption) (callConfig, error) {
	var cfg callConfig
	for _, o := range opts {
		if o != nil {
			o.applyIdempotent(&cfg)
		}
	}
	return cfg, cfg.err
}

// idempotentOptions converts a call's options to the type every call
// resolves. Go does not convert a slice of one interface type to another.
func idempotentOptions(opts []RequestOption) []IdempotentRequestOption {
	out := make([]IdempotentRequestOption, len(opts))
	for i, o := range opts {
		out[i] = o
	}
	return out
}

// ownedHeaders are the headers the SDK sets itself, in canonical form.
var ownedHeaders = []string{"Authorization", "Content-Type", "Idempotency-Key", "X-Request-Id"}

// transportHeaders are the headers net/http writes itself and drops from a
// client request's header map without a word, so setting one could only
// mislead.
var transportHeaders = []string{"Host", "Content-Length", "Transfer-Encoding"}

// checkHeader refuses a header the SDK owns, one net/http would drop, a
// name that is not an HTTP token, and a value net/http would refuse to
// send.
func checkHeader(name, value string) error {
	if !isToken(name) {
		return fmt.Errorf("header name %q is not an HTTP token", name)
	}
	for _, owned := range ownedHeaders {
		if strings.EqualFold(name, owned) {
			return fmt.Errorf("%s is set by the SDK", owned)
		}
	}
	for _, ignored := range transportHeaders {
		if strings.EqualFold(name, ignored) {
			return fmt.Errorf("%s is set by net/http, which ignores it in a request's headers", ignored)
		}
	}
	return checkHeaderValue(name, value)
}

// checkHeaderValue refuses a value net/http's transport would refuse: one
// holding a control character other than a horizontal tab. Sent anyway,
// every attempt would fail before reaching the network. The message never
// holds the value.
func checkHeaderValue(name, value string) error {
	for i := 0; i < len(value); i++ {
		if b := value[i]; (b < 0x20 && b != '\t') || b == 0x7f {
			return fmt.Errorf("the value of %s holds a control character, byte %d", name, i)
		}
	}
	return nil
}

// isToken reports whether s is an RFC 9110 token: one or more of the
// characters a header name may hold.
func isToken(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		b := s[i]
		switch {
		case b >= 'a' && b <= 'z', b >= 'A' && b <= 'Z', b >= '0' && b <= '9':
		case strings.IndexByte("!#$%&'*+-.^_`|~", b) >= 0:
		default:
			return false
		}
	}
	return true
}

// setHeaders sets each header in h, replacing one of the same name in any
// case. http.Header.Set compares names in their canonical form, which
// ignores case for a token.
func setHeaders(h http.Header, headers []header) {
	for _, hd := range headers {
		h.Set(hd.name, hd.value)
	}
}
