package allisall

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
)

// These tests hold the service methods: one per operation, each named by
// the naming rule, each sending its operation's request and returning its
// decoded answer.

// exactMoney is a Money value a float64 cannot hold: it reaches the caller
// with these digits only if it is decoded from the number's text.
const exactMoney = "0.1234567890123456789"

var (
	periodFrom = time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	periodTo   = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
)

// periodQuery is periodFrom and periodTo as a query sends them.
const periodQuery = "from=2026-09-01T00%3A00%3A00Z&to=2026-10-01T00%3A00%3A00Z"

// testPage is a *Page of any type, as a row's run returns it.
type testPage interface {
	parts() (data, body any)
	walk(ctx context.Context) (int, error)
}

func (p *Page[T, B]) parts() (data, body any) { return p.Data, p.Body }

// walk counts the items All yields, and returns the first error.
func (p *Page[T, B]) walk(ctx context.Context) (int, error) {
	n := 0
	for _, err := range p.All(ctx) {
		if err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// methodRow is one service method: the call a caller makes, the request it
// must send, and the response it reads.
type methodRow struct {
	// id is the operationId; method is the method as Group.Method.
	id, method string
	run        func(ctx context.Context, c *Client) (any, error)
	// verb and url are the request line, the URL after the base.
	verb, url string
	// body is the exact body sent, "" for none. For a multipart body, each
	// of form's strings must appear in it.
	body string
	form []string
	// resp is the response body; "" for a 204.
	resp string
	// check replaces the comparison of the result with resp, for a method
	// whose result is not the response's own type.
	check func(t *testing.T, result any)
}

const (
	clientJSON         = `{"client_key":"acme","created_at":"2026-10-28T12:00:00.123456Z","id":"client-1","name":"Acme","notes":"","renewal_date":null,"status":"active"}`
	agentJSON          = `{"agent_key":"bot","created_at":"2026-10-28T12:00:00Z","framework":"","id":"agent-1","name":"Bot","status":"active","version":""}`
	modelJSON          = `{"client_id":"client-1","created_at":"2026-10-28T12:00:00Z","currency":"USD","id":"model-1","line_items":[{"id":"line-1","label":"Ticket","outcome_type":"ticket_resolved","position":1,"provenance_note":"","unit_value":` + exactMoney + `}],"name":"Support","status":"active","updated_at":"2026-10-28T12:00:00Z","version":3}`
	lineJSON           = `{"created_at":"2026-10-28T12:00:00Z","currency":"USD","end_date":null,"id":"line-1","label":"Hosting","monthly_amount":` + exactMoney + `,"start_date":"2026-09-01","target":{"id":"client-1","kind":"client","name":"Acme"},"updated_at":"2026-10-28T12:00:00Z"}`
	calcJSON           = `{"client_id":"client-1","cost_total":` + exactMoney + `,"created_at":"2026-10-28T12:00:00Z","currency":"USD","id":"calc-1","minor_units":2,"period":{"from":"2026-09-01T00:00:00Z","to":"2026-10-01T00:00:00Z"},"published_at":null,"ratios":{"roi":1.5,"undefined_reason":null,"value_to_cost":2.5},"state":"draft","validated_at":null,"value_total":null}`
	workspaceEventJSON = `{"agent_id":"agent-1","agent_name":"Bot","category":"task","client_id":"client-1","client_key":null,"client_name":"Acme","cost":null,"created_at":"2026-10-01T12:00:01Z","event_type":"triage","id":"event-1","import_batch_id":"import-1","quantity":1,"source":"csv","timestamp":"2026-09-02T00:00:00Z"}`
	importJSON         = `{"accepted":2,"created_at":"2026-10-28T12:00:00Z","duplicates":1,"filename":"august.csv","id":"import-1","mapping":{"columns":{"agent_key":"Agent","timestamp":"Time"},"constants":{"category":"task"},"delimiter":";","timezone":"Asia/Tokyo"},"rejected":0,"total_rows":3}`
	reportJSON         = `{"calculation_id":"calc-1","client_id":"client-1","client_name":"Acme Logistics","created_at":"2026-10-28T12:00:00Z","disclosures":[],"id":"report-1","letterhead_client_name":"Acme","letterhead_workspace_name":"Partner","note":"","period":{"from":"2026-09-01T00:00:00Z","to":"2026-10-01T00:00:00Z"},"share_token":null,"state":"draft"}`
	// summaryJSON is a report as the list returns it, without disclosures.
	summaryJSON = `{"calculation_id":"calc-1","client_id":"client-1","client_name":"Acme Logistics","created_at":"2026-10-28T12:00:00Z","id":"report-1","letterhead_client_name":"Acme","letterhead_workspace_name":"Partner","note":"","period":{"from":"2026-09-01T00:00:00Z","to":"2026-10-01T00:00:00Z"},"share_token":null,"state":"draft"}`
	tableJSON   = `{"currency":"USD","models":[{"input_per_1k":` + exactMoney + `,"model":"gpt","output_per_1k":12}]}`
	lastPage    = `"has_more":false,"next_cursor":null`
)

// listJSON is a last page holding item.
func listJSON(item string) string { return `{"data":[` + item + `],` + lastPage + `}` }

// methodRows returns one row per operation the SDK calls.
func methodRows() []methodRow {
	money := MustParseDecimal(exactMoney)
	from, to := periodFrom, periodTo
	return []methodRow{
		{
			id: "ingestEvents", method: "Events.Ingest",
			run: func(ctx context.Context, c *Client) (any, error) {
				return c.Events.Ingest(ctx, []EventInput{{EventID: "e-1", Timestamp: fakeNow, AgentKey: "agent-1", Category: EventCategoryTask, EventType: "ticket_resolved"}})
			},
			verb: "POST", url: "/v1/events",
			body: `{"events":[{"event_id":"e-1","timestamp":"2026-10-28T12:00:00Z","agent_key":"agent-1","category":"task","event_type":"ticket_resolved"}]}`,
			resp: `{"accepted":1,"duplicates":0,"rejected":0,"errors":[],"errors_truncated":false}`,
			check: func(t *testing.T, result any) {
				s, ok := result.(*IngestSummary)
				if !ok || s.Accepted != 1 || s.Batches != 1 {
					t.Errorf("summary = %+v, want one accepted event in one batch", result)
				}
			},
		},
		{
			id: "listClientEvents", method: "Events.ListForClient",
			run: func(ctx context.Context, c *Client) (any, error) {
				return c.Events.ListForClient(ctx, "client-17", ListClientEventsParams{From: from, To: to, Category: new(EventCategoryOutcome), Limit: new(5)})
			},
			verb: "GET", url: "/v1/clients/client-17/events?" + periodQuery + "&category=outcome&limit=5",
			resp: `{"client_id":"client-17","currency":"USD","data":[{"agent_id":"agent-1","agent_name":"Bot","category":"outcome","cost":` + exactMoney +
				`,"event_type":"ticket_resolved","id":"event-1","quantity":2,"source":"api","timestamp":"2026-09-02T00:00:00.000001Z"}],` + lastPage +
				`,"page":null,"period":{"from":"2026-09-01T00:00:00Z","to":"2026-10-01T00:00:00Z"},"total":41}`,
		},
		{
			id: "listEvents", method: "Events.List",
			run: func(ctx context.Context, c *Client) (any, error) {
				return c.Events.List(ctx, &ListEventsParams{Source: new(EventSourceCSV), ImportBatchID: new("import-1"), Limit: new(5)})
			},
			verb: "GET", url: "/v1/events?source=csv&import_batch_id=import-1&limit=5",
			resp: `{"as_of":"2026-10-02T00:00:00Z","client_id":null,"currency":"USD","data":[` + workspaceEventJSON + `],` + lastPage +
				`,"page":null,"period":{"from":null,"to":null},"total":null,"total_capped":null}`,
		},
		{
			id: "getEventSummary", method: "Events.GetSummary",
			run: func(ctx context.Context, c *Client) (any, error) {
				return c.Events.GetSummary(ctx, GetEventSummaryParams{From: from, To: to, AsOf: new(time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC))})
			},
			verb: "GET", url: "/v1/events/summary?" + periodQuery + "&as_of=2026-10-02T00%3A00%3A00Z",
			resp: `{"as_of":"2026-10-02T00:00:00Z","categories":[{"category":"task","count":3}],"client_id":null,"days":[{"count":3,"start":"2026-09-01T00:00:00Z"}],"period":{"from":"2026-09-01T00:00:00Z","to":"2026-10-01T00:00:00Z"},"total":3}`,
		},
		{
			id: "listEventArrivals", method: "Events.ListArrivals",
			run: func(ctx context.Context, c *Client) (any, error) {
				return c.Events.ListArrivals(ctx, &ListEventArrivalsParams{Since: new(time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)), Limit: new(1)})
			},
			verb: "GET", url: "/v1/events/arrivals?since=2026-10-01T12%3A00%3A00Z&limit=1",
			resp: `{"as_of":"2026-10-02T00:00:00Z","client_id":null,"count":7,"count_capped":false,"currency":"USD","data":[` + workspaceEventJSON + `],` + lastPage +
				`,"period":{"from":null,"to":null},"since":"2026-10-01T12:00:00Z"}`,
		},
		{
			id: "listImports", method: "Imports.List",
			run: func(ctx context.Context, c *Client) (any, error) {
				return c.Imports.List(ctx, &ListImportsParams{Limit: new(5)})
			},
			verb: "GET", url: "/v1/imports?limit=5",
			resp: listJSON(`{"accepted":2,"created_at":"2026-10-28T12:00:00Z","duplicates":1,"filename":"august.csv","id":"import-1","rejected":0,"total_rows":3}`),
		},
		{
			id: "getImport", method: "Imports.Get",
			run: func(ctx context.Context, c *Client) (any, error) {
				return c.Imports.Get(ctx, "import-1")
			},
			verb: "GET", url: "/v1/imports/import-1",
			resp: importJSON,
		},
		{
			id: "previewImport", method: "Imports.Preview",
			run: func(ctx context.Context, c *Client) (any, error) {
				return c.Imports.Preview(ctx, ImportUpload{Mapping: ImportMapping{Timezone: new("Asia/Tokyo")}, File: strings.NewReader("Time,Agent\n")})
			},
			verb: "POST", url: "/v1/imports/preview",
			form: []string{`name="mapping"`, `{"timezone":"Asia/Tokyo"}`, `name="file"`, "Time,Agent\n"},
			resp: `{"errors":[],"errors_truncated":false,"ignored_columns":["Notes"],"ignored_columns_omitted":2,"preview":[],"rejected":0,"timezone_warning":null,"total_rows":3,"valid":3}`,
		},
		{
			id: "commitImport", method: "Imports.Commit",
			run: func(ctx context.Context, c *Client) (any, error) {
				return c.Imports.Commit(ctx, ImportUpload{Mapping: ImportMapping{Delimiter: new(ImportDelimiterSemicolon)}, File: strings.NewReader("Time;Agent\n")})
			},
			verb: "POST", url: "/v1/imports",
			form: []string{`name="mapping"`, `{"delimiter":";"}`, `name="file"`, "Time;Agent\n"},
			resp: `{"accepted":2,"duplicates":1,"errors":[],"errors_truncated":false,"import_batch_id":"batch-1","rejected":0}`,
		},
		{
			id: "listClients", method: "Clients.List",
			run: func(ctx context.Context, c *Client) (any, error) {
				return c.Clients.List(ctx, &ListClientsParams{Limit: new(5)})
			},
			verb: "GET", url: "/v1/clients?limit=5",
			resp: listJSON(clientJSON),
		},
		{
			id: "createClient", method: "Clients.Create",
			run: func(ctx context.Context, c *Client) (any, error) {
				return c.Clients.Create(ctx, ClientCreate{Name: "Acme", Status: ClientStatusActive})
			},
			verb: "POST", url: "/v1/clients",
			body: `{"name":"Acme","status":"active"}`,
			resp: clientJSON,
		},
		{
			id: "getClient", method: "Clients.Get",
			run: func(ctx context.Context, c *Client) (any, error) {
				return c.Clients.Get(ctx, "client-3")
			},
			verb: "GET", url: "/v1/clients/client-3",
			resp: `{"agents":[{"agent_key":"bot","id":"agent-1","name":"Bot","status":"active"}],"client_key":null,"created_at":"2026-10-28T12:00:00Z","earliest_activity_at":"2026-09-01T00:00:00Z","id":"client-3","name":"Acme","notes":"n","renewal_date":"2027-01-31","status":"active"}`,
		},
		{
			id: "updateClient", method: "Clients.Update",
			run: func(ctx context.Context, c *Client) (any, error) {
				return c.Clients.Update(ctx, "client-4", ClientPatch{Name: new("Acme 2"), Notes: Null[string]()})
			},
			verb: "PATCH", url: "/v1/clients/client-4",
			body: `{"name":"Acme 2","notes":null}`,
			resp: clientJSON,
		},
		{
			id: "deleteClient", method: "Clients.Delete",
			run: func(ctx context.Context, c *Client) (any, error) {
				return nil, c.Clients.Delete(ctx, "client-5")
			},
			verb: "DELETE", url: "/v1/clients/client-5",
		},
		{
			id: "setClientAgents", method: "Clients.SetAgents",
			run: func(ctx context.Context, c *Client) (any, error) {
				return c.Clients.SetAgents(ctx, "client-6", ClientAgentsSet{AgentIds: []string{"agent-1", "agent-2"}})
			},
			verb: "PUT", url: "/v1/clients/client-6/agents",
			body: `{"agent_ids":["agent-1","agent-2"]}`,
			resp: `{"agents":[{"agent_key":"bot","id":"agent-1","name":"Bot","status":"active"}]}`,
		},
		{
			id: "listAgents", method: "Agents.List",
			run: func(ctx context.Context, c *Client) (any, error) {
				return c.Agents.List(ctx, &ListAgentsParams{Limit: new(5)})
			},
			verb: "GET", url: "/v1/agents?limit=5",
			resp: listJSON(`{"agent_key":"bot","client_count":2,"created_at":"2026-10-28T12:00:00Z","framework":"","id":"agent-1","name":"Bot","status":"active","version":""}`),
		},
		{
			id: "createAgent", method: "Agents.Create",
			run: func(ctx context.Context, c *Client) (any, error) {
				return c.Agents.Create(ctx, AgentCreate{AgentKey: "bot", Name: "Bot", Status: AgentStatusActive})
			},
			verb: "POST", url: "/v1/agents",
			body: `{"agent_key":"bot","name":"Bot","status":"active"}`,
			resp: agentJSON,
		},
		{
			id: "getAgent", method: "Agents.Get",
			run: func(ctx context.Context, c *Client) (any, error) {
				return c.Agents.Get(ctx, "agent-3", &GetAgentParams{From: &from, To: &to})
			},
			verb: "GET", url: "/v1/agents/agent-3?" + periodQuery,
			resp: `{"agent_key":"bot","clients":[{"id":"client-1","name":"Acme","status":"active"}],"created_at":"2026-10-28T12:00:00Z","framework":"","id":"agent-3","name":"Bot","stats":{"cost_total":` + exactMoney + `,"currency":"USD","events":4,"minor_units":2},"status":"active","version":""}`,
		},
		{
			id: "updateAgent", method: "Agents.Update",
			run: func(ctx context.Context, c *Client) (any, error) {
				return c.Agents.Update(ctx, "agent-4", AgentPatch{Version: Null[string]()})
			},
			verb: "PATCH", url: "/v1/agents/agent-4",
			body: `{"version":null}`,
			resp: agentJSON,
		},
		{
			id: "deleteAgent", method: "Agents.Delete",
			run: func(ctx context.Context, c *Client) (any, error) {
				return nil, c.Agents.Delete(ctx, "agent-5")
			},
			verb: "DELETE", url: "/v1/agents/agent-5",
		},
		{
			id: "listValueModels", method: "ValueModels.List",
			run: func(ctx context.Context, c *Client) (any, error) {
				return c.ValueModels.List(ctx, &ListValueModelsParams{Limit: new(5)})
			},
			verb: "GET", url: "/v1/value-models?limit=5",
			resp: listJSON(`{"client_id":"client-1","client_name":"Acme","created_at":"2026-10-28T12:00:00Z","id":"model-1","line_items":4,"name":"Support","status":"active","updated_at":"2026-10-28T12:00:00Z","version":3}`),
		},
		{
			id: "getValueModel", method: "ValueModels.Get",
			run: func(ctx context.Context, c *Client) (any, error) {
				return c.ValueModels.Get(ctx, "model-2")
			},
			verb: "GET", url: "/v1/value-models/model-2",
			resp: modelJSON,
		},
		{
			id: "getClientValueModel", method: "ValueModels.GetForClient",
			run: func(ctx context.Context, c *Client) (any, error) {
				return c.ValueModels.GetForClient(ctx, "client-7")
			},
			verb: "GET", url: "/v1/clients/client-7/value-model",
			resp: modelJSON,
		},
		{
			id: "createValueModel", method: "ValueModels.CreateForClient",
			run: func(ctx context.Context, c *Client) (any, error) {
				return c.ValueModels.CreateForClient(ctx, "client-8", ValueModelCreate{Name: "Support", TemplateID: new("tpl-1")})
			},
			verb: "POST", url: "/v1/clients/client-8/value-model",
			body: `{"name":"Support","template_id":"tpl-1"}`,
			resp: modelJSON,
		},
		{
			id: "updateValueModel", method: "ValueModels.UpdateForClient",
			run: func(ctx context.Context, c *Client) (any, error) {
				return c.ValueModels.UpdateForClient(ctx, "client-9", ValueModelPatch{Name: new("Support 2")})
			},
			verb: "PATCH", url: "/v1/clients/client-9/value-model",
			body: `{"name":"Support 2"}`,
			resp: modelJSON,
		},
		{
			id: "deleteValueModel", method: "ValueModels.DeleteForClient",
			run: func(ctx context.Context, c *Client) (any, error) {
				return nil, c.ValueModels.DeleteForClient(ctx, "client-10")
			},
			verb: "DELETE", url: "/v1/clients/client-10/value-model",
		},
		{
			id: "setValueModelLineItems", method: "ValueModels.SetLineItemsForClient",
			run: func(ctx context.Context, c *Client) (any, error) {
				return c.ValueModels.SetLineItemsForClient(ctx, "client-11", ValueLineItemsSet{
					Version:   3,
					LineItems: []ValueLineItemInput{{Label: "Ticket", OutcomeType: "ticket_resolved", UnitValue: &money}},
				})
			},
			verb: "PUT", url: "/v1/clients/client-11/value-model/line-items",
			body: `{"line_items":[{"label":"Ticket","outcome_type":"ticket_resolved","unit_value":` + exactMoney + `}],"version":3}`,
			resp: modelJSON,
		},
		{
			id: "listValueTemplates", method: "ValueModels.ListTemplates",
			run: func(ctx context.Context, c *Client) (any, error) {
				return c.ValueModels.ListTemplates(ctx, &ListValueTemplatesParams{Limit: new(5)})
			},
			verb: "GET", url: "/v1/value-templates?limit=5",
			resp: listJSON(`{"description":"Support desk","id":"tpl-1","line_items":[],"name":"Support"}`),
		},
		{
			id: "listOutcomeTypes", method: "OutcomeTypes.List",
			run: func(ctx context.Context, c *Client) (any, error) {
				return c.OutcomeTypes.List(ctx, &ListOutcomeTypesParams{From: &from, Limit: new(5)})
			},
			verb: "GET", url: "/v1/outcome-types?from=2026-09-01T00%3A00%3A00Z&limit=5",
			resp: listJSON(`{"events":3,"outcome_type":"ticket_resolved","priced":true,"quantity":3.5}`),
		},
		{
			id: "listClientOutcomeTypes", method: "OutcomeTypes.ListForClient",
			run: func(ctx context.Context, c *Client) (any, error) {
				return c.OutcomeTypes.ListForClient(ctx, "client-12", &ListClientOutcomeTypesParams{Limit: new(5)})
			},
			verb: "GET", url: "/v1/clients/client-12/outcome-types?limit=5",
			resp: listJSON(`{"events":3,"outcome_type":"ticket_resolved","priced":null,"quantity":3}`),
		},
		{
			id: "listCostLineItems", method: "Cost.ListLineItems",
			run: func(ctx context.Context, c *Client) (any, error) {
				return c.Cost.ListLineItems(ctx, &ListCostLineItemsParams{Limit: new(5)})
			},
			verb: "GET", url: "/v1/cost-line-items?limit=5",
			resp: listJSON(lineJSON),
		},
		{
			id: "createCostLineItem", method: "Cost.CreateLineItem",
			run: func(ctx context.Context, c *Client) (any, error) {
				return c.Cost.CreateLineItem(ctx, CostLineItemCreate{
					Label: "Hosting", MonthlyAmount: money, StartDate: "2026-09-01",
					Target: CostTargetInput{ID: "client-1", Kind: CostTargetKindClient},
				})
			},
			verb: "POST", url: "/v1/cost-line-items",
			body: `{"label":"Hosting","monthly_amount":` + exactMoney + `,"start_date":"2026-09-01","target":{"id":"client-1","kind":"client"}}`,
			resp: lineJSON,
		},
		{
			id: "getCostLineItem", method: "Cost.GetLineItem",
			run: func(ctx context.Context, c *Client) (any, error) {
				return c.Cost.GetLineItem(ctx, "line-3")
			},
			verb: "GET", url: "/v1/cost-line-items/line-3",
			resp: lineJSON,
		},
		{
			id: "updateCostLineItem", method: "Cost.UpdateLineItem",
			run: func(ctx context.Context, c *Client) (any, error) {
				return c.Cost.UpdateLineItem(ctx, "line-4", CostLineItemPatch{EndDate: Null[string]()})
			},
			verb: "PATCH", url: "/v1/cost-line-items/line-4",
			body: `{"end_date":null}`,
			resp: lineJSON,
		},
		{
			id: "deleteCostLineItem", method: "Cost.DeleteLineItem",
			run: func(ctx context.Context, c *Client) (any, error) {
				return nil, c.Cost.DeleteLineItem(ctx, "line-5")
			},
			verb: "DELETE", url: "/v1/cost-line-items/line-5",
		},
		{
			id: "getPriceTable", method: "Cost.GetPriceTable",
			run: func(ctx context.Context, c *Client) (any, error) {
				return c.Cost.GetPriceTable(ctx)
			},
			verb: "GET", url: "/v1/price-table",
			resp: tableJSON,
		},
		{
			id: "setPriceTable", method: "Cost.SetPriceTable",
			run: func(ctx context.Context, c *Client) (any, error) {
				return c.Cost.SetPriceTable(ctx, PriceTableSet{Models: []ModelRateInput{{Model: "gpt", InputPer1K: money, OutputPer1K: MustParseDecimal("12")}}})
			},
			verb: "PUT", url: "/v1/price-table",
			body: `{"models":[{"input_per_1k":` + exactMoney + `,"model":"gpt","output_per_1k":12}]}`,
			resp: tableJSON,
		},
		{
			id: "getClientValue", method: "Analytics.GetValueForClient",
			run: func(ctx context.Context, c *Client) (any, error) {
				return c.Analytics.GetValueForClient(ctx, "client-13", &GetClientValueParams{From: &from})
			},
			verb: "GET", url: "/v1/clients/client-13/value?from=2026-09-01T00%3A00%3A00Z",
			resp: `{"client_id":"client-13","currency":"USD","lines":[],"minor_units":2,"period":{"from":"2026-09-01T00:00:00Z","to":null},"total":` + exactMoney + `,"value_model":null}`,
		},
		{
			id: "getClientCost", method: "Analytics.GetCostForClient",
			run: func(ctx context.Context, c *Client) (any, error) {
				return c.Analytics.GetCostForClient(ctx, "client-14", GetClientCostParams{From: from, To: to})
			},
			verb: "GET", url: "/v1/clients/client-14/cost?" + periodQuery,
			resp: `{"agent_lines":[],"client_id":"client-14","currency":"USD","manual":0,"manual_lines":[],"minor_units":2,"pass_through":1.5,"token_lines":[],"token_valued":0,"total":` + exactMoney + `}`,
		},
		{
			id: "getClientRoi", method: "Analytics.GetRoiForClient",
			run: func(ctx context.Context, c *Client) (any, error) {
				return c.Analytics.GetRoiForClient(ctx, "client-15", GetClientRoiParams{From: from, To: to})
			},
			verb: "GET", url: "/v1/clients/client-15/roi?" + periodQuery,
			resp: `{"client_id":"client-15","cost":{"total":` + exactMoney + `},"currency":"USD","minor_units":2,"ratios":{"roi":1.5,"undefined_reason":null,"value_to_cost":2.5},"value":{"total":3.25}}`,
		},
		{
			id: "getClientAnomalies", method: "Analytics.GetAnomaliesForClient",
			run: func(ctx context.Context, c *Client) (any, error) {
				return c.Analytics.GetAnomaliesForClient(ctx, "client-16", GetClientAnomaliesParams{From: from, To: to})
			},
			verb: "GET", url: "/v1/clients/client-16/anomalies?" + periodQuery,
			resp: `{"client_id":"client-16","cost_spikes":[{"baseline":` + exactMoney + `,"cost":9.5,"date":"2026-09-03"}],"currency":"USD","minor_units":2,"zero_outcome_days":["2026-09-04"]}`,
		},
		{
			id: "createCalculation", method: "Calculations.CreateForClient",
			run: func(ctx context.Context, c *Client) (any, error) {
				return c.Calculations.CreateForClient(ctx, "client-18", CalculationCreate{Period: PeriodInput{From: from, To: to}})
			},
			verb: "POST", url: "/v1/clients/client-18/calculations",
			body: `{"period":{"from":"2026-09-01T00:00:00Z","to":"2026-10-01T00:00:00Z"}}`,
			resp: calcJSON,
		},
		{
			id: "listClientCalculations", method: "Calculations.ListForClient",
			run: func(ctx context.Context, c *Client) (any, error) {
				return c.Calculations.ListForClient(ctx, "client-19", &ListClientCalculationsParams{Limit: new(5)})
			},
			verb: "GET", url: "/v1/clients/client-19/calculations?limit=5",
			resp: listJSON(`{"client_id":"client-19","cost_total":` + exactMoney + `,"id":"calc-1","is_current":true,"state":"published"}`),
		},
		{
			id: "listCalculations", method: "Calculations.List",
			run: func(ctx context.Context, c *Client) (any, error) {
				return c.Calculations.List(ctx, &ListCalculationsParams{ClientID: new("client-19"), State: new(CalculationStatePublished), Month: new("2026-08"), Limit: new(5)})
			},
			verb: "GET", url: "/v1/calculations?client_id=client-19&state=published&month=2026-08&limit=5",
			resp: listJSON(`{"client_id":"client-19","client_name":"Acme Logistics","cost_total":` + exactMoney + `,"coverage_findings":2,"id":"calc-1","is_current":true,"state":"published"}`),
		},
		{
			id: "getCalculation", method: "Calculations.Get",
			run: func(ctx context.Context, c *Client) (any, error) {
				return c.Calculations.Get(ctx, "calc-3")
			},
			verb: "GET", url: "/v1/calculations/calc-3",
			resp: calcJSON,
		},
		{
			id: "deleteCalculation", method: "Calculations.Delete",
			run: func(ctx context.Context, c *Client) (any, error) {
				return nil, c.Calculations.Delete(ctx, "calc-4")
			},
			verb: "DELETE", url: "/v1/calculations/calc-4",
		},
		{
			id: "createReport", method: "Reports.CreateForCalculation",
			run: func(ctx context.Context, c *Client) (any, error) {
				return c.Reports.CreateForCalculation(ctx, "calc-5")
			},
			verb: "POST", url: "/v1/calculations/calc-5/reports",
			resp: reportJSON,
		},
		{
			id: "listCalculationReports", method: "Reports.ListForCalculation",
			run: func(ctx context.Context, c *Client) (any, error) {
				return c.Reports.ListForCalculation(ctx, "calc-6", &ListCalculationReportsParams{Limit: new(5)})
			},
			verb: "GET", url: "/v1/calculations/calc-6/reports?limit=5",
			resp: listJSON(summaryJSON),
		},
		{
			id: "listReports", method: "Reports.List",
			run: func(ctx context.Context, c *Client) (any, error) {
				return c.Reports.List(ctx, &ListReportsParams{ClientID: new("client-19"), State: new(ReportStateShared), Limit: new(5)})
			},
			verb: "GET", url: "/v1/reports?client_id=client-19&state=shared&limit=5",
			resp: listJSON(summaryJSON),
		},
		{
			id: "getReport", method: "Reports.Get",
			run: func(ctx context.Context, c *Client) (any, error) {
				return c.Reports.Get(ctx, "report-3")
			},
			verb: "GET", url: "/v1/reports/report-3",
			resp: `{"calculation_id":"calc-1","client_id":"client-1","client_name":"Acme Logistics","created_at":"2026-10-28T12:00:00Z","disclosures":[],"id":"report-3","letterhead_client_name":"Acme","letterhead_workspace_name":"Partner","note":"","period":{"from":"2026-09-01T00:00:00Z","to":"2026-10-01T00:00:00Z"},"preview":null,"state":"draft"}`,
		},
		{
			id: "updateReport", method: "Reports.Update",
			run: func(ctx context.Context, c *Client) (any, error) {
				return c.Reports.Update(ctx, "report-4", ReportPatch{LetterheadClientName: new("Acme Holdings"), LetterheadWorkspaceName: new("Partner Co"), Note: NewNullable("Thanks")})
			},
			verb: "PATCH", url: "/v1/reports/report-4",
			body: `{"letterhead_client_name":"Acme Holdings","letterhead_workspace_name":"Partner Co","note":"Thanks"}`,
			resp: reportJSON,
		},
		{
			id: "deleteReport", method: "Reports.Delete",
			run: func(ctx context.Context, c *Client) (any, error) {
				return nil, c.Reports.Delete(ctx, "report-5")
			},
			verb: "DELETE", url: "/v1/reports/report-5",
		},
		{
			id: "getDashboard", method: "Dashboard.Get",
			run: func(ctx context.Context, c *Client) (any, error) {
				return c.Dashboard.Get(ctx, GetDashboardParams{Month: "2026-09", Sort: new(GetDashboardParamsSortRoi)})
			},
			verb: "GET", url: "/v1/dashboard?month=2026-09&sort=roi",
			resp: `{"clients":[],"cost_total":` + exactMoney + `,"counted":2,"currency":"USD","latest_published_month":"2026-09","minor_units":2,"period_ended":true,"value_total":12}`,
		},
		{
			id: "listRenewals", method: "Dashboard.ListRenewals",
			run: func(ctx context.Context, c *Client) (any, error) {
				return c.Dashboard.ListRenewals(ctx, &ListRenewalsParams{Limit: new(5)})
			},
			verb: "GET", url: "/v1/dashboard/renewals?limit=5",
			resp: listJSON(`{"client_id":"client-1","name":"Acme","renewal_date":"2027-01-31","roi":1.25}`),
		},
		{
			id: "getHealth", method: "Health.Get",
			run: func(ctx context.Context, c *Client) (any, error) {
				return c.Health.Get(ctx)
			},
			verb: "GET", url: "/healthz",
			resp: `{"status":"ok","version":"1.2.3"}`,
		},
		{
			id: "getWorkspace", method: "Workspace.Get",
			run: func(ctx context.Context, c *Client) (any, error) {
				return c.Workspace.Get(ctx)
			},
			verb: "GET", url: "/v1/workspace",
			resp: workspaceJSON,
		},
		{
			id: "getOnboarding", method: "Onboarding.Get",
			run: func(ctx context.Context, c *Client) (any, error) {
				return c.Onboarding.Get(ctx)
			},
			verb: "GET", url: "/v1/onboarding",
			resp: onboardingJSON,
		},
		{
			id: "setOnboardingSkippedSteps", method: "Onboarding.SetSkippedSteps",
			run: func(ctx context.Context, c *Client) (any, error) {
				return c.Onboarding.SetSkippedSteps(ctx, OnboardingSkippedStepsSet{Steps: []OnboardingStep{OnboardingStepAddAgent, OnboardingStepBringInData}})
			},
			verb: "PUT", url: "/v1/onboarding/skipped-steps",
			body: `{"steps":["add_agent","bring_in_data"]}`,
			resp: onboardingJSON,
		},
	}
}

const (
	workspaceJSON  = `{"created_at":"2026-10-28T12:00:00Z","currency":"JPY","id":"workspace-1","minor_units":0,"name":"Acme AI"}`
	onboardingJSON = `{"current_step":"price_outcomes","has_published":false,"steps":[{"completed":true,"skipped_at":null,"step":"add_client"},{"completed":false,"skipped_at":"2026-10-28T12:00:00Z","step":"add_agent"}]}`
)

// answer responds to every request with a row's response, under the
// status its operation documents.
func answer(t *testing.T, row methodRow) func(int, *http.Request, recorded) (*http.Response, error) {
	op := mustOp(t, row.id)
	return func(int, *http.Request, recorded) (*http.Response, error) {
		if row.resp == "" {
			return newResponse(op.status, "", ""), nil
		}
		return newResponse(op.status, "application/json", row.resp, "X-Request-Id", "req-1"), nil
	}
}

// TestEveryMethodSendsItsRequestAndDecodesItsAnswer calls each of the 48
// methods once, with arguments a swap would show, and checks the request it
// sends and the result it returns.
func TestEveryMethodSendsItsRequestAndDecodesItsAnswer(t *testing.T) {
	rows := methodRows()
	var ids []string
	for _, row := range rows {
		ids = append(ids, row.id)
	}
	var tableIDs []string
	for _, op := range operationTable() {
		tableIDs = append(tableIDs, op.id)
	}
	slices.Sort(ids)
	if len(slices.Compact(slices.Clone(ids))) != len(ids) {
		t.Errorf("an operation has two rows: %v", ids)
	}
	if onlyRows, onlyTable := sameSet(ids, tableIDs); len(onlyRows) > 0 || len(onlyTable) > 0 {
		t.Errorf("rows and operation table differ: only in the rows %v, only in the table %v", onlyRows, onlyTable)
	}

	for _, row := range rows {
		t.Run(row.method, func(t *testing.T) {
			h := newHarness(t, answer(t, row))
			result, err := row.run(context.Background(), h.client)
			if err != nil {
				t.Fatalf("%s: %v", row.method, err)
			}
			if n := h.transport.count(); n != 1 {
				t.Fatalf("sent %d requests, want 1", n)
			}
			got := h.transport.get(0)
			if got.method != row.verb || got.url != "https://api.test"+row.url {
				t.Errorf("sent %s %s, want %s https://api.test%s", got.method, got.url, row.verb, row.url)
			}
			checkRequestHeaders(t, row, got)
			checkRequestBody(t, row, got)

			switch {
			case row.resp == "":
				if result != nil {
					t.Errorf("a 204 method returned %v", result)
				}
			case row.check != nil:
				row.check(t, result)
			default:
				checkResult(t, row.resp, result)
			}
		})
	}
}

func checkRequestHeaders(t *testing.T, row methodRow, got recorded) {
	t.Helper()
	wantAuth := "Bearer aia_testkey"
	if row.id == "getHealth" {
		wantAuth = ""
	}
	if auth := got.header.Get("Authorization"); auth != wantAuth {
		t.Errorf("Authorization = %q, want %q", auth, wantAuth)
	}
	key, hasKey := got.header["Idempotency-Key"]
	if slices.Contains(idempotentOps, row.id) {
		if !hasKey || !uuidV4.MatchString(key[0]) {
			t.Errorf("Idempotency-Key = %q, want a generated UUID", key)
		}
	} else if hasKey {
		t.Errorf("Idempotency-Key = %q, want none", key)
	}
}

func checkRequestBody(t *testing.T, row methodRow, got recorded) {
	t.Helper()
	ct := got.header.Get("Content-Type")
	switch {
	case row.form != nil:
		if !strings.HasPrefix(ct, "multipart/form-data; boundary=") {
			t.Errorf("Content-Type = %q, want multipart/form-data", ct)
		}
		for _, part := range row.form {
			if !bytes.Contains(got.body, []byte(part)) {
				t.Errorf("form %q does not hold %q", got.body, part)
			}
		}
	case row.body == "":
		if len(got.body) != 0 || ct != "" {
			t.Errorf("sent body %q with Content-Type %q, want none", got.body, ct)
		}
	default:
		want := "application/json"
		if row.verb == http.MethodPatch {
			want = "application/merge-patch+json"
		}
		if ct != want {
			t.Errorf("Content-Type = %q, want %q", ct, want)
		}
		if string(got.body) != row.body {
			t.Errorf("body = %s\nwant   %s", got.body, row.body)
		}
	}
}

// checkResult checks that result holds every member of resp with the
// value it arrived with, numbers compared as their text: a member decoded
// into the wrong type, or money read through a float, fails it. A page's
// Data must equal its body's data.
func checkResult(t *testing.T, resp string, result any) {
	t.Helper()
	want := decodeNumbers(t, []byte(resp))
	if p, ok := result.(testPage); ok {
		data, body := p.parts()
		if err := subset(want, decodeNumbers(t, mustMarshal(t, body)), ""); err != nil {
			t.Errorf("page body: %v", err)
		}
		wantData, _ := want.(map[string]any)
		if err := subset(wantData["data"], decodeNumbers(t, mustMarshal(t, data)), "/data"); err != nil {
			t.Errorf("page data: %v", err)
		}
		return
	}
	if result == nil || reflect.ValueOf(result).IsNil() {
		t.Fatal("no result")
	}
	if err := subset(want, decodeNumbers(t, mustMarshal(t, result)), ""); err != nil {
		t.Error(err)
	}
}

func decodeNumbers(t *testing.T, b []byte) any {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		t.Fatalf("decode %s: %v", b, err)
	}
	return v
}

// subset reports the first member of want that got lacks or holds with
// another value. Objects in got may hold more members; arrays must match
// item by item.
func subset(want, got any, at string) error {
	switch w := want.(type) {
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok {
			return fmt.Errorf("%s: got %v, want an object", at, got)
		}
		for k, wv := range w {
			gv, ok := g[k]
			if !ok {
				return fmt.Errorf("%s/%s: missing", at, k)
			}
			if err := subset(wv, gv, at+"/"+k); err != nil {
				return err
			}
		}
	case []any:
		g, ok := got.([]any)
		if !ok || len(g) != len(w) {
			return fmt.Errorf("%s: got %v, want %d items", at, got, len(w))
		}
		for i := range w {
			if err := subset(w[i], g[i], fmt.Sprintf("%s/%d", at, i)); err != nil {
				return err
			}
		}
	default:
		if want != got {
			return fmt.Errorf("%s: got %v, want %v", at, got, want)
		}
	}
	return nil
}

// TestSubsetCanFail checks the comparison above against the failures it
// must catch: money read through a float, a missing member and a changed
// item.
func TestSubsetCanFail(t *testing.T) {
	var asFloat struct {
		Total float64 `json:"total"`
	}
	resp := []byte(`{"total":` + exactMoney + `}`)
	if err := json.Unmarshal(resp, &asFloat); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct{ want, got string }{
		{string(resp), string(mustMarshal(t, asFloat))},
		{`{"a":1,"b":2}`, `{"a":1}`},
		{`{"a":[1,2]}`, `{"a":[1,3]}`},
		{`{"a":[1,2]}`, `{"a":[1]}`},
	} {
		if subset(decodeNumbers(t, []byte(tt.want)), decodeNumbers(t, []byte(tt.got)), "") == nil {
			t.Errorf("%s was taken for a subset of %s", tt.want, tt.got)
		}
	}
	if err := subset(decodeNumbers(t, resp), decodeNumbers(t, []byte(`{"total":`+exactMoney+`,"extra":1}`)), ""); err != nil {
		t.Errorf("an extra member was refused: %v", err)
	}
}

// TestEveryListMethodWalksItsPages walks each list method over two pages:
// the second request carries the first page's cursor and the call's
// filters, and All yields the items of both.
func TestEveryListMethodWalksItsPages(t *testing.T) {
	lists := 0
	for _, row := range methodRows() {
		if !strings.Contains(row.resp, lastPage) {
			continue
		}
		lists++
		t.Run(row.method, func(t *testing.T) {
			first := strings.Replace(row.resp, lastPage, `"has_more":true,"next_cursor":"cur-2"`, 1)
			h := newHarness(t, func(n int, _ *http.Request, _ recorded) (*http.Response, error) {
				if n == 0 {
					return newResponse(200, "application/json", first), nil
				}
				return newResponse(200, "application/json", row.resp), nil
			})
			result, err := row.run(context.Background(), h.client)
			if err != nil {
				t.Fatal(err)
			}
			page, ok := result.(testPage)
			if !ok {
				t.Fatalf("%s returned %T, not a page", row.method, result)
			}
			n, err := page.walk(context.Background())
			if err != nil || n != 2 {
				t.Errorf("walk yielded %d items and %v, want 2 and no error", n, err)
			}
			if c := h.transport.count(); c != 2 {
				t.Fatalf("sent %d requests, want 2", c)
			}
			if got, want := h.transport.get(1).url, "https://api.test"+row.url+"&cursor=cur-2"; got != want {
				t.Errorf("second request %s, want %s", got, want)
			}
		})
	}
	if lists != 16 {
		t.Errorf("walked %d list methods, want 16", lists)
	}
}

// serviceMethods returns every method of every service on a client, as
// Group.Method, found by reflection on the Client's fields.
func serviceMethods(c *Client) map[string]reflect.Method {
	out := map[string]reflect.Method{}
	v := reflect.ValueOf(c).Elem()
	for i := 0; i < v.NumField(); i++ {
		f := v.Type().Field(i)
		if !f.IsExported() || f.Type.Kind() != reflect.Pointer || !strings.HasSuffix(f.Type.Elem().Name(), "Service") {
			continue
		}
		for j := 0; j < f.Type.NumMethod(); j++ {
			m := f.Type.Method(j)
			if m.Name != "Format" {
				out[f.Name+"."+m.Name] = m
			}
		}
	}
	return out
}

func testClient(t *testing.T) *Client {
	t.Helper()
	c, err := NewClient(WithAPIKey("aia_testkey"))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// TestTheServicesAreTheFourteenGroups checks the client's service fields:
// the fourteen groups, each set.
func TestTheServicesAreTheFourteenGroups(t *testing.T) {
	c := testClient(t)
	var groups []string
	v := reflect.ValueOf(c).Elem()
	for i := 0; i < v.NumField(); i++ {
		f := v.Type().Field(i)
		if !f.IsExported() {
			continue
		}
		groups = append(groups, f.Name)
		if v.Field(i).IsNil() {
			t.Errorf("Client.%s is nil", f.Name)
		}
	}
	want := []string{"Agents", "Analytics", "Calculations", "Clients", "Cost", "Dashboard", "Events", "Health", "Imports", "Onboarding", "OutcomeTypes", "Reports", "ValueModels", "Workspace"}
	slices.Sort(groups)
	if !slices.Equal(groups, want) {
		t.Errorf("groups = %v, want %v", groups, want)
	}
}

// TestTheMethodsAreTheRows checks, both ways, that the services' methods
// are the rows' methods, so each method is tied to its operation by a row.
// It also checks each method's shape: ctx first, then strings for the path
// ids, then options last, IdempotentRequestOption on exactly the seven
// idempotent operations, and a 204 operation returning only an error.
func TestTheMethodsAreTheRows(t *testing.T) {
	methods := serviceMethods(testClient(t))
	rows := map[string]methodRow{}
	for _, row := range methodRows() {
		rows[row.method] = row
	}
	var got, want []string
	for name := range methods {
		got = append(got, name)
	}
	for name := range rows {
		want = append(want, name)
	}
	if onlyRows, onlyMethods := sameSet(want, got); len(onlyRows) > 0 || len(onlyMethods) > 0 {
		t.Errorf("rows and methods differ: only in the rows %v, only on the services %v", onlyRows, onlyMethods)
	}

	ctxType := reflect.TypeFor[context.Context]()
	errType := reflect.TypeFor[error]()
	for name, m := range methods {
		row, ok := rows[name]
		if !ok {
			continue
		}
		op := mustOp(t, row.id)
		ft := m.Type // the receiver is the first parameter
		if ft.NumIn() < 3 || ft.In(1) != ctxType || !ft.IsVariadic() {
			t.Errorf("%s: %s does not take ctx first and options last", name, ft)
			continue
		}
		for i := range strings.Count(op.path, "{") {
			if ft.In(2+i).Kind() != reflect.String {
				t.Errorf("%s: parameter %d is %s, want the path id as a string", name, 2+i, ft.In(2+i))
			}
		}
		opts := ft.In(ft.NumIn() - 1).Elem()
		wantOpts := reflect.TypeFor[RequestOption]()
		if slices.Contains(idempotentOps, row.id) {
			wantOpts = reflect.TypeFor[IdempotentRequestOption]()
		}
		if opts != wantOpts {
			t.Errorf("%s: takes ...%s, want ...%s", name, opts, wantOpts)
		}
		if row.resp == "" && (ft.NumOut() != 1 || ft.Out(0) != errType) {
			t.Errorf("%s: a 204 operation returns %s, want only error", name, ft)
		}
		if row.resp != "" && (ft.NumOut() != 2 || ft.Out(0).Kind() != reflect.Pointer || ft.Out(1) != errType) {
			t.Errorf("%s: returns %s, want a pointer and an error", name, ft)
		}
	}
}

var (
	operationIDLine = regexp.MustCompile(`^      operationId: ([A-Za-z]+)$`)
	tagItemLine     = regexp.MustCompile(`^        - (.+)$`)
)

// specTags returns each operation's first tag, read from the spec.
func specTags(lines []string) map[string]string {
	out := map[string]string{}
	for i, l := range lines {
		m := operationIDLine.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		for j := i + 1; j < len(lines) && indentOf(lines[j]) >= 6; j++ {
			if lines[j] == "      tags:" && j+1 < len(lines) {
				if tag := tagItemLine.FindStringSubmatch(lines[j+1]); tag != nil {
					out[m[1]] = tag[1]
				}
				break
			}
		}
	}
	return out
}

// namedApart holds the one name the rule does not give: listValueTemplates,
// whose noun Value is the first word of its group's, Value models.
var namedApart = map[string]string{"listValueTemplates": "ListTemplates"}

// methodName applies the naming rule: the group is the tag with each word
// capitalised; the verb comes from the operationId; the group's own noun,
// plural or singular, is dropped; a path whose first id names a client or
// a calculation, outside that resource's own group, adds ForClient or
// ForCalculation; any other noun stays.
func methodName(id, tag, path string) string {
	words := strings.Fields(tag)
	plural := ""
	for _, w := range words {
		plural += strings.ToUpper(w[:1]) + strings.ToLower(w[1:])
	}
	group := plural
	if apart, ok := namedApart[id]; ok {
		return group + "." + apart
	}
	verbEnd := strings.IndexFunc(id, func(r rune) bool { return r >= 'A' && r <= 'Z' })
	if verbEnd < 0 {
		verbEnd = len(id)
	}
	verb, rest := id[:verbEnd], id[verbEnd:]
	singular := strings.TrimSuffix(plural, "s")
	for _, noun := range []string{plural, singular} {
		if strings.Contains(rest, noun) {
			rest = strings.Replace(rest, noun, "", 1)
			break
		}
	}
	parents := map[string]string{"clients": "Client", "calculations": "Calculation"}
	segments := strings.Split(strings.TrimPrefix(path, "/v1/"), "/")
	if len(segments) > 1 && segments[1] == "{id}" {
		if noun, ok := parents[segments[0]]; ok && noun+"s" != group {
			rest = strings.Replace(rest, noun, "", 1) + "For" + noun
		}
	}
	return group + "." + strings.ToUpper(verb[:1]) + verb[1:] + rest
}

// TestTheMethodNamesFollowTheNamingRule checks each row's method against
// the naming rule, applied to its operation's tag from the spec and its
// path from the operation table. With the rows equal to the table and to
// the services' methods, both ways, the set of methods is the rule's name
// for each operation, with ListTemplates the one listed exception.
func TestTheMethodNamesFollowTheNamingRule(t *testing.T) {
	tags := specTags(readSpec(t))
	for _, row := range methodRows() {
		tag, ok := tags[row.id]
		if !ok {
			t.Errorf("%s has no tag in the spec", row.id)
			continue
		}
		if got := methodName(row.id, tag, mustOp(t, row.id).path); got != row.method {
			t.Errorf("%s: the rule names %s, the row %s", row.id, got, row.method)
		}
	}
}

// TestMethodNameCanFail checks the rule gives the names it should, the
// exception aside, and that dropping the exception changes the one name.
func TestMethodNameCanFail(t *testing.T) {
	for _, tt := range []struct{ id, tag, path, want string }{
		{"listClientCalculations", "Calculations", "/v1/clients/{id}/calculations", "Calculations.ListForClient"},
		{"getClientValue", "Analytics", "/v1/clients/{id}/value", "Analytics.GetValueForClient"},
		{"setClientAgents", "Clients", "/v1/clients/{id}/agents", "Clients.SetAgents"},
		{"listCostLineItems", "Cost", "/v1/cost-line-items", "Cost.ListLineItems"},
		{"getValueModel", "Value models", "/v1/value-models/{id}", "ValueModels.Get"},
		{"createReport", "Reports", "/v1/calculations/{id}/reports", "Reports.CreateForCalculation"},
		{"getWorkspace", "Workspace", "/v1/workspace", "Workspace.Get"},
		{"getOnboarding", "Onboarding", "/v1/onboarding", "Onboarding.Get"},
		{"setOnboardingSkippedSteps", "Onboarding", "/v1/onboarding/skipped-steps", "Onboarding.SetSkippedSteps"},
	} {
		if got := methodName(tt.id, tt.tag, tt.path); got != tt.want {
			t.Errorf("methodName(%s) = %s, want %s", tt.id, got, tt.want)
		}
	}
	delete(namedApart, "listValueTemplates")
	defer func() { namedApart["listValueTemplates"] = "ListTemplates" }()
	if got := methodName("listValueTemplates", "Value models", "/v1/value-templates"); got != "ValueModels.ListValueTemplates" {
		t.Errorf("without the exception the rule names %s", got)
	}
}

// TestSetLineItemsForClientIsNeverRetried sends the version-guarded PUT
// against a 503, with the client's retries and with five asked for on the
// call: each makes one attempt, while the price table's PUT, against the
// same answer, is retried.
func TestSetLineItemsForClientIsNeverRetried(t *testing.T) {
	unavailable := func(int, *http.Request, recorded) (*http.Response, error) {
		return problemResponse(503, "service_unavailable", "req-1"), nil
	}
	set := ValueLineItemsSet{Version: 3}
	for _, tt := range []struct {
		name string
		opts []RequestOption
	}{
		{"the client's retries", nil},
		{"five retries asked for", []RequestOption{WithRequestMaxRetries(5)}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t, unavailable)
			_, err := h.client.ValueModels.SetLineItemsForClient(context.Background(), "client-1", set, tt.opts...)
			var apiErr *APIError
			if !errors.As(err, &apiErr) || apiErr.Status != 503 {
				t.Fatalf("err = %v, want the 503", err)
			}
			if n := h.transport.count(); n != 1 {
				t.Errorf("%d attempts, want 1", n)
			}
		})
	}
	h := newHarness(t, unavailable)
	_, _ = h.client.Cost.SetPriceTable(context.Background(), PriceTableSet{})
	if n := h.transport.count(); n != 3 {
		t.Errorf("SetPriceTable made %d attempts against a 503, want 3: the fixture must be one a PUT retries", n)
	}
}

// TestAKeyWithoutTheScopeGetsAnAPIError reads a 403 insufficient_scope
// through a method as an *APIError with that code.
func TestAKeyWithoutTheScopeGetsAnAPIError(t *testing.T) {
	h := newHarness(t, func(int, *http.Request, recorded) (*http.Response, error) {
		return problemResponse(403, "insufficient_scope", "req-9"), nil
	})
	_, err := h.client.Clients.Create(context.Background(), ClientCreate{Name: "Acme"})
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != 403 || apiErr.Code != ErrorCodeInsufficientScope || apiErr.RequestID != "req-9" {
		t.Errorf("err = %v, want an *APIError 403 insufficient_scope", err)
	}
	if h.transport.count() != 1 {
		t.Errorf("sent %d requests, want 1: a 403 is not retried", h.transport.count())
	}
}

// TestAReportListItemIsASummary decodes a list item without disclosures, and
// one that still carries them, as a member the summary does not list.
func TestAReportListItemIsASummary(t *testing.T) {
	withDisclosures := strings.Replace(summaryJSON, `"id":"report-1"`, `"disclosures":[{"facts":{},"included":true,"kind":"unpriced","text":null}],"id":"report-2"`, 1)
	h := newHarness(t, func(int, *http.Request, recorded) (*http.Response, error) {
		return newResponse(200, "application/json", `{"data":[`+summaryJSON+`,`+withDisclosures+`],`+lastPage+`}`), nil
	})
	page, err := h.client.Reports.ListForCalculation(context.Background(), "calc-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	period := Period{From: new(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)), To: new(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC))}
	summary := func(id string) ReportSummary {
		return ReportSummary{
			CalculationID: "calc-1", ClientID: "client-1", ClientName: "Acme Logistics", CreatedAt: fakeNow, ID: id,
			LetterheadClientName: "Acme", LetterheadWorkspaceName: "Partner", Period: period, State: ReportStateDraft,
		}
	}
	want := []ReportSummary{summary("report-1"), summary("report-2")}
	if !reflect.DeepEqual(page.Data, want) {
		t.Errorf("data = %+v, want %+v", page.Data, want)
	}
	if _, ok := reflect.TypeFor[ReportSummary]().FieldByName("Disclosures"); ok {
		t.Error("ReportSummary has a Disclosures field")
	}
}

// TestServiceFormatHidesTheClient prints every service with each verb, and
// finds no part of the credential.
func TestServiceFormatHidesTheClient(t *testing.T) {
	const key = "aia_secretkeyvalue1234"
	c, err := NewClient(WithAPIKey(key))
	if err != nil {
		t.Fatal(err)
	}
	v := reflect.ValueOf(c).Elem()
	for i := 0; i < v.NumField(); i++ {
		if !v.Type().Field(i).IsExported() {
			continue
		}
		service := v.Field(i).Interface()
		for _, verb := range []string{"%v", "%+v", "%#v", "%s"} {
			for _, x := range []any{service, v.Field(i).Elem().Interface()} {
				if out := fmt.Sprintf(verb, x); leaksCredential(out, key) {
					t.Errorf("%s with %s printed %q", v.Type().Field(i).Name, verb, out)
				}
			}
		}
	}
}

// TestAPriceTableIsSentBackThroughTheMethods reads a price table holding a
// rate in exponent form with GetPriceTable, and sends it back with
// SetPriceTable: each rate keeps its digits.
func TestAPriceTableIsSentBackThroughTheMethods(t *testing.T) {
	h := newHarness(t, func(int, *http.Request, recorded) (*http.Response, error) {
		return newResponse(200, "application/json",
			`{"currency":"USD","models":[{"model":"a","input_per_1k":1e-7,"output_per_1k":`+exactMoney+`}]}`), nil
	})
	ctx := context.Background()
	table, err := h.client.Cost.GetPriceTable(ctx)
	if err != nil {
		t.Fatal(err)
	}
	set := PriceTableSet{Currency: &table.Currency}
	for _, m := range table.Models {
		set.Models = append(set.Models, ModelRateInput(m))
	}
	if _, err := h.client.Cost.SetPriceTable(ctx, set); err != nil {
		t.Fatal(err)
	}
	want := `{"currency":"USD","models":[{"input_per_1k":0.0000001,"model":"a","output_per_1k":` + exactMoney + `}]}`
	if got := string(h.transport.get(1).body); got != want {
		t.Errorf("body = %s\nwant   %s", got, want)
	}
}

// TestSkippedStepsSendANilSliceAsNull pins SetSkippedSteps's doc comment to
// what the SDK sends: a nil Steps is sent as null, which the API refuses,
// and an empty, non-nil slice as [], which clears every skip.
func TestSkippedStepsSendANilSliceAsNull(t *testing.T) {
	for _, tt := range []struct {
		name  string
		steps []OnboardingStep
		want  string
	}{
		{"nil", nil, `{"steps":null}`},
		{"empty", []OnboardingStep{}, `{"steps":[]}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t, func(int, *http.Request, recorded) (*http.Response, error) {
				return newResponse(200, "application/json", onboardingJSON), nil
			})
			if _, err := h.client.Onboarding.SetSkippedSteps(context.Background(), OnboardingSkippedStepsSet{Steps: tt.steps}); err != nil {
				t.Fatal(err)
			}
			if got := string(h.transport.get(0).body); got != tt.want {
				t.Errorf("sent %s, want %s", got, tt.want)
			}
		})
	}
}

// TestAStepTheSDKDoesNotListIsKept decodes a progress whose current step,
// and one step's step, the SDK does not list: each is kept as it arrived,
// and Valid reports it unknown.
func TestAStepTheSDKDoesNotListIsKept(t *testing.T) {
	const later = "a_step_added_later"
	h := newHarness(t, func(int, *http.Request, recorded) (*http.Response, error) {
		return newResponse(200, "application/json", `{"current_step":"`+later+`","has_published":false,"steps":[{"completed":true,"skipped_at":null,"step":"add_client"},{"completed":false,"skipped_at":null,"step":"`+later+`"}]}`), nil
	})
	got, err := h.client.Onboarding.Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.CurrentStep == nil || *got.CurrentStep != later || got.CurrentStep.Valid() {
		t.Errorf("CurrentStep = %v, want %s kept and not Valid", got.CurrentStep, later)
	}
	if len(got.Steps) != 2 || got.Steps[0].Step != OnboardingStepAddClient || got.Steps[1].Step != later {
		t.Errorf("Steps = %+v, want add_client then %s", got.Steps, later)
	}
}

// TestANullCurrentStepIsNil decodes a progress with every step done.
func TestANullCurrentStepIsNil(t *testing.T) {
	h := newHarness(t, func(int, *http.Request, recorded) (*http.Response, error) {
		return newResponse(200, "application/json", `{"current_step":null,"has_published":true,"steps":[]}`), nil
	})
	got, err := h.client.Onboarding.Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.CurrentStep != nil {
		t.Errorf("CurrentStep = %v, want nil", *got.CurrentStep)
	}
}

// TestValueModelArchivedAt reads archived_at as an instant, and null as nil.
func TestValueModelArchivedAt(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name, value string
		want        *time.Time
	}{
		{"archived", `"2026-08-02T13:15:00+09:00"`, new(time.Date(2026, 8, 2, 4, 15, 0, 0, time.UTC))},
		{"active", `null`, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, func(int, *http.Request, recorded) (*http.Response, error) {
				return newResponse(200, "application/json", `{"id":"model-2","archived_at":`+tc.value+`}`), nil
			})
			model, err := h.client.ValueModels.Get(ctx, "model-2")
			if err != nil {
				t.Fatal(err)
			}
			switch {
			case tc.want == nil && model.ArchivedAt != nil:
				t.Errorf("ArchivedAt = %v, want nil", *model.ArchivedAt)
			case tc.want != nil && (model.ArchivedAt == nil || !model.ArchivedAt.Equal(*tc.want)):
				t.Errorf("ArchivedAt = %v, want %v", model.ArchivedAt, *tc.want)
			}
		})
	}
}

// TestAReportStateTheSDKDoesNotListIsKept decodes a state the generated
// constants do not name, and keeps it as the server sent it.
func TestAReportStateTheSDKDoesNotListIsKept(t *testing.T) {
	archived := strings.Replace(summaryJSON, `"state":"draft"`, `"state":"archived"`, 1)
	h := newHarness(t, func(int, *http.Request, recorded) (*http.Response, error) {
		return newResponse(200, "application/json", `{"data":[`+archived+`,`+summaryJSON+`],`+lastPage+`}`), nil
	})
	page, err := h.client.Reports.List(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Data) != 2 {
		t.Fatalf("%d items, want 2", len(page.Data))
	}
	if got := page.Data[0].State; got != ReportState("archived") || got.Valid() {
		t.Errorf("State = %q, Valid() = %t; want the unlisted \"archived\", not valid", got, got.Valid())
	}
	if got := page.Data[1].State; got != ReportStateDraft || !got.Valid() {
		t.Errorf("State = %q, want %q", got, ReportStateDraft)
	}
}

// TestAReadMappingIsSentBackAsTheNextCommitsMapping reads an import, turns
// its mapping into a request's with ToImportMapping, and commits with it:
// the mapping part holds the four members as they were read.
func TestAReadMappingIsSentBackAsTheNextCommitsMapping(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, func(n int, _ *http.Request, _ recorded) (*http.Response, error) {
		if n == 0 {
			return newResponse(200, "application/json", importJSON), nil
		}
		return newResponse(200, "application/json", `{"accepted":0,"batch_id":"import-2","duplicates":0,"errors":[],"errors_truncated":false,"rejected":0}`), nil
	})
	read, err := h.client.Imports.Get(ctx, "import-1")
	if err != nil {
		t.Fatal(err)
	}
	mapping := read.Mapping.ToImportMapping()
	if _, err := h.client.Imports.Commit(ctx, ImportUpload{Mapping: mapping, File: strings.NewReader("Time;Agent\n")}); err != nil {
		t.Fatal(err)
	}
	const want = `{"columns":{"agent_key":"Agent","timestamp":"Time"},"constants":{"category":"task"},"delimiter":";","timezone":"Asia/Tokyo"}`
	if body := string(h.transport.get(1).body); !strings.Contains(body, want) {
		t.Errorf("the commit's mapping part is not %s:\n%s", want, body)
	}
	// A copy: changing the result leaves the read mapping as it was.
	(*mapping.Columns)["timestamp"] = "Other"
	if read.Mapping.Columns["timestamp"] != "Time" {
		t.Errorf("ToImportMapping shares its Columns with the read mapping")
	}
}

// TestADelimiterTheSDKDoesNotListIsKept decodes a delimiter no constant
// names, and sends it back as it is.
func TestADelimiterTheSDKDoesNotListIsKept(t *testing.T) {
	unlisted := strings.Replace(importJSON, `"delimiter":";"`, `"delimiter":"|"`, 1)
	h := newHarness(t, func(int, *http.Request, recorded) (*http.Response, error) {
		return newResponse(200, "application/json", unlisted), nil
	})
	read, err := h.client.Imports.Get(context.Background(), "import-1")
	if err != nil {
		t.Fatal(err)
	}
	if read.Mapping.Delimiter != "|" {
		t.Errorf("Delimiter = %q, want the unlisted \"|\"", read.Mapping.Delimiter)
	}
	if got := read.Mapping.ToImportMapping().Delimiter; got == nil || *got != "|" {
		t.Errorf("ToImportMapping's Delimiter = %v, want \"|\"", got)
	}
}

// TestToImportMappingSendsNoNull turns a mapping whose maps are nil, as a
// caller-built AppliedImportMapping may hold, into one whose JSON has
// empty objects rather than null.
func TestToImportMappingSendsNoNull(t *testing.T) {
	got, err := json.Marshal(AppliedImportMapping{Timezone: "UTC", Delimiter: ImportDelimiterComma}.ToImportMapping())
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"columns":{},"constants":{},"delimiter":",","timezone":"UTC"}`
	if string(got) != want {
		t.Errorf("ToImportMapping marshals as %s, want %s", got, want)
	}
}
