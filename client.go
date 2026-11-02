package allisall

import (
	"context"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	defaultBaseURL    = "https://api.allisai.ai"
	defaultTimeout    = 60 * time.Second
	defaultMaxRetries = 2

	envAPIKey  = "AIA_API_KEY"
	envBaseURL = "AIA_BASE_URL"
)

// Client calls the allisall API. Build one with [NewClient] and reuse it;
// it is safe for concurrent use and holds nothing that needs closing.
type Client struct {
	// Events groups the operations on events: ingest, and the events
	// behind a client's totals.
	Events *EventsService
	// Imports groups the CSV import operations.
	Imports *ImportsService
	// Clients groups the operations on clients.
	Clients *ClientsService
	// Agents groups the operations on agents.
	Agents *AgentsService
	// ValueModels groups the operations on value models and value
	// templates.
	ValueModels *ValueModelsService
	// OutcomeTypes groups the operations on outcome types.
	OutcomeTypes *OutcomeTypesService
	// Cost groups the operations on cost line items and the price table.
	Cost *CostService
	// Analytics groups the operations that compute a client's figures.
	Analytics *AnalyticsService
	// Calculations groups the operations on calculations.
	Calculations *CalculationsService
	// Reports groups the operations on reports.
	Reports *ReportsService
	// Dashboard groups the operations on the workspace's dashboard.
	Dashboard *DashboardService
	// Health groups the liveness check.
	Health *HealthService

	apiKey        string
	tokenProvider func(ctx context.Context) (string, error)
	baseURL       *url.URL
	timeout       time.Duration
	maxRetries    int
	httpClient    *http.Client
	userAgent     string
	// headers are the WithHeader headers, sent on every request.
	headers []header

	// Seams for tests: the clock, the wait between attempts and the
	// jitter source.
	now    func() time.Time
	sleep  func(ctx context.Context, d time.Duration) error
	random func() float64
}

// Format prints the client for the fmt verbs %v, %+v, %#v and %s,
// without its credential: only whether it holds an API key or a token
// provider, and its base URL.
func (c Client) Format(f fmt.State, _ rune) {
	credential := "API key [redacted]"
	if c.tokenProvider != nil {
		credential = "token provider"
	}
	base := ""
	if c.baseURL != nil {
		base = c.baseURL.Redacted()
	}
	_, _ = fmt.Fprintf(f, "allisall.Client{BaseURL: %s, Credential: %s}", base, credential)
}

// Format prints the service for every fmt verb without the client it
// belongs to, which holds the credential.
func (s EventsService) Format(f fmt.State, _ rune) {
	_, _ = fmt.Fprint(f, "allisall.EventsService{}")
}

// hasInvalidCredentialChar reports whether s holds a character outside
// printable ASCII, U+0021 to U+007E: a control character, a space, or any
// non-ASCII rune. Checking bytes suffices, because every byte of a
// non-ASCII rune's UTF-8 encoding is 0x80 or above.
func hasInvalidCredentialChar(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x21 || s[i] > 0x7e {
			return true
		}
	}
	return false
}

// Option configures a [Client] built by [NewClient].
type Option func(*settings)

type settings struct {
	apiKey        *string
	tokenProvider func(ctx context.Context) (string, error)
	providerSet   bool
	baseURL       *string
	timeout       *time.Duration
	maxRetries    *int
	httpClient    *http.Client
	headers       []header
}

// WithAPIKey sets the API key the client sends. Without it, and without
// [WithTokenProvider], the client reads AIA_API_KEY from the environment.
// Leading and trailing white space is removed from the key, as from
// AIA_API_KEY and from each token a provider returns, so a key read with
// a trailing newline works. A key that is empty after that counts as
// missing. What remains must be printable ASCII, U+0021 to U+007E: an
// inner space, a control character or any non-ASCII character is refused.
func WithAPIKey(key string) Option {
	return func(s *settings) { s.apiKey = &key }
}

// WithTokenProvider sets a function the client calls before every
// attempt, retries included, for the bearer token to send. An error it
// returns is not retried. In [EventsService.Ingest] it fails the batch:
// the call returns an [*IngestError] whose cause is the provider's error,
// reachable with errors.Is and errors.As.
//
// A provider may supply an API key fetched at run time, for example from
// a secret store, or a session token: every operation but ingest accepts
// one, and acts as the session's user. Ingest takes API keys only, and
// answers a session token with 401 unauthenticated.
func WithTokenProvider(provider func(ctx context.Context) (string, error)) Option {
	return func(s *settings) {
		s.tokenProvider = provider
		s.providerSet = true
	}
}

// WithBaseURL sets the API's base URL, an absolute http or https URL. A
// path it carries is kept: "https://example.test/api" sends ingest to
// "https://example.test/api/v1/events". Without it, the client reads
// AIA_BASE_URL from the environment, then uses https://api.allisai.ai.
func WithBaseURL(baseURL string) Option {
	return func(s *settings) { s.baseURL = &baseURL }
}

// WithTimeout bounds each attempt, not the whole call; the default is 60
// seconds. Bound the whole call with the context passed to each method.
func WithTimeout(d time.Duration) Option {
	return func(s *settings) { s.timeout = &d }
}

// WithMaxRetries sets how many times a failed attempt is retried; the
// default is 2. Zero sends one attempt.
func WithMaxRetries(n int) Option {
	return func(s *settings) { s.maxRetries = &n }
}

// WithHTTPClient sets the *http.Client the client sends requests with.
// The SDK never modifies it, so its own redirect policy applies: a client
// that follows redirects, as http.Client does by default, may send the
// request and its Authorization header on to the redirect's target.
// Without it, or with nil, the client uses a new http.Client with the
// default transport that follows no redirect, so a 3xx response is
// returned as an [*APIError] and is not retried.
func WithHTTPClient(c *http.Client) Option {
	return func(s *settings) { s.httpClient = c }
}

// WithHeader sends a header on every request. A header of the same name,
// in any case, from the SDK, such as User-Agent, or from an earlier
// WithHeader, is replaced, so one header is sent; a call's own
// [WithRequestHeader] replaces it in turn.
//
// It may not name Authorization, Content-Type, Idempotency-Key or
// X-Request-Id, which the SDK sets, nor Host, Content-Length or
// Transfer-Encoding, which net/http sets. That, a name that is not an HTTP
// token, or a value holding a control character such as a line feed,
// makes [NewClient] fail.
func WithHeader(name, value string) Option {
	return func(s *settings) { s.headers = append(s.headers, header{name: name, value: value}) }
}

// NewClient builds a Client. It resolves the credential and the base URL
// once, here, and fails rather than waiting for a request to fail.
//
// The credential is the [WithAPIKey] key or the [WithTokenProvider]
// provider; passing both is an error. Without either, it is AIA_API_KEY
// from the environment, when set and not empty. Without that, NewClient
// fails.
//
// Every error NewClient returns matches errors.Is(err, ErrAllisall).
func NewClient(opts ...Option) (*Client, error) {
	var s settings
	for _, opt := range opts {
		if opt != nil {
			opt(&s)
		}
	}

	c := &Client{
		timeout:    defaultTimeout,
		maxRetries: defaultMaxRetries,
		httpClient: s.httpClient,
		userAgent:  userAgent(),
		now:        time.Now,
		sleep:      sleepContext,
		random:     rand.Float64,
	}
	c.Events = &EventsService{client: c}
	c.Imports = &ImportsService{client: c}
	c.Clients = &ClientsService{client: c}
	c.Agents = &AgentsService{client: c}
	c.ValueModels = &ValueModelsService{client: c}
	c.OutcomeTypes = &OutcomeTypesService{client: c}
	c.Cost = &CostService{client: c}
	c.Analytics = &AnalyticsService{client: c}
	c.Calculations = &CalculationsService{client: c}
	c.Reports = &ReportsService{client: c}
	c.Dashboard = &DashboardService{client: c}
	c.Health = &HealthService{client: c}

	switch {
	case s.apiKey != nil && s.providerSet:
		return nil, fmt.Errorf("%w: pass an API key or a token provider, not both", ErrAllisall)
	case s.apiKey != nil:
		key := strings.TrimSpace(*s.apiKey)
		if key == "" {
			return nil, fmt.Errorf("%w: the API key is empty", ErrAllisall)
		}
		if hasInvalidCredentialChar(key) {
			return nil, fmt.Errorf("%w: the API key contains a character outside printable ASCII", ErrAllisall)
		}
		c.apiKey = key
	case s.providerSet:
		if s.tokenProvider == nil {
			return nil, fmt.Errorf("%w: the token provider is nil", ErrAllisall)
		}
		c.tokenProvider = s.tokenProvider
	default:
		key := strings.TrimSpace(os.Getenv(envAPIKey))
		if key == "" {
			return nil, fmt.Errorf("%w: no credential: pass WithAPIKey or WithTokenProvider, or set %s", ErrAllisall, envAPIKey)
		}
		if hasInvalidCredentialChar(key) {
			return nil, fmt.Errorf("%w: %s contains a character outside printable ASCII", ErrAllisall, envAPIKey)
		}
		c.apiKey = key
	}

	base := defaultBaseURL
	if s.baseURL != nil {
		base = *s.baseURL
	} else if env := os.Getenv(envBaseURL); env != "" {
		base = env
	}
	u, err := url.Parse(base)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("%w: base URL %q is not an absolute http or https URL", ErrAllisall, base)
	}
	c.baseURL = u

	if s.timeout != nil {
		if *s.timeout <= 0 {
			return nil, fmt.Errorf("%w: timeout %v is not positive", ErrAllisall, *s.timeout)
		}
		c.timeout = *s.timeout
	}
	if s.maxRetries != nil {
		if *s.maxRetries < 0 {
			return nil, fmt.Errorf("%w: max retries %d is negative", ErrAllisall, *s.maxRetries)
		}
		c.maxRetries = *s.maxRetries
	}
	for _, h := range s.headers {
		if err := checkHeader(h.name, h.value); err != nil {
			return nil, fmt.Errorf("%w: WithHeader: %w", ErrAllisall, err)
		}
	}
	c.headers = s.headers
	if c.httpClient == nil {
		// The SDK's own client follows no redirect: a 3xx becomes an
		// *APIError, so the credential is never sent on to another URL.
		c.httpClient = &http.Client{
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		}
	}
	return c, nil
}
