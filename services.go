package allisall

import (
	"context"
	"fmt"
)

// This file holds one service per group of operations, each reached as a
// field of [Client]. Every method sends one operation: it takes the
// context first, then the operation's path ids, then its body or query,
// then the call's options.

// opCall builds a call of the operation with the given id. A missing id is
// a mistake in the SDK, which its tests catch; it is an error, never a
// panic.
func opCall(id string, path ...string) (call, error) {
	op, err := operationFor(id)
	if err != nil {
		return call{}, err
	}
	return call{op: op, path: path}, nil
}

// send calls an operation that is not a list and returns its decoded
// response.
func send[R any](ctx context.Context, c *Client, cl call, err error, opts []IdempotentRequestOption) (*R, error) {
	if err != nil {
		return nil, err
	}
	out := new(R)
	if err := c.do(ctx, cl, opts, out); err != nil {
		return nil, err
	}
	return out, nil
}

// sendEmpty calls an operation whose success response has no body.
func sendEmpty(ctx context.Context, c *Client, cl call, err error, opts []IdempotentRequestOption) error {
	if err != nil {
		return err
	}
	return c.do(ctx, cl, opts, nil)
}

// sendList calls a list operation and returns its first page.
func sendList[T, B, Q any](ctx context.Context, c *Client, cl call, err error, query *Q, opts []RequestOption, data func(*B) []T) (*Page[T, B], error) {
	if err != nil {
		return nil, err
	}
	return list(ctx, c, cl, query, opts, data)
}

// List lists the workspace's events whose timestamp falls in the period,
// newest first, and returns the first page. Walk every event of every page
// with [Page.All]. A nil query sends none.
//
// From and To are required unless ImportBatchID is set, and then are both
// set or neither; the API refuses a missing bound with required. They are
// pointers because the type cannot say so.
//
// The list is held still under an arrival limit, AsOf, which the first
// response issues. A walk follows the next cursor, which carries it, so
// every page of one walk reads the same events while new ones arrive. To
// show numbered pages instead, send the first response's Body.AsOf with
// each later Page. Filtered by ImportBatchID, AsOf is issued but not
// applied, because an import's events appear together when it commits.
// Otherwise an event appears a second or two after the API stores it, and
// later while a long request, such as a large import, is still in progress,
// so a read straight after an ingest may not include it yet.
//
// Set Page for a numbered page: the page's Body then holds Page, Total and
// TotalCapped. Total counts at most 10,000 events, and TotalCapped is true
// when more matched; filtered by ImportBatchID, Total is exact. Only that
// first page is counted; every later page of a walk continues by cursor
// from it, and its Body.Page, Body.Total and Body.TotalCapped are nil. To
// show another numbered page, call List again with its Page rather than
// [Page.NextPage].
//
// A Sort and Dir other than timestamp descending need Page, or the API
// refuses the call with required on page. A walk from a sorted page
// continues by offset and reads every earlier row for each page, so walk
// in the default order.
//
// Needs the read scope. Operation listEvents.
func (s *EventsService) List(ctx context.Context, query *ListEventsParams, opts ...RequestOption) (*Page[AttributedEvent, EventList], error) {
	cl, err := opCall("listEvents")
	return sendList(ctx, s.client, cl, err, query, opts, func(b *EventList) []AttributedEvent { return b.Data })
}

// GetSummary counts the events [EventsService.List] holds under the same
// period, filters and AsOf: in all, per day and per category. A day is 24
// hours counted from From, so local midnight with its offset as From gives
// local days. The period is required, also with ImportBatchID. Without
// ImportBatchID, an event is counted a second or two after the API stores
// it, and later while a long request, such as a large import, is still in
// progress, so a read straight after an ingest may not count it yet.
//
// Needs the read scope. Operation getEventSummary.
func (s *EventsService) GetSummary(ctx context.Context, query GetEventSummaryParams, opts ...RequestOption) (*EventSummary, error) {
	cl, err := opCall("getEventSummary")
	cl.query = &query
	return send[EventSummary](ctx, s.client, cl, err, idempotentOptions(opts))
}

// ListArrivals lists the workspace's events in the order the API stored
// them, newest first, below an arrival limit, AsOf, which the response
// issues, and returns the first page. Walk every event of every page with
// [Page.All]. A nil query sends none. From and To filter by timestamp, and
// each is optional on its own.
//
// With Since, the list holds the events that arrived at or after it, and
// Body.Count counts them. To poll, send each response's Body.AsOf as the
// next call's Since and walk the result: consecutive windows meet without a
// gap or an overlap. To learn only how many arrived, set Limit to 1 and
// read Body.Count. The SDK does not poll on its own. Since may be at most
// 24 hours back; a feed further behind reloads without it. An event falls
// below AsOf a second or two after the API stores it, and later while a
// long request, such as a large import, is still in progress. A read
// straight after an ingest may not include it yet; poll with Since until it
// does.
//
// Needs the read scope. Operation listEventArrivals.
func (s *EventsService) ListArrivals(ctx context.Context, query *ListEventArrivalsParams, opts ...RequestOption) (*Page[AttributedEvent, EventArrivalList], error) {
	cl, err := opCall("listEventArrivals")
	return sendList(ctx, s.client, cl, err, query, opts, func(b *EventArrivalList) []AttributedEvent { return b.Data })
}

// ListForClient lists the events behind a client's totals: the event
// drill-down, over the period query.From to query.To, which are required.
//
// Pass query.Page or query.Cursor, not both; neither means page 1. A Page
// beyond the end returns no rows and the true total. The first page's Body
// holds Total and Period; walk every event from there with [Page.All].
//
// Needs the read scope. Operation listClientEvents.
func (s *EventsService) ListForClient(ctx context.Context, clientID string, query ListClientEventsParams, opts ...RequestOption) (*Page[AttributedEvent, ClientEventList], error) {
	cl, err := opCall("listClientEvents", clientID)
	return sendList(ctx, s.client, cl, err, &query, opts, func(b *ClientEventList) []AttributedEvent { return b.Data })
}

// ImportsService groups the CSV import operations. Reach it as
// client.Imports.
type ImportsService struct {
	client *Client
}

// List lists the workspace's imports, newest first, each with its counts,
// and returns the first page. Walk every import of every page with
// [Page.All]. A nil query sends none.
//
// Set Page for a numbered page: the page's Body then holds Page and Total.
// Only that first page is counted; every later page of a walk continues by
// cursor from it, and its Body.Page and Body.Total are nil. To show another
// numbered page, call List again with its Page rather than
// [Page.NextPage]. A page number past the end has no items, and the walk
// ends there.
//
// Needs the read scope. Operation listImports.
func (s *ImportsService) List(ctx context.Context, query *ListImportsParams, opts ...RequestOption) (*Page[ImportSummary, ImportList], error) {
	cl, err := opCall("listImports")
	return sendList(ctx, s.client, cl, err, query, opts, func(b *ImportList) []ImportSummary { return b.Data })
}

// Get reads an import, with the mapping it ran with. Send that mapping as
// the next import's with [AppliedImportMapping.ToImportMapping].
//
// Needs the read scope. Operation getImport.
func (s *ImportsService) Get(ctx context.Context, id string, opts ...RequestOption) (*Import, error) {
	cl, err := opCall("getImport", id)
	return send[Import](ctx, s.client, cl, err, idempotentOptions(opts))
}

// Preview previews a CSV import: it parses, maps and validates the file
// exactly as [ImportsService.Commit] would, and writes nothing.
//
// Never retried, although it writes nothing; repeat the call to try again.
//
// Needs the ingest scope. Operation previewImport.
func (s *ImportsService) Preview(ctx context.Context, upload ImportUpload, opts ...RequestOption) (*ImportPreview, error) {
	cl, err := opCall("previewImport")
	cl.upload = &upload
	return send[ImportPreview](ctx, s.client, cl, err, idempotentOptions(opts))
}

// Commit imports a CSV file: it stores the file's valid rows and records
// an import batch. A problem with one row rejects that row only. The
// result lists the problems of the rejected rows, one entry of Errors per
// problem, up to the first 1,000; ErrorsTruncated says when more were
// omitted, and the counts always cover the whole file. Events deduplicate
// as they do on ingest.
//
// Every attempt of the call sends one Idempotency-Key, so a retry cannot
// record a second import batch.
//
// Needs the ingest scope. Operation commitImport.
func (s *ImportsService) Commit(ctx context.Context, upload ImportUpload, opts ...IdempotentRequestOption) (*ImportResult, error) {
	cl, err := opCall("commitImport")
	cl.upload = &upload
	return send[ImportResult](ctx, s.client, cl, err, opts)
}

// ClientsService groups the operations on clients, the partner's own
// customers. Reach it as client.Clients.
type ClientsService struct {
	client *Client
}

// List lists clients, and returns the first page. Walk every client of
// every page with [Page.All]. A nil query sends none.
//
// Set Page for a numbered page: the page's Body then holds Page and Total.
// Only that first page is counted; every later page of a walk continues by
// cursor from it, and its Body.Page and Body.Total are nil. To show another
// numbered page, call List again with its Page rather than
// [Page.NextPage]. A page number past the end has no items, and the walk
// ends there.
//
// Needs the read scope. Operation listClients.
func (s *ClientsService) List(ctx context.Context, query *ListClientsParams, opts ...RequestOption) (*Page[ClientRecord, ClientList], error) {
	cl, err := opCall("listClients")
	return sendList(ctx, s.client, cl, err, query, opts, func(b *ClientList) []ClientRecord { return b.Data })
}

// Create creates a client.
//
// A RenewalDate before the request's UTC date less one day is refused with
// 400 out_of_range, so no time zone's today is refused. With a RenewalTerm,
// an active client's renewal date rolls forward by whole terms once it
// passes.
//
// ClientKey is unique in the workspace, ignoring letter case and
// surrounding whitespace: a key another client already holds, in any case
// or padding, is refused with 409 client_key_in_use.
//
// Every attempt of the call sends one Idempotency-Key, so a retry cannot
// create a second client.
//
// Needs the write scope. Operation createClient.
func (s *ClientsService) Create(ctx context.Context, body ClientCreate, opts ...IdempotentRequestOption) (*ClientRecord, error) {
	cl, err := opCall("createClient")
	cl.body = body
	return send[ClientRecord](ctx, s.client, cl, err, opts)
}

// Get reads a client.
//
// Needs the read scope. Operation getClient.
func (s *ClientsService) Get(ctx context.Context, id string, opts ...RequestOption) (*ClientDetail, error) {
	cl, err := opCall("getClient", id)
	return send[ClientDetail](ctx, s.client, cl, err, idempotentOptions(opts))
}

// Update updates a client with a merge patch: a member left out keeps its
// value, a [Null] clears it, and any other value sets it.
//
// Sending back the RenewalDate a read returned keeps the client's renewal
// schedule: a date on the schedule, up to the one it shows on the next UTC
// day, changes nothing. Any other date is a new renewal date, and one
// before the request's UTC date less one day is refused with 400
// out_of_range. A patch that changes RenewalTerm or Status saves the date
// the client shows on the current UTC day as the new start of its
// schedule, without the patch sending a date.
//
// ClientKey is unique in the workspace, ignoring letter case and
// surrounding whitespace: a key another client already holds, in any case
// or padding, is refused with 409 client_key_in_use.
//
// Never retried.
//
// Needs the write scope. Operation updateClient.
func (s *ClientsService) Update(ctx context.Context, id string, patch ClientPatch, opts ...RequestOption) (*ClientRecord, error) {
	cl, err := opCall("updateClient", id)
	cl.body = patch
	return send[ClientRecord](ctx, s.client, cl, err, idempotentOptions(opts))
}

// Delete deletes a client. It is refused while events carry the client's
// key, or while the client has a validated or published calculation; set
// its status to churned instead.
//
// A retry after a lost response can answer 404 for a delete that
// succeeded.
//
// Needs the write scope. Operation deleteClient.
func (s *ClientsService) Delete(ctx context.Context, id string, opts ...RequestOption) error {
	cl, err := opCall("deleteClient", id)
	return sendEmpty(ctx, s.client, cl, err, idempotentOptions(opts))
}

// SetAgents replaces a client's assigned agents. Assignment decides how
// later events are attributed: an event without client_key from an agent
// assigned to exactly one client is attributed to that client. A model or
// outcome event from an agent assigned to two or more clients must carry
// client_key, or it is refused with client_key_required.
//
// Retried, because a repeat leaves the same assignment.
//
// Needs the write scope. Operation setClientAgents.
func (s *ClientsService) SetAgents(ctx context.Context, id string, body ClientAgentsSet, opts ...RequestOption) (*ClientAgents, error) {
	cl, err := opCall("setClientAgents", id)
	cl.body = body
	return send[ClientAgents](ctx, s.client, cl, err, idempotentOptions(opts))
}

// AgentsService groups the operations on agents. Reach it as
// client.Agents.
type AgentsService struct {
	client *Client
}

// List lists agents, and returns the first page. Walk every agent of every
// page with [Page.All]. A nil query sends none.
//
// Set Page for a numbered page: the page's Body then holds Page and Total.
// Only that first page is counted; every later page of a walk continues by
// cursor from it, and its Body.Page and Body.Total are nil. To show another
// numbered page, call List again with its Page rather than
// [Page.NextPage]. A page number past the end has no items, and the walk
// ends there.
//
// Needs the read scope. Operation listAgents.
func (s *AgentsService) List(ctx context.Context, query *ListAgentsParams, opts ...RequestOption) (*Page[AgentListItem, AgentList], error) {
	cl, err := opCall("listAgents")
	return sendList(ctx, s.client, cl, err, query, opts, func(b *AgentList) []AgentListItem { return b.Data })
}

// Create creates an agent.
//
// Every attempt of the call sends one Idempotency-Key, so a retry cannot
// create a second agent.
//
// Needs the write scope. Operation createAgent.
func (s *AgentsService) Create(ctx context.Context, body AgentCreate, opts ...IdempotentRequestOption) (*Agent, error) {
	cl, err := opCall("createAgent")
	cl.body = body
	return send[Agent](ctx, s.client, cl, err, opts)
}

// Get reads an agent, with its statistics. query.From and query.To bound
// the statistics only; absent bounds, or a nil query, mean all time.
//
// Needs the read scope. Operation getAgent.
func (s *AgentsService) Get(ctx context.Context, id string, query *GetAgentParams, opts ...RequestOption) (*AgentDetail, error) {
	cl, err := opCall("getAgent", id)
	cl.query = query
	return send[AgentDetail](ctx, s.client, cl, err, idempotentOptions(opts))
}

// Update updates an agent with a merge patch: a member left out keeps its
// value, a [Null] clears it, and any other value sets it.
//
// Never retried.
//
// Needs the write scope. Operation updateAgent.
func (s *AgentsService) Update(ctx context.Context, id string, patch AgentPatch, opts ...RequestOption) (*Agent, error) {
	cl, err := opCall("updateAgent", id)
	cl.body = patch
	return send[Agent](ctx, s.client, cl, err, idempotentOptions(opts))
}

// Delete deletes an agent. It is refused while events reference the
// agent; set its status to retired instead.
//
// A retry after a lost response can answer 404 for a delete that
// succeeded.
//
// Needs the write scope. Operation deleteAgent.
func (s *AgentsService) Delete(ctx context.Context, id string, opts ...RequestOption) error {
	cl, err := opCall("deleteAgent", id)
	return sendEmpty(ctx, s.client, cl, err, idempotentOptions(opts))
}

// ValueModelsService groups the operations on value models and value
// templates. Reach it as client.ValueModels.
//
// A method whose name ends in ForClient takes a client's id, and acts on
// that client's active value model; [ValueModelsService.Get] takes a value
// model's own id.
type ValueModelsService struct {
	client *Client
}

// List lists value models, and returns the first page. Walk every value
// model of every page with [Page.All]. A nil query sends none.
//
// Set Page for a numbered page: the page's Body then holds Page and Total.
// Only that first page is counted; every later page of a walk continues by
// cursor from it, and its Body.Page and Body.Total are nil. To show another
// numbered page, call List again with its Page rather than
// [Page.NextPage]. A page number past the end has no items, and the walk
// ends there.
//
// Needs the read scope. Operation listValueModels.
func (s *ValueModelsService) List(ctx context.Context, query *ListValueModelsParams, opts ...RequestOption) (*Page[ValueModelSummary, ValueModelList], error) {
	cl, err := opCall("listValueModels")
	return sendList(ctx, s.client, cl, err, query, opts, func(b *ValueModelList) []ValueModelSummary { return b.Data })
}

// Get reads a value model by its own id, not its client's: any model in
// the workspace, archived ones included.
//
// Needs the read scope. Operation getValueModel.
func (s *ValueModelsService) Get(ctx context.Context, id string, opts ...RequestOption) (*ValueModel, error) {
	cl, err := opCall("getValueModel", id)
	return send[ValueModel](ctx, s.client, cl, err, idempotentOptions(opts))
}

// GetForClient reads a client's active value model.
//
// Needs the read scope. Operation getClientValueModel.
func (s *ValueModelsService) GetForClient(ctx context.Context, clientID string, opts ...RequestOption) (*ValueModel, error) {
	cl, err := opCall("getClientValueModel", clientID)
	return send[ValueModel](ctx, s.client, cl, err, idempotentOptions(opts))
}

// CreateForClient creates a client's value model.
//
// Every attempt of the call sends one Idempotency-Key, so a retry cannot
// create a second value model.
//
// Needs the write scope. Operation createValueModel.
func (s *ValueModelsService) CreateForClient(ctx context.Context, clientID string, body ValueModelCreate, opts ...IdempotentRequestOption) (*ValueModel, error) {
	cl, err := opCall("createValueModel", clientID)
	cl.body = body
	return send[ValueModel](ctx, s.client, cl, err, opts)
}

// UpdateForClient updates a client's active value model with a merge
// patch: a member left out keeps its value, and any other value sets it.
//
// Never retried.
//
// Needs the write scope. Operation updateValueModel.
func (s *ValueModelsService) UpdateForClient(ctx context.Context, clientID string, patch ValueModelPatch, opts ...RequestOption) (*ValueModel, error) {
	cl, err := opCall("updateValueModel", clientID)
	cl.body = patch
	return send[ValueModel](ctx, s.client, cl, err, idempotentOptions(opts))
}

// DeleteForClient deletes a client's active value model.
//
// A retry after a lost response can answer 404 for a delete that
// succeeded.
//
// Needs the write scope. Operation deleteValueModel.
func (s *ValueModelsService) DeleteForClient(ctx context.Context, clientID string, opts ...RequestOption) error {
	cl, err := opCall("deleteValueModel", clientID)
	return sendEmpty(ctx, s.client, cl, err, idempotentOptions(opts))
}

// SetLineItemsForClient replaces the line items of a client's active value
// model, as a whole set. It is guarded by body.Version: a stale version is
// refused rather than overwriting a co-editor's change. Read the current
// version with [ValueModelsService.GetForClient].
//
// Never retried: a repeat of a write that landed would be refused as
// stale. After a network error the write may or may not have landed, so
// re-read the value model's Version with [ValueModelsService.GetForClient]
// before trying again.
//
// Needs the write scope. Operation setValueModelLineItems.
func (s *ValueModelsService) SetLineItemsForClient(ctx context.Context, clientID string, body ValueLineItemsSet, opts ...RequestOption) (*ValueModel, error) {
	cl, err := opCall("setValueModelLineItems", clientID)
	cl.body = body
	return send[ValueModel](ctx, s.client, cl, err, idempotentOptions(opts))
}

// ListTemplates lists value templates, and returns the first page. Walk
// every template of every page with [Page.All]. A nil query sends none.
//
// Needs the read scope. Operation listValueTemplates.
func (s *ValueModelsService) ListTemplates(ctx context.Context, query *ListValueTemplatesParams, opts ...RequestOption) (*Page[ValueTemplate, ValueTemplateList], error) {
	cl, err := opCall("listValueTemplates")
	return sendList(ctx, s.client, cl, err, query, opts, func(b *ValueTemplateList) []ValueTemplate { return b.Data })
}

// OutcomeTypesService groups the operations on outcome types. Reach it as
// client.OutcomeTypes.
type OutcomeTypesService struct {
	client *Client
}

// List lists the workspace's outcome types, and returns the first page.
// Walk every outcome type of every page with [Page.All]. A nil query sends
// none.
//
// Needs the read scope. Operation listOutcomeTypes.
func (s *OutcomeTypesService) List(ctx context.Context, query *ListOutcomeTypesParams, opts ...RequestOption) (*Page[OutcomeTypeUsage, OutcomeTypeList], error) {
	cl, err := opCall("listOutcomeTypes")
	return sendList(ctx, s.client, cl, err, query, opts, func(b *OutcomeTypeList) []OutcomeTypeUsage { return b.Data })
}

// ListForClient lists a client's outcome types, and returns the first
// page. Walk every outcome type of every page with [Page.All]. A nil query
// sends none.
//
// Needs the read scope. Operation listClientOutcomeTypes.
func (s *OutcomeTypesService) ListForClient(ctx context.Context, clientID string, query *ListClientOutcomeTypesParams, opts ...RequestOption) (*Page[OutcomeTypeUsage, OutcomeTypeList], error) {
	cl, err := opCall("listClientOutcomeTypes", clientID)
	return sendList(ctx, s.client, cl, err, query, opts, func(b *OutcomeTypeList) []OutcomeTypeUsage { return b.Data })
}

// CostService groups the operations on cost line items and the price
// table. Reach it as client.Cost.
type CostService struct {
	client *Client
}

// ListLineItems lists cost line items, and returns the first page. Walk
// every line item of every page with [Page.All]. A nil query sends none.
//
// Set Page for a numbered page: the page's Body then holds Page and Total.
// Only that first page is counted; every later page of a walk continues by
// cursor from it, and its Body.Page and Body.Total are nil. To show another
// numbered page, call ListLineItems again with its Page rather than
// [Page.NextPage]. A page number past the end has no items, and the walk
// ends there.
//
// Needs the read scope. Operation listCostLineItems.
func (s *CostService) ListLineItems(ctx context.Context, query *ListCostLineItemsParams, opts ...RequestOption) (*Page[CostLineItem, CostLineItemList], error) {
	cl, err := opCall("listCostLineItems")
	return sendList(ctx, s.client, cl, err, query, opts, func(b *CostLineItemList) []CostLineItem { return b.Data })
}

// CreateLineItem creates a cost line item.
//
// Every attempt of the call sends one Idempotency-Key, so a retry cannot
// create a second line item.
//
// Needs the write scope. Operation createCostLineItem.
func (s *CostService) CreateLineItem(ctx context.Context, body CostLineItemCreate, opts ...IdempotentRequestOption) (*CostLineItem, error) {
	cl, err := opCall("createCostLineItem")
	cl.body = body
	return send[CostLineItem](ctx, s.client, cl, err, opts)
}

// GetLineItem reads a cost line item.
//
// Needs the read scope. Operation getCostLineItem.
func (s *CostService) GetLineItem(ctx context.Context, id string, opts ...RequestOption) (*CostLineItem, error) {
	cl, err := opCall("getCostLineItem", id)
	return send[CostLineItem](ctx, s.client, cl, err, idempotentOptions(opts))
}

// UpdateLineItem updates a cost line item with a merge patch: a member
// left out keeps its value, a [Null] clears it, and any other value sets
// it.
//
// Never retried.
//
// Needs the write scope. Operation updateCostLineItem.
func (s *CostService) UpdateLineItem(ctx context.Context, id string, patch CostLineItemPatch, opts ...RequestOption) (*CostLineItem, error) {
	cl, err := opCall("updateCostLineItem", id)
	cl.body = patch
	return send[CostLineItem](ctx, s.client, cl, err, idempotentOptions(opts))
}

// DeleteLineItem deletes a cost line item.
//
// A retry after a lost response can answer 404 for a delete that
// succeeded.
//
// Needs the write scope. Operation deleteCostLineItem.
func (s *CostService) DeleteLineItem(ctx context.Context, id string, opts ...RequestOption) error {
	cl, err := opCall("deleteCostLineItem", id)
	return sendEmpty(ctx, s.client, cl, err, idempotentOptions(opts))
}

// GetPriceTable reads the price table.
//
// Needs the read scope. Operation getPriceTable.
func (s *CostService) GetPriceTable(ctx context.Context, opts ...RequestOption) (*PriceTable, error) {
	cl, err := opCall("getPriceTable")
	return send[PriceTable](ctx, s.client, cl, err, idempotentOptions(opts))
}

// SetPriceTable replaces the price table, as a whole. Each rate is a
// [Decimal], so the rates of a table read with [CostService.GetPriceTable]
// are sent back with the same digits.
//
// Retried, because a repeat leaves the same price table.
//
// Needs the write scope. Operation setPriceTable.
func (s *CostService) SetPriceTable(ctx context.Context, body PriceTableSet, opts ...RequestOption) (*PriceTable, error) {
	cl, err := opCall("setPriceTable")
	cl.body = body
	return send[PriceTable](ctx, s.client, cl, err, idempotentOptions(opts))
}

// AnalyticsService groups the operations that compute a client's figures.
// Reach it as client.Analytics.
type AnalyticsService struct {
	client *Client
}

// GetValueForClient computes a client's value. A nil query, or absent
// bounds, mean all time.
//
// Needs the read scope. Operation getClientValue.
func (s *AnalyticsService) GetValueForClient(ctx context.Context, clientID string, query *GetClientValueParams, opts ...RequestOption) (*ClientValue, error) {
	cl, err := opCall("getClientValue", clientID)
	cl.query = query
	return send[ClientValue](ctx, s.client, cl, err, idempotentOptions(opts))
}

// GetCostForClient computes a client's cost over a period. Both bounds are
// required: standing charges are prorated day by day, so an open From
// would prorate each charge from its own start date, however long ago.
//
// Needs the read scope. Operation getClientCost.
func (s *AnalyticsService) GetCostForClient(ctx context.Context, clientID string, query GetClientCostParams, opts ...RequestOption) (*ClientCost, error) {
	cl, err := opCall("getClientCost", clientID)
	cl.query = &query
	return send[ClientCost](ctx, s.client, cl, err, idempotentOptions(opts))
}

// GetRoiForClient computes a client's ROI over a period, whose bounds are
// required.
//
// Needs the read scope. Operation getClientRoi.
func (s *AnalyticsService) GetRoiForClient(ctx context.Context, clientID string, query GetClientRoiParams, opts ...RequestOption) (*RoiResult, error) {
	cl, err := opCall("getClientRoi", clientID)
	cl.query = &query
	return send[RoiResult](ctx, s.client, cl, err, idempotentOptions(opts))
}

// GetAnomaliesForClient flags anomalies in a client's period, whose bounds
// are required.
//
// Needs the read scope. Operation getClientAnomalies.
func (s *AnalyticsService) GetAnomaliesForClient(ctx context.Context, clientID string, query GetClientAnomaliesParams, opts ...RequestOption) (*Anomalies, error) {
	cl, err := opCall("getClientAnomalies", clientID)
	cl.query = &query
	return send[Anomalies](ctx, s.client, cl, err, idempotentOptions(opts))
}

// CalculationsService groups the operations on calculations, a client's
// ROI frozen for a period. Reach it as client.Calculations.
type CalculationsService struct {
	client *Client
}

// List lists the workspace's calculations across clients, without
// snapshots, and returns the first page. Walk every calculation of every
// page with [Page.All]. A nil query sends none. Month takes YYYY-MM and
// matches only a period that is exactly that calendar month in UTC.
//
// Calculations come latest period first, then newest first. IsCurrent is
// decided among each client's own calculations for the period, before any
// filter or page applies.
//
// Set Page for a numbered page: the page's Body then holds Page and Total.
// Only that first page is counted; every later page of a walk continues by
// cursor from it, and its Body.Page and Body.Total are nil. To show another
// numbered page, call List again with its Page rather than
// [Page.NextPage]. A page number past the end has no items, and the walk
// ends there.
//
// Needs the read scope. Operation listCalculations.
func (s *CalculationsService) List(ctx context.Context, query *ListCalculationsParams, opts ...RequestOption) (*Page[CalculationSummary, WorkspaceCalculationList], error) {
	cl, err := opCall("listCalculations")
	return sendList(ctx, s.client, cl, err, query, opts, func(b *WorkspaceCalculationList) []CalculationSummary { return b.Data })
}

// CreateForClient freezes a client's ROI for an ended period as a draft
// calculation. An API key with the write scope may create a draft;
// validating and publishing it need a signed-in user, and this SDK does
// not offer them.
//
// Every attempt of the call sends one Idempotency-Key, so a retry cannot
// create a second calculation.
//
// Needs the write scope. Operation createCalculation.
func (s *CalculationsService) CreateForClient(ctx context.Context, clientID string, body CalculationCreate, opts ...IdempotentRequestOption) (*Calculation, error) {
	cl, err := opCall("createCalculation", clientID)
	cl.body = body
	return send[Calculation](ctx, s.client, cl, err, opts)
}

// ListForClient lists a client's calculations, and returns the first page.
// Walk every calculation of every page with [Page.All]. A nil query sends
// none.
//
// Needs the read scope. Operation listClientCalculations.
func (s *CalculationsService) ListForClient(ctx context.Context, clientID string, query *ListClientCalculationsParams, opts ...RequestOption) (*Page[CalculationSummary, CalculationList], error) {
	cl, err := opCall("listClientCalculations", clientID)
	return sendList(ctx, s.client, cl, err, query, opts, func(b *CalculationList) []CalculationSummary { return b.Data })
}

// Get reads a calculation.
//
// Needs the read scope. Operation getCalculation.
func (s *CalculationsService) Get(ctx context.Context, id string, opts ...RequestOption) (*Calculation, error) {
	cl, err := opCall("getCalculation", id)
	return send[Calculation](ctx, s.client, cl, err, idempotentOptions(opts))
}

// Delete deletes a draft calculation.
//
// A retry after a lost response can answer 404 for a delete that
// succeeded.
//
// Needs the write scope. Operation deleteCalculation.
func (s *CalculationsService) Delete(ctx context.Context, id string, opts ...RequestOption) error {
	cl, err := opCall("deleteCalculation", id)
	return sendEmpty(ctx, s.client, cl, err, idempotentOptions(opts))
}

// ReportsService groups the operations on reports. Reach it as
// client.Reports.
type ReportsService struct {
	client *Client
}

// List lists the workspace's reports across calculations, newest first,
// each without its disclosures, and returns the first page. Walk every
// report of every page with [Page.All]. A nil query sends none.
//
// Set Page for a numbered page: the page's Body then holds Page and Total.
// Only that first page is counted; every later page of a walk continues by
// cursor from it, and its Body.Page and Body.Total are nil. To show another
// numbered page, call List again with its Page rather than
// [Page.NextPage]. A page number past the end has no items, and the walk
// ends there.
//
// Needs the read scope. Operation listReports.
func (s *ReportsService) List(ctx context.Context, query *ListReportsParams, opts ...RequestOption) (*Page[ReportSummary, WorkspaceReportList], error) {
	cl, err := opCall("listReports")
	return sendList(ctx, s.client, cl, err, query, opts, func(b *WorkspaceReportList) []ReportSummary { return b.Data })
}

// CreateForCalculation composes a report on a published calculation, with
// its disclosures taken from the calculation's coverage. It sends no
// request body.
//
// Every attempt of the call sends one Idempotency-Key, so a retry cannot
// create a second report.
//
// Needs the write scope. Operation createReport.
func (s *ReportsService) CreateForCalculation(ctx context.Context, calculationID string, opts ...IdempotentRequestOption) (*Report, error) {
	cl, err := opCall("createReport", calculationID)
	return send[Report](ctx, s.client, cl, err, opts)
}

// ListForCalculation lists a calculation's reports, and returns the first
// page. Walk every report of every page with [Page.All]. A nil query sends
// none. Each item is a [ReportSummary], without disclosures; [ReportsService.Get]
// reads a report with them.
//
// Needs the read scope. Operation listCalculationReports.
func (s *ReportsService) ListForCalculation(ctx context.Context, calculationID string, query *ListCalculationReportsParams, opts ...RequestOption) (*Page[ReportSummary, ReportList], error) {
	cl, err := opCall("listCalculationReports", calculationID)
	return sendList(ctx, s.client, cl, err, query, opts, func(b *ReportList) []ReportSummary { return b.Data })
}

// Get reads a report.
//
// Needs the read scope. Operation getReport.
func (s *ReportsService) Get(ctx context.Context, id string, opts ...RequestOption) (*ReportWithPreview, error) {
	cl, err := opCall("getReport", id)
	return send[ReportWithPreview](ctx, s.client, cl, err, idempotentOptions(opts))
}

// Update edits a report with a merge patch: a member left out keeps its
// value, a [Null] clears it, and any other value sets it. A slice, such as
// Disclosures, replaces the stored one whole.
//
// Never retried.
//
// Needs the write scope. Operation updateReport.
func (s *ReportsService) Update(ctx context.Context, id string, patch ReportPatch, opts ...RequestOption) (*Report, error) {
	cl, err := opCall("updateReport", id)
	cl.body = patch
	return send[Report](ctx, s.client, cl, err, idempotentOptions(opts))
}

// Delete deletes an unshared report. A shared report cannot be deleted
// until a signed-in user revokes its link, which this SDK does not offer.
//
// A retry after a lost response can answer 404 for a delete that
// succeeded.
//
// Needs the write scope. Operation deleteReport.
func (s *ReportsService) Delete(ctx context.Context, id string, opts ...RequestOption) error {
	cl, err := opCall("deleteReport", id)
	return sendEmpty(ctx, s.client, cl, err, idempotentOptions(opts))
}

// DashboardService groups the operations on the workspace's dashboard.
// Reach it as client.Dashboard.
type DashboardService struct {
	client *Client
}

// Get reads the workspace's month: its published figures for the calendar
// month query.Month, such as "2026-08", which is required.
//
// Needs the read scope. Operation getDashboard.
func (s *DashboardService) Get(ctx context.Context, query GetDashboardParams, opts ...RequestOption) (*Dashboard, error) {
	cl, err := opCall("getDashboard")
	cl.query = &query
	return send[Dashboard](ctx, s.client, cl, err, idempotentOptions(opts))
}

// ListRenewals lists renewal readiness: each client's latest published
// figures, whatever month [DashboardService.Get] reads. It returns the
// first page; walk every client of every page with [Page.All]. A nil query
// sends none.
//
// Today is the date renewal dates are rolled against and Window is
// measured from; left nil, it is the current UTC date, which Body.Today
// echoes. A walk sends the first page's Today on every later page, so it
// reads one date even across midnight UTC; to page by number instead, set
// that Today yourself with each Page. RenewalTerm is sent as one parameter
// per item; a non-nil empty slice is refused before sending, because
// leaving it nil means every term.
//
// Needs the read scope. Operation listRenewals.
func (s *DashboardService) ListRenewals(ctx context.Context, query *ListRenewalsParams, opts ...RequestOption) (*Page[Renewal, RenewalList], error) {
	cl, err := opCall("listRenewals")
	return sendList(ctx, s.client, cl, err, query, opts, func(b *RenewalList) []Renewal { return b.Data })
}

// HealthService groups the liveness check. Reach it as client.Health.
type HealthService struct {
	client *Client
}

// Get checks liveness. It sends no credential, and does not call the
// client's token provider.
//
// Operation getHealth.
func (s *HealthService) Get(ctx context.Context, opts ...RequestOption) (*Health, error) {
	cl, err := opCall("getHealth")
	return send[Health](ctx, s.client, cl, err, idempotentOptions(opts))
}

// WorkspaceService groups the operation on the workspace the credential
// belongs to. Reach it as client.Workspace.
type WorkspaceService struct {
	client *Client
}

// Get reads the workspace the credential belongs to: its name, its
// currency and when it was created.
//
// Needs the read scope. Operation getWorkspace.
func (s *WorkspaceService) Get(ctx context.Context, opts ...RequestOption) (*Workspace, error) {
	cl, err := opCall("getWorkspace")
	return send[Workspace](ctx, s.client, cl, err, idempotentOptions(opts))
}

// OnboardingService groups the operations on the guided setup's progress.
// Reach it as client.Onboarding.
type OnboardingService struct {
	client *Client
}

// Get reads the guided setup's progress: the five steps in their fixed
// order, each with whether the workspace's data completes it and when it
// was skipped, and CurrentStep, the first step neither completed nor
// skipped. This is the read for the guided setup's progress.
//
// Needs the read scope. Operation getOnboarding.
func (s *OnboardingService) Get(ctx context.Context, opts ...RequestOption) (*Onboarding, error) {
	cl, err := opCall("getOnboarding")
	return send[Onboarding](ctx, s.client, cl, err, idempotentOptions(opts))
}

// SetSkippedSteps replaces the set of skipped steps, and returns the whole
// progress. body.Steps replaces the whole set: a step left out is no
// longer skipped, and a step already skipped keeps its SkippedAt.
//
// To clear every skip, pass an empty, non-nil slice, such as
// []OnboardingStep{}. A nil Steps is sent as null, which the API refuses
// with 400.
//
// Retried, because a repeat leaves the same set.
//
// Needs the write scope. Operation setOnboardingSkippedSteps.
func (s *OnboardingService) SetSkippedSteps(ctx context.Context, body OnboardingSkippedStepsSet, opts ...RequestOption) (*Onboarding, error) {
	cl, err := opCall("setOnboardingSkippedSteps")
	cl.body = body
	return send[Onboarding](ctx, s.client, cl, err, idempotentOptions(opts))
}

// Format prints the service for every fmt verb without the client it
// belongs to, which holds the credential.
func (s ImportsService) Format(f fmt.State, _ rune) { formatService(f, "ImportsService") }

// Format prints the service for every fmt verb without the client it
// belongs to, which holds the credential.
func (s ClientsService) Format(f fmt.State, _ rune) { formatService(f, "ClientsService") }

// Format prints the service for every fmt verb without the client it
// belongs to, which holds the credential.
func (s AgentsService) Format(f fmt.State, _ rune) { formatService(f, "AgentsService") }

// Format prints the service for every fmt verb without the client it
// belongs to, which holds the credential.
func (s ValueModelsService) Format(f fmt.State, _ rune) { formatService(f, "ValueModelsService") }

// Format prints the service for every fmt verb without the client it
// belongs to, which holds the credential.
func (s OutcomeTypesService) Format(f fmt.State, _ rune) { formatService(f, "OutcomeTypesService") }

// Format prints the service for every fmt verb without the client it
// belongs to, which holds the credential.
func (s CostService) Format(f fmt.State, _ rune) { formatService(f, "CostService") }

// Format prints the service for every fmt verb without the client it
// belongs to, which holds the credential.
func (s AnalyticsService) Format(f fmt.State, _ rune) { formatService(f, "AnalyticsService") }

// Format prints the service for every fmt verb without the client it
// belongs to, which holds the credential.
func (s CalculationsService) Format(f fmt.State, _ rune) { formatService(f, "CalculationsService") }

// Format prints the service for every fmt verb without the client it
// belongs to, which holds the credential.
func (s ReportsService) Format(f fmt.State, _ rune) { formatService(f, "ReportsService") }

// Format prints the service for every fmt verb without the client it
// belongs to, which holds the credential.
func (s DashboardService) Format(f fmt.State, _ rune) { formatService(f, "DashboardService") }

// Format prints the service for every fmt verb without the client it
// belongs to, which holds the credential.
func (s HealthService) Format(f fmt.State, _ rune) { formatService(f, "HealthService") }

// formatService prints a service as its name alone, never the client it
// belongs to, which holds the credential.
func formatService(f fmt.State, name string) {
	_, _ = fmt.Fprintf(f, "allisall.%s{}", name)
}

// Format prints the service for every fmt verb without the client it
// belongs to, which holds the credential.
func (s WorkspaceService) Format(f fmt.State, _ rune) { formatService(f, "WorkspaceService") }

// Format prints the service for every fmt verb without the client it
// belongs to, which holds the credential.
func (s OnboardingService) Format(f fmt.State, _ rune) { formatService(f, "OnboardingService") }
