package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/opensvc/oc3-mcp/internal/core"
)

type SetServiceSLAInput struct {
	SLA    *float64 `json:"sla,omitempty" jsonschema:"the availability target, a percent between 0 and 100, such as 99.9; not with remove"`
	Remove bool     `json:"remove,omitempty" jsonschema:"true removes the availability target; not with sla"`
	ServiceSelectionInput
}

type SetServiceSLAOutput = core.ServiceSLAChange

// registerServiceSLATools registers the tool that sets the availability
// target of services. RegisterNodeActionTools calls it: it is registered
// only when actions are enabled.
func registerServiceSLATools(registrar *Registrar, service *core.Service) error {
	return addTool(
		registrar,
		&mcp.Tool{
			Name:  "set_service_sla",
			Title: "Set service SLA",
			Description: "Set the SLA of one or several OpenSVC services, the availability target in percent the collector shows beside the availability it computes over 30 days, or remove it. The value replaces the one the services had; the agents never send it. A service already as asked is told unchanged. " +
				"Nothing follows from the SLA in the collector: no alert and no report when a service falls below it. The result gives the SLA and the availability of each service, and get_service those of one. " +
				"The target is rounded to three decimals, as the collector stores it. " +
				serviceSelection("") +
				"Requires to be responsible for the services, by their app.",
			Annotations: actionAnnotations(false, true),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input SetServiceSLAInput) (*mcp.CallToolResult, SetServiceSLAOutput, error) {
			result, err := service.SetServiceSLA(ctx, core.ServiceSLAOptions{SLA: input.SLA, Remove: input.Remove, ServiceSelection: input.selection()})
			if err != nil {
				return nil, SetServiceSLAOutput{}, err
			}
			return nil, result, nil
		},
	)
}
