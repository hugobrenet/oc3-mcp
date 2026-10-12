package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/opensvc/oc3-mcp/internal/core"
)

type ListServicesInput struct {
	Name        string `json:"name,omitempty" jsonschema:"optional text the service name contains, case-insensitive"`
	ClusterID   string `json:"cluster_id,omitempty" jsonschema:"optional exact cluster ID"`
	App         string `json:"app,omitempty" jsonschema:"optional exact collector app"`
	Env         string `json:"env,omitempty" jsonschema:"optional exact service environment, such as PRD or DEV"`
	AvailStatus string `json:"availstatus,omitempty" jsonschema:"optional exact availability status, such as up, down, warn, n/a or undef"`
	Node        string `json:"node,omitempty" jsonschema:"optional exact node name or node ID; lists only the services with an instance on that node"`
	Limit       int    `json:"limit,omitempty" jsonschema:"optional page size between 1 and 200; defaults to 50"`
	Cursor      string `json:"cursor,omitempty" jsonschema:"optional next_cursor returned by a previous call with the same filters"`
}

type ListServicesOutput = core.ServiceList

type GetServiceInput struct {
	Service string `json:"service" jsonschema:"one exact service name, such as ns/svc/name, or collector service ID"`
}

type GetServiceOutput = core.ServiceDetails

type GetServiceStatusHistoryInput struct {
	Service string `json:"service" jsonschema:"one exact service name, such as ns/svc/name, or collector service ID"`
	Node    string `json:"node,omitempty" jsonschema:"optional exact node name or node ID; returns the history of the instance on that node instead of the service"`
	Days    int    `json:"days,omitempty" jsonschema:"optional days of history between 1 and 365; defaults to 7"`
}

type GetServiceStatusHistoryOutput = core.ServiceStatusHistory

func RegisterServiceTools(registrar *Registrar, service *core.Service) error {
	if err := addTool(
		registrar,
		&mcp.Tool{
			Name:  "list_services",
			Title: "List collector services",
			Description: "List the services the OpenSVC collector knows, sorted by name: service ID, cluster ID, app, environment, topology, aggregated availability, overall, freeze, placement and provisioned states, configured nodes and status update time. " +
				"Filter by text in the name, exact cluster ID, app, environment or availability status, and by node to list only the services with an instance on it; use the cursor to continue a page. " +
				"States are those the collector last received. Collector dates are as it gives them. Use get_service for the instances of one service.",
			Annotations: readOnlyClosedWorldAnnotations(),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input ListServicesInput) (*mcp.CallToolResult, ListServicesOutput, error) {
			services, err := service.ListServices(ctx, core.ListServicesOptions{
				Name: input.Name, ClusterID: input.ClusterID, App: input.App, Env: input.Env, AvailStatus: input.AvailStatus,
				Node: input.Node, Limit: input.Limit, Cursor: input.Cursor,
			})
			if err != nil {
				return nil, ListServicesOutput{}, err
			}
			return nil, services, nil
		},
	); err != nil {
		return err
	}
	if err := addTool(
		registrar,
		&mcp.Tool{
			Name:  "get_service",
			Title: "Get collector service",
			Description: "Read one service of the OpenSVC collector by exact name or service ID: identity, topology and configured nodes, aggregated status, 30-day availability rate and SLA, " +
				"and its instances, one per node, with their availability, overall, monitor and freeze states and the status of each resource kind. " +
				"A name several services hold is refused with their service IDs; pass one of them. The configuration text is left out. " +
				"States are those the collector last received; collector dates are as it gives them.",
			Annotations: readOnlyClosedWorldAnnotations(),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input GetServiceInput) (*mcp.CallToolResult, GetServiceOutput, error) {
			details, err := service.GetService(ctx, input.Service)
			if err != nil {
				return nil, GetServiceOutput{}, err
			}
			return nil, details, nil
		},
	); err != nil {
		return err
	}
	if err := addTool(
		registrar,
		&mcp.Tool{
			Name:  "get_service_status_history",
			Title: "Get service status history",
			Description: "Read the status history of one OpenSVC service from the collector over the last days, 7 by default and 365 at most: the periods of one availability status, oldest first, with their justification when a responsible gave one, " +
				"and the availability rate the collector computes over them, where the time no status covers counts as downtime and justified downtime marked not to count is left out. " +
				"With node, the periods of one availability and overall status of the instance on that node instead, without rate. Use it to tell since when a service or instance is down. " +
				"Names several items hold are refused with their IDs. The 500 most recent periods are kept. Collector dates are in its local time without a zone.",
			Annotations: readOnlyClosedWorldAnnotations(),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input GetServiceStatusHistoryInput) (*mcp.CallToolResult, GetServiceStatusHistoryOutput, error) {
			history, err := service.GetServiceStatusHistory(ctx, core.ServiceStatusHistoryOptions{Service: input.Service, Node: input.Node, Days: input.Days})
			if err != nil {
				return nil, GetServiceStatusHistoryOutput{}, err
			}
			return nil, history, nil
		},
	); err != nil {
		return err
	}
	return nil
}
