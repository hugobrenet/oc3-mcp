package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/opensvc/oc3-mcp/internal/core"
)

type ListPackageDifferencesInput struct {
	Nodes   []string `json:"nodes,omitempty" jsonschema:"optional exact node names or node IDs to compare, at most 50"`
	Service string   `json:"service,omitempty" jsonschema:"optional exact service name or service ID whose nodes to compare, with the given nodes"`
	Encap   bool     `json:"encap,omitempty" jsonschema:"optional; with service, compares the encapsulated nodes of the service, its containers or virtual machines, instead of its hosts"`
	Name    string   `json:"name,omitempty" jsonschema:"optional text the package name contains, case-insensitive"`
}

type ListPackageDifferencesOutput = core.PackageDifferences

type ListNodePackagesInput struct {
	Node   string `json:"node" jsonschema:"one exact node name or node ID"`
	Name   string `json:"name,omitempty" jsonschema:"optional text the package name contains, case-insensitive"`
	Limit  int    `json:"limit,omitempty" jsonschema:"optional page size between 1 and 500; defaults to 100"`
	Cursor string `json:"cursor,omitempty" jsonschema:"optional next_cursor returned by a previous call with the same filters"`
}

type ListNodePackagesOutput = core.NodePackageList

func RegisterPackageTools(registrar *Registrar, service *core.Service) error {
	if err := addTool(
		registrar,
		&mcp.Tool{
			Name:  "list_node_packages",
			Title: "List node packages",
			Description: "List the packages the OpenSVC agent of one node reported installed to the collector, sorted by name: name, version, architecture, type, signing key provider, install date and when the agent last reported it. " +
				"Use it to tell which version of a package, such as opensvc or openssl, a node runs. " +
				"Use list_package_differences to compare nodes. Collector dates are as it gives them.",
			Annotations: readOnlyClosedWorldAnnotations(),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input ListNodePackagesInput) (*mcp.CallToolResult, ListNodePackagesOutput, error) {
			packages, err := service.ListNodePackages(ctx, core.ListNodePackagesOptions{Node: input.Node, Name: input.Name, Limit: input.Limit, Cursor: input.Cursor})
			if err != nil {
				return nil, ListNodePackagesOutput{}, err
			}
			return nil, packages, nil
		},
	); err != nil {
		return err
	}
	return addTool(
		registrar,
		&mcp.Tool{
			Name:  "list_package_differences",
			Title: "List package differences",
			Description: "Compare the packages the OpenSVC agents reported installed on several nodes, as the collector does for its package differences in cluster alert: the nodes given, plus the nodes of a service, or with encap its encapsulated nodes. " +
				"Returns the packages that differ, grouped by package with their versions and the nodes having each: version_mismatch when every node has the package in different versions, missing when some nodes do not have it, version mismatches first. " +
				"Pass at least two nodes, or a service. At most 500 packages are returned.",
			Annotations: readOnlyClosedWorldAnnotations(),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input ListPackageDifferencesInput) (*mcp.CallToolResult, ListPackageDifferencesOutput, error) {
			differences, err := service.ListPackageDifferences(ctx, core.ListPackageDifferencesOptions{
				Nodes: input.Nodes, Service: input.Service, Encap: input.Encap, Name: input.Name,
			})
			if err != nil {
				return nil, ListPackageDifferencesOutput{}, err
			}
			return nil, differences, nil
		},
	)
}
