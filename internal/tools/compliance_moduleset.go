package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/opensvc/oc3-mcp/internal/core"
)

// modulesetDescription tells what a moduleset is, for the moduleset tools.
const modulesetDescription = "A moduleset groups compliance modules, such as linux.files or sec.ssh, which the OpenSVC agent checks and fixes on the nodes and service instances it applies to; it may include other modulesets and rulesets, whose variables tell the modules what to enforce. "

type ListComplianceModulesetsInput struct {
	Name    string `json:"name,omitempty" jsonschema:"optional text the moduleset name contains, case-insensitive"`
	Module  string `json:"module,omitempty" jsonschema:"optional exact module, such as sec.ssh, or with a trailing dot a whole family, such as sec.: the modulesets holding it"`
	Node    string `json:"node,omitempty" jsonschema:"optional exact node name or node ID: the modulesets of that node"`
	Service string `json:"service,omitempty" jsonschema:"optional exact service name or service ID: the modulesets of that service itself, those of its encapsulated service apart"`
	Limit   int    `json:"limit,omitempty" jsonschema:"optional page size between 1 and 200; defaults to 50"`
	Cursor  string `json:"cursor,omitempty" jsonschema:"optional next_cursor returned by a previous call with the same filters"`
}

type ListComplianceModulesetsOutput = core.ComplianceModulesetList

type GetComplianceModulesetInput struct {
	Moduleset string `json:"moduleset" jsonschema:"one exact moduleset name, such as base.linux, or collector moduleset ID"`
}

type GetComplianceModulesetOutput = core.ComplianceModulesetDetails

func RegisterComplianceModulesetTools(registrar *Registrar, service *core.Service) error {
	if err := addTool(
		registrar,
		&mcp.Tool{
			Name:  "list_compliance_modulesets",
			Title: "List compliance modulesets",
			Description: "List the compliance modulesets of the OpenSVC collector, or those of one node or service, or holding one module, sorted by name, each with its own modules and whether the agent fixes them on its own. " +
				modulesetDescription +
				"Only the modulesets published to one of the groups of the caller are listed, every moduleset for a manager. Use get_compliance_moduleset for what one includes and where it applies, list_compliance_runs for the runs of its modules.",
			Annotations: readOnlyClosedWorldAnnotations(),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input ListComplianceModulesetsInput) (*mcp.CallToolResult, ListComplianceModulesetsOutput, error) {
			modulesets, err := service.ListComplianceModulesets(ctx, core.ListComplianceModulesetsOptions{
				Name: input.Name, Module: input.Module, Node: input.Node, Service: input.Service, Limit: input.Limit, Cursor: input.Cursor,
			})
			if err != nil {
				return nil, ListComplianceModulesetsOutput{}, err
			}
			return nil, modulesets, nil
		},
	); err != nil {
		return err
	}
	return addTool(
		registrar,
		&mcp.Tool{
			Name:  "get_compliance_moduleset",
			Title: "Get compliance moduleset",
			Description: "Read one compliance moduleset of the OpenSVC collector by its name or ID: its modules, the modulesets it includes, its rulesets, and the nodes and services it is attached to directly. " +
				"It also gives its publication and responsible groups and the modulesets including it. " +
				modulesetDescription +
				"The modulesets including it, in included_by, carry their own attachments: it also applies where they are attached. At most 50 nodes and 50 services are named; list_nodes and list_services with moduleset read them all. Use list_compliance_runs with module for the runs of its modules.",
			Annotations: readOnlyClosedWorldAnnotations(),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input GetComplianceModulesetInput) (*mcp.CallToolResult, GetComplianceModulesetOutput, error) {
			moduleset, err := service.GetComplianceModuleset(ctx, input.Moduleset)
			if err != nil {
				return nil, GetComplianceModulesetOutput{}, err
			}
			return nil, moduleset, nil
		},
	)
}
