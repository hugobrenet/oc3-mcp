package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/opensvc/oc3-mcp/internal/core"
)

// rulesetDescription tells what a ruleset is, for the ruleset tools.
const rulesetDescription = "A ruleset holds the variables the compliance modules read to know what to enforce, such as files, packages, sysctl or cron entries; an explicit ruleset applies where it is attached, a contextual one to the nodes and services its filterset selects, and a ruleset may include others. "

type ListComplianceRulesetsInput struct {
	Name    string `json:"name,omitempty" jsonschema:"optional text the ruleset name contains, case-insensitive"`
	Type    string `json:"type,omitempty" jsonschema:"optional type: explicit or contextual"`
	Node    string `json:"node,omitempty" jsonschema:"optional exact node name or node ID: the rulesets of that node, attached to it or contextual with a filterset selecting it"`
	Service string `json:"service,omitempty" jsonschema:"optional exact service name or service ID: the rulesets of that service, attached to it or contextual with a filterset selecting it"`
	Limit   int    `json:"limit,omitempty" jsonschema:"optional page size between 1 and 200; defaults to 50"`
	Cursor  string `json:"cursor,omitempty" jsonschema:"optional next_cursor returned by a previous call with the same filters"`
}

type ListComplianceRulesetsOutput = core.ComplianceRulesetList

type GetComplianceRulesetInput struct {
	Ruleset string `json:"ruleset" jsonschema:"one exact ruleset name, such as os.linux.base, or collector ruleset ID"`
}

type GetComplianceRulesetOutput = core.ComplianceRulesetDetails

func RegisterComplianceRulesetTools(registrar *Registrar, service *core.Service) error {
	if err := addTool(
		registrar,
		&mcp.Tool{
			Name:  "list_compliance_rulesets",
			Title: "List compliance rulesets",
			Description: "List the compliance rulesets of the OpenSVC collector, or those of one node or service, sorted by name: ID, name, type and whether it may be attached. " +
				rulesetDescription +
				"With node or service, applies_by tells whether the ruleset is attached to it, or a contextual ruleset whose filterset selects it; the rulesets these include, and those of the modulesets attached to it, apply too: get_compliance_ruleset and get_compliance_moduleset give them. " +
				"Only the rulesets published to one of the groups of the caller are listed, every ruleset for a manager.",
			Annotations: readOnlyClosedWorldAnnotations(),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input ListComplianceRulesetsInput) (*mcp.CallToolResult, ListComplianceRulesetsOutput, error) {
			rulesets, err := service.ListComplianceRulesets(ctx, core.ListComplianceRulesetsOptions{
				Name: input.Name, Type: input.Type, Node: input.Node, Service: input.Service, Limit: input.Limit, Cursor: input.Cursor,
			})
			if err != nil {
				return nil, ListComplianceRulesetsOutput{}, err
			}
			return nil, rulesets, nil
		},
	); err != nil {
		return err
	}
	return addTool(
		registrar,
		&mcp.Tool{
			Name:  "get_compliance_ruleset",
			Title: "Get compliance ruleset",
			Description: "Read one compliance ruleset of the OpenSVC collector by its name or ID: its variables, the rulesets it includes with theirs, its filterset with the nodes it selects, and what uses it. " +
				rulesetDescription +
				"It also gives its publication and responsible groups, the rulesets including it, the modulesets it is attached to, and the nodes and services it is attached to directly, among those the caller sees, at most 50 of each named; list_nodes and list_services with ruleset read them all. " +
				"Values are cut to 2 KiB: this MCP masks the values of the variables and keys that name a secret or a mail and the email addresses, but partially; when values are asked for, show them saying so, never as free of secrets. list_compliance_history with ruleset gives the versions in which it changed.",
			Annotations: readOnlyClosedWorldAnnotations(),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input GetComplianceRulesetInput) (*mcp.CallToolResult, GetComplianceRulesetOutput, error) {
			ruleset, err := service.GetComplianceRuleset(ctx, input.Ruleset)
			if err != nil {
				return nil, GetComplianceRulesetOutput{}, err
			}
			return nil, ruleset, nil
		},
	)
}
