# allisall-go

The Go client for the allisall API: a typed client covering events,
imports, clients, agents, value models, outcome types, costs, analytics,
calculations, reports, the dashboard and the health check. It needs Go
1.26 or later and imports nothing outside the standard library.

## Install

```sh
go get github.com/All-is-AI/allisall-go
```

```go
import "github.com/All-is-AI/allisall-go"
```

The package name is `allisall`.

## Create a client

The client reads your API key from `AIA_API_KEY`:

```sh
export AIA_API_KEY=aia_...
```

```go
client, err := allisall.NewClient()
if err != nil {
	log.Fatal(err)
}
```

A `Client` holds no mutable state after construction and is safe for
concurrent use.

## Call the API

The client groups its methods by resource, each a field of the client:
`Events`, `Imports`, `Clients`, `Agents`, `ValueModels`, `OutcomeTypes`,
`Cost`, `Analytics`, `Calculations`, `Reports`, `Dashboard` and `Health`.
Every method takes a `context.Context` first, then the ids in its path,
then its body or query, then options. Each method's documentation names
the API operation it calls and the scope your key needs.

```go
package main

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/All-is-AI/allisall-go"
)

func main() {
	client, err := allisall.NewClient()
	if err != nil {
		log.Fatal(err)
	}
	ctx := context.Background()

	created, err := client.Clients.Create(ctx, allisall.ClientCreate{
		Name:      "Acme Logistics",
		Status:    allisall.ClientStatusActive,
		ClientKey: new("acme"),
	})
	var apiErr *allisall.APIError
	if errors.As(err, &apiErr) {
		log.Fatalf("the API refused the request: %d %s (request %s)",
			apiErr.Status, apiErr.Code, apiErr.RequestID)
	}
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("created", created.ID)
}
```

A call that creates something sends an `Idempotency-Key`, and sends the
same key again on a retry, so a retry never creates a second one. Pass
`allisall.WithIdempotencyKey(key)` to choose the key yourself, for
example to make a create safe to repeat after your process restarts.

### Update with a merge patch

An update sends only what you set. A field you leave at its zero value
keeps its stored value. A field that can be cleared is an
`allisall.Nullable`: `allisall.NewNullable(v)` sets it, `allisall.Null`
clears it, and the zero `Nullable` leaves it unchanged.

```go
updated, err := client.Clients.Update(ctx, clientID, allisall.ClientPatch{
	Status:      new(allisall.ClientStatusChurned),
	Notes:       allisall.Null[string](),           // clears the notes
	RenewalDate: allisall.NewNullable("2026-12-31"), // sets the renewal date
})
```

## Lists

A list method sends the first request when you call it, and returns the
first page. `All` walks every item of that page and of each page after
it, fetching each page as the loop reaches it:

```go
page, err := client.Agents.List(ctx, nil)
if err != nil {
	return err
}
for agent, err := range page.All(ctx) {
	if err != nil {
		return err
	}
	fmt.Println(agent.AgentKey, agent.Name)
}
```

To work a page at a time, read the page's `Data`, and move on with
`HasNextPage` and `NextPage`. `Body` holds the whole response, such as
the event list's `Total`:

```go
page, err := client.Clients.List(ctx, &allisall.ListClientsParams{Limit: new(50)})
for err == nil {
	fmt.Println(len(page.Data), "clients")
	if !page.HasNextPage() {
		break
	}
	page, err = page.NextPage(ctx)
}
if err != nil {
	return err
}
```

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
500 events and 4,000,000 bytes, one batch after another, and returns one
summary for the whole list. A refused event does not fail the call: it is
counted in `Rejected`, and each of its problems is an entry of `Errors`,
whose `Index` is the event's position in your list. The API lists at most
20 problems of one event, and at most 1,000,000 bytes of entries for one
batch. When either limit leaves entries out, `ErrorsTruncated` is
`true`, and a refused event may have no entry; the counts still cover
every event.

An event too large to send in any batch, more than 3,999,987 bytes as
JSON, is not sent. It is reported in the summary as a rejected row with
code `too_large`, and the other events are sent.

Ingest takes an API key only. A session token is refused with `401`
`unauthenticated`.

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

## Import a CSV file

`Imports.Preview` reports what an import would store, and stores nothing.
`Imports.Commit` stores the file's valid rows. Both take the mapping from
your columns to event members, and the file as an `io.ReadSeeker`, such
as an `*os.File`:

```go
f, err := os.Open("events.csv")
if err != nil {
	return err
}
defer f.Close()
result, err := client.Imports.Commit(ctx, allisall.ImportUpload{
	Mapping: allisall.ImportMapping{
		Columns: &map[string]string{
			"timestamp":  "Time",
			"agent_key":  "Agent",
			"category":   "Category",
			"event_type": "Type",
		},
		Timezone: new("Asia/Tokyo"),
	},
	File: f,
})
```

The file is read again from the same position for each retry, so it is
never held in memory.

## Money

An amount of money is an `allisall.Decimal`, which holds the exact
amount and never passes through a binary float. Write one from text:

```go
cost, err := allisall.ParseDecimal("12.50")      // from text
cost = allisall.MustParseDecimal("0.3683")       // a constant in source
cost, err = allisall.DecimalFromFloat(0.3683)    // from a float64
```

`DecimalFromFloat` writes the shortest text that reads back as the same
number, which is the literal you wrote for any value of up to 15
significant digits. The API takes at most 6 decimal places.

An amount the API returns is a `Decimal` too, with every digit the API
sent. Its `String` method gives the exact text, and a `Decimal` you read
can be sent back unchanged:

```go
table, err := client.Cost.GetPriceTable(ctx)
if err != nil {
	return err
}
for _, rate := range table.Models {
	fmt.Println(rate.Model, rate.InputPer1K.String(), table.Currency)
}
```

## Errors

An error from `NewClient`, and every error from sending a request,
matches `errors.Is(err, allisall.ErrAllisall)`. Reach the details with
`errors.As`:

| Type | When |
| --- | --- |
| `*allisall.APIError` | The API answered with an error status. `Code` names the error; `Problem` holds the whole problem document |
| `*allisall.ConnectionError` | No response arrived, after any retries: the connection failed or the attempt timed out. `RequestID` is the id the client sent |
| `*allisall.IngestError` | A batch failed inside `Ingest`. It wraps one of the two above, the context's error, or your token provider's error |

Branch on `APIError.Code`. The API may add codes, and the SDK keeps a code
it does not list as it arrived, so fall back on `APIError.Status` for a
code you do not recognise. Quote `RequestID` when you report a problem.

```go
err = client.Clients.Delete(ctx, clientID)
var apiErr *allisall.APIError
if errors.As(err, &apiErr) && apiErr.Code == allisall.ErrorCodeClientHasEvents {
	// Events carry the client's key, so mark the client churned instead.
	_, err = client.Clients.Update(ctx, clientID, allisall.ClientPatch{
		Status: new(allisall.ClientStatusChurned),
	})
}
```

A mistake in your own values is an ordinary error, which does not match
`ErrAllisall` and fails the same way on every run. Nothing is sent. These
include a path id that is empty, `.` or `..`; text that is not valid
UTF-8 in a body; a header option the SDK refuses, such as one it sets
itself, or an `Idempotency-Key` it cannot send; an import file that cannot seek; an event `Ingest` cannot encode
as JSON, such as a `NaN` `Quantity`; text `ParseDecimal` refuses; and a
`NaN` or infinity passed to `DecimalFromFloat`. An error your token
provider returns reaches you unchanged, and is not retried.

## Retries

The client retries a failed attempt up to twice when no response arrived,
or when the response is `408`, `429`, `500`, `502`, `503` or `504`. Any
other error status is returned at once.

It retries only a request that is safe to send again: a `GET`, `PUT`
or `DELETE`; ingest, whose event ids deduplicate; and a call that sends an
`Idempotency-Key`, which is every create and an import's commit. Such a
call is also retried on a `409` `idempotency_key_in_use`, which means an
earlier attempt with the same key is still running. A `PATCH`, an import
preview, and `ValueModels.SetLineItemsForClient` are never retried. Setting
line items is guarded by the model's `Version`, which refuses a repeat of
a write that already landed; after a network error, read the model again
before you retry.

Before a retry the client waits for the response's `Retry-After`. Without
one, it waits about 0.5 seconds before the first retry and about 1 second
before the second, with random jitter. When `Retry-After` asks for more
than 60 seconds, the client does not retry: it returns the error, whose
`RetryAfter` holds the wait, so you can decide.

A `DELETE` retried after a lost response can answer `404` for a delete
that succeeded.

## Configuration

`NewClient` takes these options:

| Option | Default |
| --- | --- |
| `WithAPIKey(key)` | `AIA_API_KEY` from the environment |
| `WithTokenProvider(func(ctx) (string, error))` | None |
| `WithBaseURL(url)` | `AIA_BASE_URL` from the environment, then `https://api.allisai.ai` |
| `WithTimeout(d)`, per attempt | 60 seconds |
| `WithMaxRetries(n)` | 2 |
| `WithHTTPClient(*http.Client)` | A new `http.Client` |
| `WithHeader(name, value)`, on every request | None |

`NewClient` resolves the credential once. Pass an API key or a token
provider, not both. Without either it reads `AIA_API_KEY`, and fails when
that is unset or empty. A token provider is called before every attempt,
retries included.

The timeout bounds each attempt. Bound the whole call, retries and waits
included, with the context you pass to the method.

Each method also takes options for that one call:

| Option | Effect |
| --- | --- |
| `WithRequestTimeout(d)` | The timeout of each attempt of this call |
| `WithRequestMaxRetries(n)` | The retries of this call. It cannot make a request the client never retries eligible |
| `WithRequestHeader(name, value)` | One header for this call, over the client's `WithHeader` |
| `WithIdempotencyKey(key)` | The `Idempotency-Key` of a create or an import's commit. Other methods do not accept it |

```go
agents, err := client.Agents.List(ctx, nil,
	allisall.WithRequestTimeout(10*time.Second),
	allisall.WithRequestMaxRetries(0),
)
```

## Sessions

A token provider may return a signed-in user's session token instead of
an API key. Every method but `Events.Ingest` accepts one, and acts as
that user. The client calls the provider before every attempt, so a
refreshed token is picked up:

```go
client, err := allisall.NewClient(allisall.WithTokenProvider(
	func(ctx context.Context) (string, error) {
		// In an application, return the signed-in user's current token
		// from your sign-in library.
		return os.Getenv("AIA_SESSION_TOKEN"), nil
	},
))
```

Validating, publishing and reopening a calculation, sharing and revoking a
report, and managing API keys need a signed-in user, and this SDK does
not offer them.

## Documentation

See [https://docs.allisai.ai](https://docs.allisai.ai).

## License

Apache License 2.0. See [LICENSE](LICENSE).
