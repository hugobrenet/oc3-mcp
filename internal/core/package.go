package core

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
)

const (
	maxPackageDiffNodes    = 50
	maxPackageDifferences  = 500
	packageVersionMismatch = "version_mismatch"
	packageMissing         = "missing"
)

type ListPackageDifferencesOptions struct {
	Nodes   []string
	Service string
	Encap   bool
	Name    string
}

type PackageDifferences struct {
	Provenance        Provenance          `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Service           *ServiceReference   `json:"service,omitempty" jsonschema:"the service whose nodes are compared"`
	Encap             bool                `json:"encap" jsonschema:"whether the encapsulated nodes of the service are compared instead of its hosts"`
	ComparedNodes     []NodeReference     `json:"compared_nodes" jsonschema:"the nodes the collector compared"`
	Total             int                 `json:"total" jsonschema:"the number of packages differing between the compared nodes, after the name filter"`
	VersionMismatches int                 `json:"version_mismatches" jsonschema:"the number of packages installed on every compared node, but not in the same version everywhere"`
	Missing           int                 `json:"missing" jsonschema:"the number of packages missing on some compared nodes"`
	Count             int                 `json:"count" jsonschema:"the number of packages returned"`
	Packages          []PackageDifference `json:"packages" jsonschema:"the differing packages: version mismatches first, then by name, architecture and type"`
	Truncated         bool                `json:"truncated" jsonschema:"whether packages were left out after 500"`
}

type PackageDifference struct {
	Name      string           `json:"name" jsonschema:"the package name"`
	Arch      string           `json:"arch,omitempty" jsonschema:"the package architecture"`
	Type      string           `json:"type,omitempty" jsonschema:"the package type, such as deb or rpm"`
	Kind      string           `json:"kind" jsonschema:"version_mismatch when every compared node has the package in different versions, missing when some nodes do not have it"`
	Versions  []PackageVersion `json:"versions" jsonschema:"the installed versions and the nodes having each"`
	MissingOn []string         `json:"missing_on,omitempty" jsonschema:"the compared nodes not having the package"`
}

type PackageVersion struct {
	Version string   `json:"version" jsonschema:"the installed version"`
	Nodes   []string `json:"nodes" jsonschema:"the nodes having this version"`
}

type collectorPackageDiff struct {
	Data []struct {
		NodeID     string `json:"node_id"`
		PkgName    string `json:"pkg_name"`
		PkgArch    string `json:"pkg_arch"`
		PkgType    string `json:"pkg_type"`
		PkgVersion string `json:"pkg_version"`
	} `json:"data"`
	Meta struct {
		Nodes []struct {
			NodeID   string `json:"node_id"`
			Nodename string `json:"nodename"`
		} `json:"nodes"`
	} `json:"meta"`
}

// ListPackageDifferences compares the packages installed on the given nodes,
// plus the nodes of the given service, and groups the differences the
// collector reports by package.
func (s *Service) ListPackageDifferences(ctx context.Context, options ListPackageDifferencesOptions) (PackageDifferences, error) {
	if err := validateFilterValue("name", options.Name); err != nil {
		return PackageDifferences{}, err
	}
	if len(options.Nodes) > maxPackageDiffNodes {
		return PackageDifferences{}, fmt.Errorf("nodes accepts at most %d nodes", maxPackageDiffNodes)
	}
	if options.Encap && options.Service == "" {
		return PackageDifferences{}, fmt.Errorf("encap requires a service")
	}
	if options.Service == "" && len(options.Nodes) < 2 {
		return PackageDifferences{}, fmt.Errorf("pass at least two nodes, or a service whose nodes to compare")
	}
	result := PackageDifferences{Encap: options.Encap}
	query := url.Values{}
	if len(options.Nodes) > 0 {
		ids := make([]string, 0, len(options.Nodes))
		seen := make(map[string]bool, len(options.Nodes))
		for _, name := range options.Nodes {
			node, err := s.lookupNode(ctx, name, []string{"node_id", "nodename", "cluster_id"})
			if err != nil {
				return PackageDifferences{}, err
			}
			if !seen[node.NodeID] {
				seen[node.NodeID] = true
				ids = append(ids, node.NodeID)
			}
		}
		query.Set("node_ids", strings.Join(ids, ","))
	}
	if options.Service != "" {
		service, err := s.lookupService(ctx, options.Service, []string{"svc_id", "svcname", "cluster_id"})
		if err != nil {
			return PackageDifferences{}, err
		}
		result.Service = &ServiceReference{SvcID: service.SvcID, Svcname: service.Svcname, ClusterID: service.ClusterID}
		query.Set("svc_ids", service.SvcID)
		if options.Encap {
			query.Set("encap", "true")
		}
	}

	var response collectorPackageDiff
	if err := s.client.GetJSON(ctx, "/packages/diff", query, &response); err != nil {
		return PackageDifferences{}, fmt.Errorf("list package differences: %w", err)
	}
	if response.Data == nil {
		return PackageDifferences{}, fmt.Errorf("list package differences: unexpected collector response")
	}
	names := make(map[string]string, len(response.Meta.Nodes))
	result.ComparedNodes = make([]NodeReference, 0, len(response.Meta.Nodes))
	for _, node := range response.Meta.Nodes {
		names[node.NodeID] = node.Nodename
		result.ComparedNodes = append(result.ComparedNodes, NodeReference{NodeID: node.NodeID, Nodename: node.Nodename})
	}
	nodename := func(id string) string {
		if name := names[id]; name != "" {
			return name
		}
		return id
	}

	type packageKey struct{ name, arch, kind string }
	versions := make(map[packageKey]map[string][]string)
	for _, row := range response.Data {
		if options.Name != "" && !strings.Contains(strings.ToLower(row.PkgName), strings.ToLower(options.Name)) {
			continue
		}
		key := packageKey{row.PkgName, row.PkgArch, row.PkgType}
		if versions[key] == nil {
			versions[key] = make(map[string][]string)
		}
		versions[key][row.PkgVersion] = append(versions[key][row.PkgVersion], nodename(row.NodeID))
	}
	packages := make([]PackageDifference, 0, len(versions))
	for key, byVersion := range versions {
		item := PackageDifference{Name: key.name, Arch: key.arch, Type: key.kind}
		having := make(map[string]bool)
		for version, nodes := range byVersion {
			sort.Strings(nodes)
			item.Versions = append(item.Versions, PackageVersion{Version: version, Nodes: nodes})
			for _, node := range nodes {
				having[node] = true
			}
		}
		sort.Slice(item.Versions, func(i, j int) bool { return item.Versions[i].Version < item.Versions[j].Version })
		for _, node := range result.ComparedNodes {
			if !having[nodename(node.NodeID)] {
				item.MissingOn = append(item.MissingOn, nodename(node.NodeID))
			}
		}
		if len(item.MissingOn) == 0 {
			item.Kind = packageVersionMismatch
			result.VersionMismatches++
		} else {
			item.Kind = packageMissing
			result.Missing++
		}
		packages = append(packages, item)
	}
	sort.Slice(packages, func(i, j int) bool {
		a, b := packages[i], packages[j]
		if (a.Kind == packageVersionMismatch) != (b.Kind == packageVersionMismatch) {
			return a.Kind == packageVersionMismatch
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		if a.Arch != b.Arch {
			return a.Arch < b.Arch
		}
		return a.Type < b.Type
	})
	result.Total = len(packages)
	if len(packages) > maxPackageDifferences {
		packages = packages[:maxPackageDifferences]
		result.Truncated = true
	}
	result.Packages, result.Count = packages, len(packages)
	result.Provenance = s.newProvenance()
	return result, nil
}

const (
	defaultListNodePackagesLimit = 100
	maxListNodePackagesLimit     = 500
)

var nodePackageProps = []string{"id", "pkg_name", "pkg_version", "pkg_arch", "pkg_type", "sig_provider", "pkg_install_date", "pkg_updated"}

type ListNodePackagesOptions struct {
	Node   string
	Name   string
	Limit  int
	Cursor string
}

type NodePackageList struct {
	Provenance Provenance    `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Node       NodeReference `json:"node" jsonschema:"the node whose packages are listed"`
	Total      int           `json:"total" jsonschema:"the number of packages matching the filters, as the collector counts them"`
	Count      int           `json:"count" jsonschema:"the number of packages returned in this page"`
	Packages   []NodePackage `json:"packages" jsonschema:"the packages sorted by name, then architecture"`
	NextCursor string        `json:"next_cursor,omitempty" jsonschema:"the opaque cursor to pass with the same filters for the next page"`
	Truncated  bool          `json:"truncated" jsonschema:"whether matching packages remain after this page"`
}

type NodePackage struct {
	Name           string `json:"name" jsonschema:"the package name"`
	Version        string `json:"version" jsonschema:"the installed version"`
	Arch           string `json:"arch,omitempty" jsonschema:"the package architecture"`
	Type           string `json:"type,omitempty" jsonschema:"the package type, such as deb or rpm"`
	SignedBy       string `json:"signed_by,omitempty" jsonschema:"the provider of the key that signed the package, when the collector knows it"`
	InstalledAt    string `json:"installed_at,omitempty" jsonschema:"when the package was installed, as the agent reported it"`
	LastReportedAt string `json:"last_reported_at,omitempty" jsonschema:"when the agent last reported the package, as the collector gives it"`
}

type collectorNodePackage struct {
	PkgName        string `json:"pkg_name"`
	PkgVersion     string `json:"pkg_version"`
	PkgArch        string `json:"pkg_arch"`
	PkgType        string `json:"pkg_type"`
	SigProvider    string `json:"sig_provider"`
	PkgInstallDate string `json:"pkg_install_date"`
	PkgUpdated     string `json:"pkg_updated"`
}

// ListNodePackages reads one page of the packages the agent of one node
// reported installed, sorted by name.
func (s *Service) ListNodePackages(ctx context.Context, options ListNodePackagesOptions) (NodePackageList, error) {
	if err := validateFilterValue("name", options.Name); err != nil {
		return NodePackageList{}, err
	}
	limit, err := pageLimit(options.Limit, defaultListNodePackagesLimit, maxListNodePackagesLimit)
	if err != nil {
		return NodePackageList{}, err
	}
	node, err := s.lookupNode(ctx, options.Node, []string{"node_id", "nodename", "cluster_id"})
	if err != nil {
		return NodePackageList{}, err
	}
	filters := []string{filterEquals("node_id", node.NodeID)}
	if options.Name != "" {
		filters = append(filters, filterContains("pkg_name", options.Name))
	}
	fingerprint := listFingerprint(url.Values{"filter": filters})
	offset, err := decodeListCursor(options.Cursor, fingerprint)
	if err != nil {
		return NodePackageList{}, err
	}

	var response collectorList[collectorNodePackage]
	if err := s.client.GetJSON(ctx, "/packages", pageQuery(nodePackageProps, "pkg_name,pkg_arch,id", filters, limit, offset), &response); err != nil {
		return NodePackageList{}, fmt.Errorf("list packages of node %s: %w", node.Nodename, err)
	}
	if response.Meta.Total == nil || *response.Meta.Total < 0 || len(response.Data) > limit {
		return NodePackageList{}, fmt.Errorf("list packages of node %s: unexpected collector response", node.Nodename)
	}
	result := NodePackageList{
		Provenance: s.newProvenance(), Node: NodeReference{NodeID: node.NodeID, Nodename: node.Nodename, ClusterID: node.ClusterID},
		Total: *response.Meta.Total, Count: len(response.Data), Packages: make([]NodePackage, 0, len(response.Data)),
	}
	for _, pkg := range response.Data {
		result.Packages = append(result.Packages, NodePackage{
			Name: pkg.PkgName, Version: pkg.PkgVersion, Arch: pkg.PkgArch, Type: pkg.PkgType, SignedBy: pkg.SigProvider,
			InstalledAt: pkg.PkgInstallDate, LastReportedAt: pkg.PkgUpdated,
		})
	}
	if next := offset + len(response.Data); len(response.Data) > 0 && next < result.Total {
		result.NextCursor = encodeListCursor(next, fingerprint)
		result.Truncated = true
	}
	return result, nil
}
