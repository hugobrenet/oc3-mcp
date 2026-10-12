package core

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const (
	defaultListNodeDisksLimit = 100
	maxListNodeDisksLimit     = 200
	// maxDiskSharingRows bounds the rows read to tell which other nodes see
	// the disks of a page.
	maxDiskSharingRows = 5000
)

// worldWideDiskID matches the disk IDs that name one disk wherever it is seen,
// such as a NAA world wide identifier. Local devices carry their device name,
// such as vda, which other nodes reuse for other disks.
var worldWideDiskID = regexp.MustCompile(`^[0-9a-fA-F]{16,}$`)

var nodeDiskProps = []string{
	"disk_id", "disk_size", "disk_used", "disk_vendor", "disk_model", "disk_arrayid", "disk_group", "disk_dg", "disk_region",
	"svc_id", "svcname", "updated",
}

type ListNodeDisksOptions struct {
	Node   string
	DiskID string
	Limit  int
	Cursor string
}

type NodeDiskList struct {
	Provenance Provenance    `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Node       NodeReference `json:"node" jsonschema:"the node whose disks are listed"`
	Total      int           `json:"total" jsonschema:"the number of disk rows matching the filters, as the collector counts them: one per disk and service using it"`
	Count      int           `json:"count" jsonschema:"the number of disk rows returned in this page"`
	Disks      []NodeDisk    `json:"disks" jsonschema:"the disks sorted by disk ID, then service"`
	NextCursor string        `json:"next_cursor,omitempty" jsonschema:"the opaque cursor to pass with the same filters for the next page"`
	Truncated  bool          `json:"truncated" jsonschema:"whether matching disk rows remain after this page"`
}

type NodeDisk struct {
	DiskID     string   `json:"disk_id" jsonschema:"the disk ID: a world wide identifier for a shared disk, or the device name, such as vda, for a local one"`
	SizeMB     int64    `json:"size_mb" jsonschema:"the disk size in MiB"`
	UsedMB     int64    `json:"used_mb" jsonschema:"the size the node uses on the disk, in MiB"`
	Vendor     string   `json:"vendor,omitempty" jsonschema:"the disk vendor"`
	Model      string   `json:"model,omitempty" jsonschema:"the disk model"`
	Array      string   `json:"array,omitempty" jsonschema:"the storage array of the disk, as the collector knows it"`
	ArrayGroup string   `json:"array_disk_group,omitempty" jsonschema:"the disk group of the disk in its storage array"`
	DiskGroup  string   `json:"disk_group,omitempty" jsonschema:"the disk group the node reports for the disk, such as a volume group or a pool"`
	Region     string   `json:"region,omitempty" jsonschema:"the region of the disk the node uses"`
	SvcID      string   `json:"svc_id,omitempty" jsonschema:"the collector ID of the service using the disk, when the collector knows it"`
	Svcname    string   `json:"svcname,omitempty" jsonschema:"the name of the service using the disk"`
	SharedWith []string `json:"shared_with" jsonschema:"the other nodes seeing the same disk; empty for a local disk named by its device"`
	Updated    string   `json:"updated,omitempty" jsonschema:"when the collector last received the disk, as the collector gives it"`
}

type collectorDisk struct {
	DiskID      string `json:"disk_id"`
	DiskSize    int64  `json:"disk_size"`
	DiskUsed    int64  `json:"disk_used"`
	DiskVendor  string `json:"disk_vendor"`
	DiskModel   string `json:"disk_model"`
	DiskArrayID string `json:"disk_arrayid"`
	DiskGroup   string `json:"disk_group"`
	DiskDG      string `json:"disk_dg"`
	DiskRegion  string `json:"disk_region"`
	SvcID       string `json:"svc_id"`
	Svcname     string `json:"svcname"`
	Updated     string `json:"updated"`
	Nodename    string `json:"nodename"`
	NodeID      string `json:"node_id"`
}

// ListNodeDisks reads one page of the disks one node sees, as the collector
// knows them, with the other nodes seeing each shared disk.
func (s *Service) ListNodeDisks(ctx context.Context, options ListNodeDisksOptions) (NodeDiskList, error) {
	if err := validateFilterValue("disk_id", options.DiskID); err != nil {
		return NodeDiskList{}, err
	}
	limit, err := pageLimit(options.Limit, defaultListNodeDisksLimit, maxListNodeDisksLimit)
	if err != nil {
		return NodeDiskList{}, err
	}
	node, err := s.lookupNode(ctx, options.Node, []string{"node_id", "nodename", "cluster_id"})
	if err != nil {
		return NodeDiskList{}, err
	}
	filters := []string{filterEquals("node_id", node.NodeID)}
	if options.DiskID != "" {
		filters = append(filters, filterContains("disk_id", options.DiskID))
	}
	fingerprint := listFingerprint(url.Values{"filter": filters})
	offset, err := decodeListCursor(options.Cursor, fingerprint)
	if err != nil {
		return NodeDiskList{}, err
	}

	var response collectorList[collectorDisk]
	if err := s.client.GetJSON(ctx, "/disks", pageQuery(nodeDiskProps, "disk_id,svc_id", filters, limit, offset), &response); err != nil {
		return NodeDiskList{}, fmt.Errorf("list disks of node %s: %w", node.Nodename, err)
	}
	if response.Meta.Total == nil || *response.Meta.Total < 0 || len(response.Data) > limit {
		return NodeDiskList{}, fmt.Errorf("list disks of node %s: unexpected collector response", node.Nodename)
	}
	sharing, err := s.diskSharing(ctx, node.NodeID, response.Data)
	if err != nil {
		return NodeDiskList{}, fmt.Errorf("list disks of node %s: the other nodes of the disks: %w", node.Nodename, err)
	}
	result := NodeDiskList{
		Provenance: s.newProvenance(), Node: NodeReference{NodeID: node.NodeID, Nodename: node.Nodename, ClusterID: node.ClusterID},
		Total: *response.Meta.Total, Count: len(response.Data), Disks: make([]NodeDisk, 0, len(response.Data)),
	}
	for _, disk := range response.Data {
		shared := sharing[disk.DiskID]
		if shared == nil {
			shared = []string{}
		}
		result.Disks = append(result.Disks, NodeDisk{
			DiskID: disk.DiskID, SizeMB: disk.DiskSize, UsedMB: disk.DiskUsed, Vendor: strings.TrimSpace(disk.DiskVendor),
			Model: strings.TrimSpace(disk.DiskModel), Array: disk.DiskArrayID, ArrayGroup: disk.DiskGroup, DiskGroup: disk.DiskDG,
			Region: disk.DiskRegion, SvcID: disk.SvcID, Svcname: disk.Svcname, SharedWith: shared, Updated: disk.Updated,
		})
	}
	if next := offset + len(response.Data); len(response.Data) > 0 && next < result.Total {
		result.NextCursor = encodeListCursor(next, fingerprint)
		result.Truncated = true
	}
	return result, nil
}

// diskSharing maps each world wide disk ID of disks to the names of the other
// nodes seeing it.
func (s *Service) diskSharing(ctx context.Context, nodeID string, disks []collectorDisk) (map[string][]string, error) {
	seen := make(map[string]bool)
	var ids []string
	for _, disk := range disks {
		if worldWideDiskID.MatchString(disk.DiskID) && !seen[disk.DiskID] {
			seen[disk.DiskID] = true
			ids = append(ids, disk.DiskID)
		}
	}
	sharing := make(map[string][]string, len(ids))
	if len(ids) == 0 {
		return sharing, nil
	}
	query := url.Values{}
	query.Set("props", "disk_id,node_id,nodename")
	query.Add("filter", "disk_id:in:"+strings.Join(ids, ","))
	query.Set("limit", strconv.Itoa(maxDiskSharingRows))
	query.Set("meta", "false")
	var response collectorList[collectorDisk]
	if err := s.client.GetJSON(ctx, "/disks", query, &response); err != nil {
		return nil, err
	}
	nodes := make(map[string]map[string]bool)
	for _, row := range response.Data {
		if row.NodeID == nodeID || row.Nodename == "" {
			continue
		}
		if nodes[row.DiskID] == nil {
			nodes[row.DiskID] = make(map[string]bool)
		}
		nodes[row.DiskID][row.Nodename] = true
	}
	for id, names := range nodes {
		list := make([]string, 0, len(names))
		for name := range names {
			list = append(list, name)
		}
		sort.Strings(list)
		sharing[id] = list
	}
	return sharing, nil
}
