# allisall-go

The Go client for the allisall API.

This release covers ingest: sending your agents' events to the
API. It needs Go 1.26 or later and imports nothing outside the standard
library.

## Install

```sh
go get github.com/All-is-AI/allisall-go
```

```go
import "github.com/All-is-AI/allisall-go"
```

The package name is `allisall`.

## Send events

```go
package main

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/All-is-AI/allisall-go"
)

func main() {
	// Reads the API key from AIA_API_KEY.
	client, err := allisall.NewClient()
	if err != nil {
		log.Fatal(err)
	}

	events := []allisall.EventInput{
		{
			Timestamp: time.Now(),
			AgentKey:  "support-bot",
			ClientKey: "contoso",
			Category:  allisall.EventCategoryModel,
			EventType: "completion",
			Model:     "gpt-4o",
			Cost:      new(allisall.MustParseDecimal("0.3683")),
			TokensIn:  new(int64(1200)),
			TokensOut: new(int64(350)),
		},
	}

	summary, err := client.Events.Ingest(context.Background(), events)
	var ingestErr *allisall.IngestError
	if errors.As(err, &ingestErr) {
		log.Fatalf("stopped at event %d: %v", ingestErr.RemainingFrom, err)
	}
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("accepted %d, duplicates %d, rejected %d",
		summary.Accepted, summary.Duplicates, summary.Rejected)
	for _, row := range summary.Errors {
		log.Printf("event %d: %s at %s: %s", row.Index, row.Code, row.Pointer, row.Detail)
	}
}
```

`Ingest` takes any number of events. It sends them in batches of at most
500 events and 5,000,000 bytes, one batch after another, and returns one
summary for the whole list. A refused event does not fail the call: it is
counted in `Rejected`, and each of its problems is an entry of `Errors`,
whose `Index` is the event's position in your list.

An event too large to send in any batch, more than 4,999,987 bytes as
JSON, is not sent. It is reported in the summary as a rejected row with
code `too_large`, and the other events are sent.

### Event ids and redelivery

Every event without an `EventID` is given a random one before it is first
sent. A batch that is retried therefore never stores an event twice: an
event the first attempt stored comes back counted as a duplicate.

That id lasts only for the call. If your own delivery is at-least-once,
such as a queue that can redeliver a message, set `EventID` yourself, for
example to the message's id. Otherwise an event delivered twice is sent
twice with two different ids, and stored twice.

### When a batch fails

`Ingest` stops at the first batch that fails, and sends nothing after it.
It returns an `*allisall.IngestError` holding:

- `Summary`: the outcome of the batches that succeeded;
- `Remaining`: every event from the failed batch onward, each with the
  `EventID` it was given;
- `RemainingFrom`: the position in your list of the first event in
  `Remaining`;
- the cause, reachable with `errors.As` or `errors.Is`.

To recover, send `Remaining` again. Any event the failed batch stored
before its response was lost comes back as a duplicate.

## Configuration

| Option | Default |
| --- | --- |
| `WithAPIKey(key)` | `AIA_API_KEY` from the environment |
| `WithTokenProvider(func(ctx) (string, error))` | None |
| `WithBaseURL(url)` | `AIA_BASE_URL` from the environment, then `https://api.allisai.ai` |
| `WithTimeout(d)`, per attempt | 60 seconds |
| `WithMaxRetries(n)` | 2 |
| `WithHTTPClient(*http.Client)` | A new `http.Client` |

`NewClient` resolves the credential once. Pass an API key or a token
provider, not both. Without either it reads `AIA_API_KEY`, and fails when
that is unset or empty. A token provider is called before every attempt,
retries included. An error it returns is not retried: it fails the
batch, and `Ingest` returns an `*allisall.IngestError` whose cause is
your provider's error, so `errors.Is` and `errors.As` still find it.

The timeout bounds each attempt. Bound the whole call, retries and waits
included, with the context you pass to `Ingest`.

## Retries

A failed attempt is retried up to twice when no response arrived, or
when the response is `408`, `429`, `500`, `502`, `503` or `504`. Any
other error status is returned at once.

Before a retry the client waits for the response's `Retry-After`. Without
one, it waits about 0.5 seconds before the first retry and about 1 second
before the second, with random jitter. When `Retry-After` asks for more
than 60 seconds, the client does not retry: it returns the error, whose
`RetryAfter` holds the wait, so you can decide.

## Errors

An error from `NewClient`, and every error from sending a request,
matches `errors.Is(err, allisall.ErrAllisall)`. Reach the details with
`errors.As`:

| Type | When |
| --- | --- |
| `*allisall.APIError` | The API answered with an error status. `Code` names the error; `Problem` holds the whole problem document |
| `*allisall.ConnectionError` | No response arrived: the connection failed or the attempt timed out. `RequestID` is the id the client sent |
| `*allisall.IngestError` | A batch failed inside `Ingest`. It wraps one of the two above, the context's error, or your token provider's error |

A mistake in your own values is an ordinary error, which does not match
`ErrAllisall` and fails the same way on every run. These are an event
`Ingest` cannot encode as JSON, such as a `NaN` `Quantity` or a string
that is not valid UTF-8, which ends the call before anything is sent;
text `ParseDecimal` refuses; and a `NaN` or infinity passed to
`DecimalFromFloat`.

Branch on `APIError.Code`. The API may add codes, and the SDK keeps a code
it does not list as it arrived, so fall back on `APIError.Status` for a
code you do not recognise.

```go
var apiErr *allisall.APIError
if errors.As(err, &apiErr) && apiErr.Code == allisall.ErrorCodeRateLimited {
	// ...
}
```

Quote `RequestID` when you report a problem.

## Money

A cost is an `allisall.Decimal`, which the client writes to the API
exactly as its text, never through a binary float.

```go
cost, err := allisall.ParseDecimal("12.50")      // from text
cost = allisall.MustParseDecimal("0.3683")       // a constant in source
cost, err = allisall.DecimalFromFloat(0.3683)    // from a float64
```

`DecimalFromFloat` writes the shortest text that reads back as the same
number, which is the literal you wrote for any value of up to 15
significant digits. The API takes at most 6 decimal places.

## License

Apache License 2.0. See [LICENSE](LICENSE).
