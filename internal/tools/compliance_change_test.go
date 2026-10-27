package tools

import (
	"slices"
	"testing"

	"github.com/opensvc/oc3-mcp/internal/core"
)

// The input schema of apply_compliance_changes is what the model sees: the
// values of its lists, and its bounds.
func TestComplianceChangesSchemaListsTheValues(t *testing.T) {
	schema, err := complianceOperationsSchema()
	if err != nil {
		t.Fatal(err)
	}
	operations := schema.Properties["operations"]
	if operations.MinItems == nil || *operations.MinItems != 1 || operations.MaxItems == nil || *operations.MaxItems != 100 {
		t.Fatalf("operations bounds = %v, %v", operations.MinItems, operations.MaxItems)
	}
	for name, want := range map[string][]string{
		"op": core.ComplianceOperations, "kind": core.ComplianceKinds, "child_kind": core.ComplianceKinds,
		"role": core.ComplianceTeamRoles, "to_role": core.ComplianceTeamRoles, "ruleset_type": core.ComplianceRulesetTypes,
	} {
		var values []string
		for _, value := range operations.Items.Properties[name].Enum {
			values = append(values, value.(string))
		}
		if !slices.Equal(values, want) {
			t.Fatalf("%s enum = %v, want %v", name, values, want)
		}
	}
	if len(core.ComplianceOperations) != 20 {
		t.Fatalf("operations = %d, the collector takes 20", len(core.ComplianceOperations))
	}
	if !slices.Contains(schema.Required, "message") || !slices.Contains(schema.Required, "operations") || !slices.Equal(operations.Items.Required, []string{"op", "kind"}) {
		t.Fatalf("required = %v, %v", schema.Required, operations.Items.Required)
	}
}
