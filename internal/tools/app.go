package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/opensvc/oc3-mcp/internal/core"
)

type GetAppInput struct {
	App string `json:"app" jsonschema:"one exact app code, as nodes and services carry it, such as WEB, or collector app ID"`
}

type GetAppOutput = core.App

func RegisterAppTools(registrar *Registrar, service *core.Service) error {
	return addTool(
		registrar,
		&mcp.Tool{
			Name:  "get_app",
			Title: "Get collector app",
			Description: "Read one app of the OpenSVC collector by exact code or ID, to tell who is responsible for it. An app is the code that groups nodes and services for their responsibles: the result holds its description, domain, operations team, the collector groups responsible for it, and the number of its nodes and services. " +
				"Use it to tell who is responsible for an app, a node or a service: their app field gives the code. " +
				"The responsibles are groups, not people; a group named user_ followed by a number is the own group of one collector user. List the nodes or services of the app with list_nodes or list_services and app. Collector dates are as it gives them.",
			Annotations: readOnlyClosedWorldAnnotations(),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input GetAppInput) (*mcp.CallToolResult, GetAppOutput, error) {
			app, err := service.GetApp(ctx, input.App)
			if err != nil {
				return nil, GetAppOutput{}, err
			}
			return nil, app, nil
		},
	)
}
