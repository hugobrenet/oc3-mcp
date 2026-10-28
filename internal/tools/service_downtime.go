package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/opensvc/oc3-mcp/internal/core"
)

type JustifyServiceDowntimeInput struct {
	Service string `json:"service" jsonschema:"one exact service name or service ID"`
	Begin   string `json:"begin" jsonschema:"the begin of the period, YYYY-MM-DD HH:MM:SS in collector local time, exactly as get_service_status_history gives it"`
	End     string `json:"end,omitempty" jsonschema:"optional end of the period, YYYY-MM-DD HH:MM:SS in collector local time: with it, the period is named by both its bounds, sent as is to the collector, for the period still going on; never to cover several periods, which shows on none: justify each by its begin"`
	Comment string `json:"comment" jsonschema:"why the service was down, such as a planned maintenance, as the user gives it; ask the user, never invent it; the collector refuses an empty one"`
	Counted bool   `json:"counted" jsonschema:"true keeps the period in the availability rate; false leaves it out, as for a planned maintenance the SLA does not count"`
}

type UnjustifyServiceDowntimeInput struct {
	Service string `json:"service" jsonschema:"one exact service name or service ID"`
	Begin   string `json:"begin" jsonschema:"the begin of the period, YYYY-MM-DD HH:MM:SS in collector local time, exactly as get_service_status_history gives it"`
	End     string `json:"end,omitempty" jsonschema:"optional end of the period, YYYY-MM-DD HH:MM:SS in collector local time: with it, the period is named by both its bounds, sent as is to the collector, such as the bounds of a justification whose period grew since"`
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
				"Name the period by its begin, exactly as get_service_status_history gives it; a justification already there is replaced. A period still going on is refused, unless end names its bounds; a period the service was up is justified as the others, without effect on the rate. With end, the bounds go as is to the collector, a period of the history or not: a justification on bounds no period has, such as those of several periods, leaves their span out of the rate but shows on no period. To justify an outage made of several down periods, justify each by its begin. " +
				"The last period of a service whose agents stopped reporting may be justified, but grows again when they resume with the same status: its justification then no longer shows on it, and still leaves the original period out of the rate. " +
				"It is not a maintenance window: it does not snooze any alert, and only a period in the history is justified. The comment is required: ask the user for it, never invent it. " +
				"Requires to be responsible for the service.",
			Annotations: actionAnnotations(false, true),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input JustifyServiceDowntimeInput) (*mcp.CallToolResult, ServiceDowntimeOutput, error) {
			result, err := service.JustifyServiceDowntime(ctx, core.JustifyServiceDowntimeOptions{
				Service: input.Service, Begin: input.Begin, End: input.End, Comment: input.Comment, Counted: input.Counted,
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
				"With end, the bounds go as is to the collector: a justification whose period grew since, which the history no longer shows on any period, is removed by its own bounds, the collector answering not found when it holds none. " +
				"Requires to be responsible for the service.",
			Annotations: actionAnnotations(false, true),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input UnjustifyServiceDowntimeInput) (*mcp.CallToolResult, ServiceDowntimeOutput, error) {
			result, err := service.UnjustifyServiceDowntime(ctx, core.UnjustifyServiceDowntimeOptions{Service: input.Service, Begin: input.Begin, End: input.End})
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
