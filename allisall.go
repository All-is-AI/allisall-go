// Package allisall is the Go client for the allisall API.
//
// A [Client], built with [NewClient], groups its methods by resource, each
// a field of the client: Events, Imports, Clients, Agents, ValueModels,
// OutcomeTypes, Cost, Analytics, Calculations, Reports, Dashboard and
// Health. Every method takes a context first, then the ids in its path,
// then its body or query, then options for that one call.
//
//	client, err := allisall.NewClient() // reads AIA_API_KEY
//	if err != nil {
//		return err
//	}
//	detail, err := client.Clients.Get(ctx, clientID)
//
// [EventsService.Ingest] sends any number of events in as many batches as
// the API's limits require, and reports one combined [IngestSummary].
//
// A list method returns the first [Page]; [Page.All] walks every item of
// every page. An update is a merge patch: a field left at its zero value
// is not sent, and a [Nullable] field is cleared with [Null]. Amounts of
// money are a [Decimal], exact in both directions.
//
// A request that is safe to send again is retried, twice by default: a
// GET, PUT or DELETE, ingest, and a create or an import's commit, which
// send one Idempotency-Key on every attempt. A PATCH, an import's preview
// and [ValueModelsService.SetLineItemsForClient] are never retried.
//
// An error from [NewClient], and every error from sending a request,
// matches errors.Is(err, allisall.ErrAllisall). The concrete types are
// [*APIError], [*ConnectionError] and [*IngestError]; reach their fields
// with errors.As, and branch on [APIError.Code]. A mistake in the caller's
// own values is an ordinary error that does not match ErrAllisall, and
// nothing is sent. Such mistakes include a path id that is empty, . or
// ..; a header option the SDK refuses, such as one it sets itself, or an
// Idempotency-Key it cannot send; an event Ingest cannot encode as JSON;
// text [ParseDecimal] refuses; and a non-finite number passed to
// [DecimalFromFloat].
//
// The credential is an API key or a token provider set with
// [WithTokenProvider]. A provider may return a signed-in user's session
// token, which every method but Ingest accepts.
//
// A Client holds no mutable state after construction and is safe for
// concurrent use.
package allisall
