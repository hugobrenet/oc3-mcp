package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/opensvc/oc3-mcp/internal/core"
)

type JustifyServiceDowntimeInput struct {
	Service string `json:"service" jsonschema:"one exact service name or service ID"`
	Begin   string `json:"begin" jsonschema:"the begin of the period, YYYY-MM-DD HH:MM:SS in collector local time, exactly as get_service_status_history gives it"`
	Comment string `json:"comment" jsonschema:"why the service was down, such as a planned maintenance, as the user gives it; ask the user, never invent it; at most 1024 bytes"`
	Counted bool   `json:"counted" jsonschema:"true keeps the period in the availability rate; false leaves it out, as for a planned maintenance the SLA does not count"`
}

type UnjustifyServiceDowntimeInput struct {
	Service string `json:"service" jsonschema:"one exact service name or service ID"`
	Begin   string `json:"begin" jsonschema:"the begin of the period, YYYY-MM-DD HH:MM:SS in collector local time, exactly as get_service_status_history gives it"`
}

type ServiceDowntimeOutput = core.ServiceDowntimeChange

// registerServiceDowntimeTools registers the tools that justify the
// downtime periods of a service and remove a justification.
// RegisterNodeActionTools calls it: they are registered only when actions
// are enabled.
func registerServiceDowntimeTools(registrar *Registrar, service *core.Service) error {
	if err := addTool(
		registrar,
		&mcp.Tool{
			Name:  "justify_service_downtime",
			Title: "Justify service downtime",
			Description: "Justify one period of downtime of one OpenSVC service in its status history, with a comment the collector records with the caller and the date, and whether the period still counts in the availability rate. " +
				"Use it when the user explains a downtime, such as a planned maintenance: counted false leaves the period out of the availability rate the SLA is compared with. " +
				"Name the period by its begin, exactly as get_service_status_history gives it; a justification already there is replaced. A period the service was up, or one still going on, is refused. " +
				"The last period of a service whose agents stopped reporting may be justified, but grows again when they resume with the same status: its justification then no longer shows on it, and still leaves the original period out of the rate. " +
				"It is not a maintenance window: it does not snooze any alert, and only a period in the history is justified. The comment is required: ask the user for it, never invent it. " +
				"Requires to be responsible for the service.",
			Annotations: actionAnnotations(true, true),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input JustifyServiceDowntimeInput) (*mcp.CallToolResult, ServiceDowntimeOutput, error) {
			result, err := service.JustifyServiceDowntime(ctx, core.JustifyServiceDowntimeOptions{
				Service: input.Service, Begin: input.Begin, Comment: input.Comment, Counted: input.Counted,
			})
			if err != nil {
				return nil, ServiceDowntimeOutput{}, err
			}
			return nil, result, nil
		},
	); err != nil {
		return err
	}
	if err := addTool(
		registrar,
		&mcp.Tool{
			Name:  "unjustify_service_downtime",
			Title: "Unjustify service downtime",
			Description: "Remove the justification of one period of downtime of one OpenSVC service: the period counts again in the availability rate, and its comment is deleted. " +
				"Name the period by its begin, exactly as get_service_status_history gives it. A period not justified is left as it is; a justified period the service was up is accepted, as one given in the collector web interface. " +
				"Requires to be responsible for the service.",
			Annotations: actionAnnotations(true, true),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input UnjustifyServiceDowntimeInput) (*mcp.CallToolResult, ServiceDowntimeOutput, error) {
			result, err := service.UnjustifyServiceDowntime(ctx, core.UnjustifyServiceDowntimeOptions{Service: input.Service, Begin: input.Begin})
			if err != nil {
				return nil, ServiceDowntimeOutput{}, err
			}
			return nil, result, nil
		},
	); err != nil {
		return err
	}
	return nil
}
