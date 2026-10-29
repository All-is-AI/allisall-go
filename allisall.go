// Package allisall is the Go client for the allisall API.
//
// This release covers ingest: a [Client] built with [NewClient], and
// [EventsService.Ingest], which sends any number of events in as many
// batches as the API's limits require and reports one combined
// [IngestSummary].
//
//	client, err := allisall.NewClient() // reads AIA_API_KEY
//	if err != nil {
//		return err
//	}
//	summary, err := client.Events.Ingest(ctx, events)
//
// An error from [NewClient], and every error from sending a request,
// matches errors.Is(err, allisall.ErrAllisall). The concrete types are
// [*APIError], [*ConnectionError] and [*IngestError]; reach their fields
// with errors.As. A mistake in the caller's own values is an ordinary
// error that does not match ErrAllisall: an event Ingest cannot encode as
// JSON, text [ParseDecimal] refuses, and a non-finite number passed to
// [DecimalFromFloat].
//
// A Client holds no mutable state after construction and is safe for
// concurrent use.
package allisall
