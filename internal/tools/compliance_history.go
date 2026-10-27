package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/opensvc/oc3-mcp/internal/core"
)

type ListComplianceHistoryInput struct {
	Ruleset   string `json:"ruleset,omitempty" jsonschema:"optional exact ruleset name or ID: the versions in which it changed"`
	Moduleset string `json:"moduleset,omitempty" jsonschema:"optional exact moduleset name or ID: the versions in which it changed"`
	Filterset string `json:"filterset,omitempty" jsonschema:"optional exact filterset name or ID: the versions in which it changed"`
	Limit     int    `json:"limit,omitempty" jsonschema:"optional number of versions, 1 to 100, default 20"`
}

type GetComplianceVersionInput struct {
	Commit string `json:"commit" jsonschema:"the commit of the version, as list_compliance_history gives it, full or its first characters"`
}

type ListComplianceHistoryOutput = core.ComplianceHistory
type GetComplianceVersionOutput = core.ComplianceVersionDetails

// RegisterComplianceHistoryTools registers the tools reading the versions of
// the compliance configuration the collector records.
func RegisterComplianceHistoryTools(registrar *Registrar, service *core.Service) error {
	if err := addTool(registrar, &mcp.Tool{
		Name: "list_compliance_history", Title: "List compliance history",
		Description: "List the versions of the compliance configuration of the OpenSVC collector, its rulesets, modulesets and filtersets, the newest first: who recorded each, when, with which message, and its source. " +
			"With ruleset, moduleset or filterset, only the versions in which that object changed, among the last 300. A version is recorded when the designer of the collector or apply_compliance_changes commits a change, and gathers then the changes made by other means; by_assistant tells those apply_compliance_changes recorded. " +
			"get_compliance_version reads what a version changed. Requires the CompManager or the Manager privilege.",
		Annotations: readOnlyClosedWorldAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input ListComplianceHistoryInput) (*mcp.CallToolResult, ListComplianceHistoryOutput, error) {
		result, err := service.ListComplianceHistory(ctx, input.Ruleset, input.Moduleset, input.Filterset, input.Limit)
		if err != nil {
			return nil, ListComplianceHistoryOutput{}, err
		}
		return nil, result, nil
	}); err != nil {
		return err
	}
	return addTool(registrar, &mcp.Tool{
		Name: "get_compliance_version", Title: "Get compliance version",
		Description: "Read one version of the compliance configuration of the OpenSVC collector, by its commit from list_compliance_history: what it changed compared with the version before. " +
			"It gives the rulesets, modulesets and filtersets it added, removed or changed, and the line diff of the configuration, cut to 32 KiB. " +
			"The diff masks the values of the variables naming a secret, the sensitive keys of their JSON values and the email addresses, but partially: show values saying so, never as free of secrets. get_compliance_ruleset and get_compliance_moduleset read an object as it is now. Requires the CompManager or the Manager privilege.",
		Annotations: readOnlyClosedWorldAnnotations(),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input GetComplianceVersionInput) (*mcp.CallToolResult, GetComplianceVersionOutput, error) {
		result, err := service.GetComplianceVersion(ctx, input.Commit)
		if err != nil {
			return nil, GetComplianceVersionOutput{}, err
		}
		return nil, result, nil
	})
}
