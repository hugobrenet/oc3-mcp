package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/opensvc/oc3-mcp/internal/core"
)

type DeleteServiceInput struct {
	Service string `json:"service" jsonschema:"one exact service name or collector service ID; a name several clusters hold is refused with their IDs"`
}

type DeleteServiceOutput = core.ServiceDeletion

type DeleteServiceInstanceInput struct {
	Service string `json:"service" jsonschema:"one exact service name or collector service ID; a name several clusters hold is refused with their IDs"`
	Node    string `json:"node" jsonschema:"one exact node name or collector node ID"`
}

type DeleteServiceInstanceOutput = core.ServiceInstanceDeletion

// registerServiceDeleteTools registers the tools that delete services and
// service instances. RegisterNodeActionTools calls it: they are registered
// only when actions are enabled.
func registerServiceDeleteTools(registrar *Registrar, service *core.Service) error {
	if err := addTool(
		registrar,
		&mcp.Tool{
			Name:  "delete_service",
			Title: "Delete service",
			Description: "Delete one service from the OpenSVC collector, irreversibly, with its instances and its history. That is its states and their history, actions and queued actions, compliance status, logs and attached rulesets and modulesets, checks, resources, disks, tags, alerts, saves and form results. " +
				"It does not stop or change the service on its cluster: a service an agent still reports comes back at the next status of its cluster, with the same ID and without the history deleted, and the result warns of it. Use it for a service no agent reports any more, such as one of a cluster gone. " +
				"For the services of a cluster that still reports, the @purge tag, attached with attach_service_tag, has the collector purge each of them once it has no instance left. Requires to be responsible for the service.",
			Annotations: actionAnnotations(true, true),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input DeleteServiceInput) (*mcp.CallToolResult, DeleteServiceOutput, error) {
			result, err := service.DeleteService(ctx, input.Service)
			if err != nil {
				return nil, DeleteServiceOutput{}, err
			}
			return nil, result, nil
		},
	); err != nil {
		return err
	}
	return addTool(
		registrar,
		&mcp.Tool{
			Name:  "delete_service_instance",
			Title: "Delete service instance",
			Description: "Delete one instance of a service, on one node, from the OpenSVC collector: its current state, resources, live checks and alerts; the history of the service stays. " +
				"It does not stop or change the instance on its node: an instance its node still reports comes back at its next status, and the result warns of it. The collector already purges by itself the instance of an object a node that still reports no longer has: use it for the instance of a node that reports no more. " +
				"Requires to be responsible for the service.",
			Annotations: actionAnnotations(true, true),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input DeleteServiceInstanceInput) (*mcp.CallToolResult, DeleteServiceInstanceOutput, error) {
			result, err := service.DeleteServiceInstance(ctx, input.Service, input.Node)
			if err != nil {
				return nil, DeleteServiceInstanceOutput{}, err
			}
			return nil, result, nil
		},
	)
}
