package allisall

import (
	"context"
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	initialBackoff = 500 * time.Millisecond
	maxBackoff     = 8 * time.Second
	// maxRetryAfter is the longest Retry-After the SDK waits for. A longer
	// one ends the call with the error, which carries the value.
	maxRetryAfter = 60 * time.Second
)

// send sends a prepared request with its retries. It returns the 2xx
// response, or the error of the last attempt.
func (c *Client) send(ctx context.Context, p *prepared) (*response, error) {
	for retry := 0; ; retry++ {
		resp, err := c.attempt(ctx, p)
		if err == nil {
			return resp, nil
		}
		wait, ok := c.retryWait(err, retry, p.maxRetries, p.op.idempotent)
		if !ok {
			return nil, err
		}
		if err := c.sleep(ctx, wait); err != nil {
			return nil, err
		}
	}
}

// retryWait decides whether the failed attempt, after retry of maxRetries
// retries, is retried, and how long to wait first. On an operation that
// takes an Idempotency-Key, a 409 idempotency_key_in_use is retried too:
// an earlier attempt with the same key is still running.
func (c *Client) retryWait(err error, retry, maxRetries int, idempotent bool) (time.Duration, bool) {
	if retry >= maxRetries {
		return 0, false
	}
	var connErr *ConnectionError
	if errors.As(err, &connErr) {
		return c.backoff(retry + 1), true
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return 0, false
	}
	keyInUse := idempotent && apiErr.Status == http.StatusConflict && apiErr.Code == ErrorCodeIdempotencyKeyInUse
	if !retryableStatus(apiErr.Status) && !keyInUse {
		return 0, false
	}
	if apiErr.hasRetryAfter {
		if apiErr.RetryAfter > maxRetryAfter {
			return 0, false
		}
		return apiErr.RetryAfter, true
	}
	return c.backoff(retry + 1), true
}

// retryableStatus reports whether a response with this status is retried.
func retryableStatus(status int) bool {
	switch status {
	case http.StatusRequestTimeout, http.StatusTooManyRequests,
		http.StatusInternalServerError, http.StatusBadGateway,
		http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	}
	return false
}

// backoff is the wait before retry n (1 for the first retry):
// min(0.5 s × 2^(n−1), 8 s), multiplied by a random factor in (0.75, 1].
func (c *Client) backoff(n int) time.Duration {
	d := maxBackoff
	if n-1 < 5 {
		d = min(initialBackoff<<(n-1), maxBackoff)
	}
	return time.Duration(float64(d) * (1 - c.random()*0.25))
}

// parseRetryAfter reads a Retry-After value, delay-seconds or an
// HTTP-date (RFC 9110, section 10.2.3). It reports false for a value that
// does not parse or names a time in the past.
func parseRetryAfter(v string, now time.Time) (time.Duration, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, false
	}
	if isAllDigits(v) {
		secs, err := strconv.ParseInt(v, 10, 64)
		if err != nil || secs > math.MaxInt64/int64(time.Second) {
			// Only digits, so the one failure is overflow: a wait far
			// over the ceiling. Report the longest a Duration holds.
			return time.Duration(math.MaxInt64), true
		}
		return time.Duration(secs) * time.Second, true
	}
	t, ok := parseHTTPDate(v, now)
	if !ok {
		return 0, false
	}
	d := t.Sub(now)
	if d < 0 {
		return 0, false
	}
	return d, true
}

// parseHTTPDate reads the three HTTP-date forms of RFC 9110, section
// 5.6.7. http.ParseTime reads the obsolete rfc850-date's two-digit year
// in a fixed century (69 to 99 as 19xx), so that form's year is set here
// by the RFC's rule instead: in now's century, unless that puts it more
// than 50 years after now's year, in which case a century earlier.
func parseHTTPDate(v string, now time.Time) (time.Time, bool) {
	if t, err := time.Parse(http.TimeFormat, v); err == nil {
		return t, true
	}
	if t, err := time.Parse(time.RFC850, v); err == nil {
		t = t.UTC()
		thisYear := now.UTC().Year()
		year := thisYear - thisYear%100 + t.Year()%100
		if year > thisYear+50 {
			year -= 100
		}
		return time.Date(year, t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), 0, time.UTC), true
	}
	if t, err := time.Parse(time.ANSIC, v); err == nil {
		return t, true
	}
	return time.Time{}, false
}

func isAllDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if !isDigit(s[i]) {
			return false
		}
	}
	return true
}

// sleepContext waits for d, or until ctx ends.
func sleepContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
