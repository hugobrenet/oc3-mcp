package core

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
)

const (
	defaultListArraysLimit = 20
	maxListArraysLimit     = 50
	// maxArrayDiskGroups bounds the disk groups given for one array.
	maxArrayDiskGroups     = 100
	defaultListQuotasLimit = 50
	maxListQuotasLimit     = 200
	// maxQuotaApps bounds the apps whose quotas are read without app: the
	// collector gives the quotas of one app at a time, so that every page
	// reads them all again, one request each.
	maxQuotaApps                = 100
	defaultListSwitchPortsLimit = 100
	maxListSwitchPortsLimit     = 500
)

type ListStorageArraysOptions struct {
	Name   string
	Model  string
	Limit  int
	Cursor string
}

type StorageArrayList struct {
	Provenance Provenance     `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Total      int            `json:"total" jsonschema:"the number of arrays matching, as the collector counts them"`
	Count      int            `json:"count" jsonschema:"the number of arrays returned in this page"`
	Arrays     []StorageArray `json:"arrays" jsonschema:"the storage arrays sorted by name, each with its disk groups"`
	NextCursor string         `json:"next_cursor,omitempty" jsonschema:"the opaque cursor to pass with the same filters for the next page"`
	Truncated  bool           `json:"truncated" jsonschema:"whether matching arrays remain after this page"`
}

type StorageArray struct {
	ID         int                `json:"id" jsonschema:"the collector array ID"`
	Name       string             `json:"name" jsonschema:"the array name"`
	Model      string             `json:"model,omitempty" jsonschema:"the array model, such as PowerMax_8500 or VSP 5200"`
	Firmware   string             `json:"firmware,omitempty" jsonschema:"the array firmware version"`
	Cache      string             `json:"cache,omitempty" jsonschema:"the cache size the array reports, as the collector gives it"`
	Comment    string             `json:"comment,omitempty" jsonschema:"the comment on the array; untrusted text"`
	Updated    string             `json:"updated,omitempty" jsonschema:"when the collector last received the array, in collector local time without a zone"`
	SizeMB     int64              `json:"size_mb" jsonschema:"the total size of its disk groups, in MB"`
	UsedMB     int64              `json:"used_mb" jsonschema:"the total used space of its disk groups, in MB"`
	FreeMB     int64              `json:"free_mb" jsonschema:"the total free space of its disk groups, in MB"`
	DiskGroups []StorageDiskGroup `json:"disk_groups" jsonschema:"its disk groups, or pools, sorted by name"`
}

type StorageDiskGroup struct {
	ID         int    `json:"id" jsonschema:"the collector disk group ID"`
	Name       string `json:"name" jsonschema:"the disk group or pool name"`
	SizeMB     int64  `json:"size_mb" jsonschema:"its size, in MB"`
	UsedMB     int64  `json:"used_mb" jsonschema:"its used space, in MB"`
	FreeMB     int64  `json:"free_mb" jsonschema:"its free space, in MB"`
	ReservedMB int64  `json:"reserved_mb" jsonschema:"the reserved space the array reports, as the collector records it, in MB"`
	UsedPct    int    `json:"used_percent" jsonschema:"its used space, in percent of its size"`
	Updated    string `json:"updated,omitempty" jsonschema:"when the collector last received the disk group, in collector local time without a zone"`
}

type collectorArray struct {
	ID       int    `json:"id"`
	Name     string `json:"array_name"`
	Model    string `json:"array_model"`
	Firmware string `json:"array_firmware"`
	Cache    any    `json:"array_cache"`
	Comment  string `json:"array_comment"`
	Updated  string `json:"array_updated"`
}

type collectorDiskGroup struct {
	ID       int    `json:"id"`
	Name     string `json:"dg_name"`
	Size     int64  `json:"dg_size"`
	Used     int64  `json:"dg_used"`
	Free     int64  `json:"dg_free"`
	Reserved int64  `json:"dg_reserved"`
	Updated  string `json:"dg_updated"`
}

// ListStorageArrays reads one page of the storage arrays of the collector,
// sorted by name, with their disk groups and their capacity.
func (s *Service) ListStorageArrays(ctx context.Context, options ListStorageArraysOptions) (StorageArrayList, error) {
	for name, value := range map[string]string{"name": options.Name, "model": options.Model} {
		if err := validateFilterValue(name, value); err != nil {
			return StorageArrayList{}, err
		}
	}
	limit, err := pageLimit(options.Limit, defaultListArraysLimit, maxListArraysLimit)
	if err != nil {
		return StorageArrayList{}, err
	}
	var filters []string
	if options.Name != "" {
		filters = append(filters, filterContains("array_name", options.Name))
	}
	if options.Model != "" {
		filters = append(filters, filterContains("array_model", options.Model))
	}
	fingerprint := listFingerprint(url.Values{"filter": filters})
	offset, err := decodeListCursor(options.Cursor, fingerprint)
	if err != nil {
		return StorageArrayList{}, err
	}
	props := []string{"id", "array_name", "array_model", "array_firmware", "array_cache", "array_comment", "array_updated"}
	var response collectorList[collectorArray]
	if err := s.client.GetJSON(ctx, "/arrays", pageQuery(props, "array_name,id", filters, limit, offset), &response); err != nil {
		return StorageArrayList{}, fmt.Errorf("list arrays: %w", err)
	}
	if response.Meta.Total == nil || *response.Meta.Total < 0 || len(response.Data) > limit {
		return StorageArrayList{}, fmt.Errorf("list arrays: unexpected collector response")
	}
	result := StorageArrayList{Total: *response.Meta.Total, Count: len(response.Data), Arrays: make([]StorageArray, 0, len(response.Data))}
	for _, row := range response.Data {
		array := StorageArray{
			ID: row.ID, Name: row.Name, Model: strings.TrimSpace(row.Model), Firmware: strings.TrimSpace(row.Firmware),
			Cache: strings.TrimSpace(propText(map[string]any{"v": row.Cache}, "v")), Comment: strings.TrimSpace(row.Comment), Updated: row.Updated,
			DiskGroups: []StorageDiskGroup{},
		}
		// The array is given by its ID: the collector does not find it by its
		// name in this path.
		var groups collectorList[collectorDiskGroup]
		query := pageQuery([]string{"id", "dg_name", "dg_size", "dg_used", "dg_free", "dg_reserved", "dg_updated"}, "dg_name,id", nil, maxArrayDiskGroups, 0)
		if err := s.client.GetJSON(ctx, "/arrays/"+strconv.Itoa(row.ID)+"/diskgroups", query, &groups); err != nil {
			return StorageArrayList{}, fmt.Errorf("list disk groups of array %s: %w", row.Name, err)
		}
		for _, group := range groups.Data {
			dg := StorageDiskGroup{
				ID: group.ID, Name: group.Name, SizeMB: group.Size, UsedMB: group.Used, FreeMB: group.Free, ReservedMB: group.Reserved, Updated: group.Updated,
			}
			if group.Size > 0 {
				dg.UsedPct = int(group.Used * 100 / group.Size)
			}
			array.SizeMB, array.UsedMB, array.FreeMB = array.SizeMB+group.Size, array.UsedMB+group.Used, array.FreeMB+group.Free
			array.DiskGroups = append(array.DiskGroups, dg)
		}
		result.Arrays = append(result.Arrays, array)
	}
	if next := offset + len(response.Data); len(response.Data) > 0 && next < result.Total {
		result.NextCursor = encodeListCursor(next, fingerprint)
		result.Truncated = true
	}
	result.Provenance = s.newProvenance()
	return result, nil
}

type ListStorageQuotasOptions struct {
	App    string
	Array  string
	Limit  int
	Cursor string
}

type StorageQuotaList struct {
	Provenance    Provenance     `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Total         int            `json:"total" jsonschema:"the number of quotas matching"`
	ScanTruncated bool           `json:"scan_truncated,omitempty" jsonschema:"without app, whether the MCP read the quotas of the first 100 apps only, so that other quotas may match"`
	Count         int            `json:"count" jsonschema:"the number of quotas returned in this page"`
	Quotas        []StorageQuota `json:"quotas" jsonschema:"the quotas sorted by app, array and disk group"`
	NextCursor    string         `json:"next_cursor,omitempty" jsonschema:"the opaque cursor to pass with the same filters for the next page"`
	Truncated     bool           `json:"truncated" jsonschema:"whether matching quotas remain after this page"`
}

type StorageQuota struct {
	App          string `json:"app" jsonschema:"the app code the quota is given to"`
	Array        string `json:"array" jsonschema:"the storage array of the disk group"`
	ArrayModel   string `json:"array_model,omitempty" jsonschema:"the model of the array"`
	DiskGroup    string `json:"disk_group" jsonschema:"the disk group or pool the quota is on"`
	QuotaMB      int64  `json:"quota_mb" jsonschema:"the quota of the app on the disk group, in MB"`
	UsedMB       int64  `json:"used_mb" jsonschema:"the space of the quota the app uses, in MB; zero when the collector does not link the disks of the app to the disk group, which needs the disk group of the disks: the agents of the nodes do not report it, only the array feeds may"`
	GroupSizeMB  int64  `json:"disk_group_size_mb" jsonschema:"the size of the disk group, in MB"`
	GroupFreeMB  int64  `json:"disk_group_free_mb" jsonschema:"the free space of the disk group, in MB"`
	ReservableMB int64  `json:"disk_group_reservable_mb" jsonschema:"the space of the disk group still reservable by quotas, in MB"`
}

type collectorQuota struct {
	App        string `json:"app"`
	ArrayName  string `json:"array_name"`
	ArrayModel string `json:"array_model"`
	DgName     string `json:"dg_name"`
	Quota      int64  `json:"quota"`
	QuotaUsed  int64  `json:"quota_used"`
	DgSize     int64  `json:"dg_size"`
	DgFree     int64  `json:"dg_free"`
	Reservable int64  `json:"dg_reservable"`
}

// ListStorageQuotas reads the storage quotas of the apps on the disk groups
// of the arrays, of one app or of all the apps the caller sees, optionally on
// one array.
func (s *Service) ListStorageQuotas(ctx context.Context, options ListStorageQuotasOptions) (StorageQuotaList, error) {
	for name, value := range map[string]string{"app": options.App, "array": options.Array} {
		if err := validateFilterValue(name, value); err != nil {
			return StorageQuotaList{}, err
		}
	}
	limit, err := pageLimit(options.Limit, defaultListQuotasLimit, maxListQuotasLimit)
	if err != nil {
		return StorageQuotaList{}, err
	}
	fingerprint := listFingerprint(url.Values{"app": {strings.ToLower(options.App)}, "array": {strings.ToLower(options.Array)}})
	offset, err := decodeListCursor(options.Cursor, fingerprint)
	if err != nil {
		return StorageQuotaList{}, err
	}
	result := StorageQuotaList{Quotas: []StorageQuota{}}

	// The collector gives the quotas of one app at a time: without app, the
	// quotas of every app the caller sees are read.
	var apps []string
	if options.App != "" {
		apps = []string{options.App}
	} else {
		var response collectorList[collectorApp]
		if err := s.client.GetJSON(ctx, "/apps", pageQuery([]string{"id", "app"}, "app,id", nil, maxQuotaApps, 0), &response); err != nil {
			return StorageQuotaList{}, fmt.Errorf("list apps: %w", err)
		}
		if response.Meta.Total == nil || len(response.Data) > maxQuotaApps {
			return StorageQuotaList{}, fmt.Errorf("list apps: unexpected collector response")
		}
		result.ScanTruncated = *response.Meta.Total > len(response.Data)
		for _, app := range response.Data {
			apps = append(apps, strconv.Itoa(app.ID))
		}
	}
	var filters []string
	if options.Array != "" {
		filters = append(filters, filterEquals("array_name", options.Array))
	}
	var quotas []StorageQuota
	for _, app := range apps {
		var response collectorList[collectorQuota]
		query := pageQuery([]string{"app", "array_name", "array_model", "dg_name", "quota", "quota_used", "dg_size", "dg_free", "dg_reservable"}, "array_name,dg_name", filters, maxArrayDiskGroups, 0)
		if err := s.client.GetJSON(ctx, "/apps/"+url.PathEscape(app)+"/quotas", query, &response); err != nil {
			if options.App != "" {
				return StorageQuotaList{}, fmt.Errorf("list quotas of app %s: %w%s", options.App, err, notFoundHint(err))
			}
			return StorageQuotaList{}, fmt.Errorf("list quotas: %w", err)
		}
		for _, row := range response.Data {
			quotas = append(quotas, StorageQuota{
				App: row.App, Array: row.ArrayName, ArrayModel: strings.TrimSpace(row.ArrayModel), DiskGroup: row.DgName,
				QuotaMB: row.Quota, UsedMB: row.QuotaUsed, GroupSizeMB: row.DgSize, GroupFreeMB: row.DgFree, ReservableMB: row.Reservable,
			})
		}
	}
	sort.SliceStable(quotas, func(i, j int) bool {
		a, b := quotas[i], quotas[j]
		return a.App+"\x00"+a.Array+"\x00"+a.DiskGroup < b.App+"\x00"+b.Array+"\x00"+b.DiskGroup
	})
	result.Total = len(quotas)
	end := min(offset+limit, len(quotas))
	if offset < len(quotas) {
		result.Quotas = quotas[offset:end]
	}
	result.Count = len(result.Quotas)
	if end < len(quotas) && result.Count > 0 {
		result.NextCursor = encodeListCursor(end, fingerprint)
		result.Truncated = true
	}
	result.Provenance = s.newProvenance()
	return result, nil
}

// switchPortTypes are the port types the collector gives the ports of the
// SAN switches, as their switches report them.
var switchPortTypes = []string{"F-Port", "E-Port", "U-Port", "N-Port", "L-Port", "G-Port"}

type ListSANSwitchPortsOptions struct {
	Fabric      string
	Switch      string
	State       string
	Type        string
	ConnectedTo string
	Node        string
	Limit       int
	Cursor      string
}

type SANSwitchPortList struct {
	Provenance Provenance      `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Node       *NodeReference  `json:"node,omitempty" jsonschema:"the node the node filter resolved to: the ports its adapters are plugged in"`
	Total      int             `json:"total" jsonschema:"the number of ports matching, as the collector counts them"`
	Count      int             `json:"count" jsonschema:"the number of ports returned in this page"`
	Ports      []SANSwitchPort `json:"ports" jsonschema:"the ports sorted by fabric, switch, slot and port"`
	NextCursor string          `json:"next_cursor,omitempty" jsonschema:"the opaque cursor to pass with the same filters for the next page"`
	Truncated  bool            `json:"truncated" jsonschema:"whether matching ports remain after this page"`
	// NamesContaining is set when no port matches.
	NamesContaining map[string][]string `json:"names_containing,omitempty" jsonschema:"when no port matches, for each of fabric, switch and connected_to that no port has: the names containing it, else the first 20 names; call again with one of them to filter on it"`
}

type SANSwitchPort struct {
	Fabric      string `json:"fabric,omitempty" jsonschema:"the fabric of the switch"`
	Switch      string `json:"switch" jsonschema:"the switch name"`
	Slot        int    `json:"slot" jsonschema:"the slot of the port"`
	Port        int    `json:"port" jsonschema:"the port number in its slot"`
	Index       int    `json:"index" jsonschema:"the port index on the switch"`
	SpeedGbps   int    `json:"speed_gbps" jsonschema:"the port speed, in Gbit/s"`
	Negotiation string `json:"negotiation,omitempty" jsonschema:"the speed negotiation mode the switch reports"`
	Type        string `json:"type,omitempty" jsonschema:"the port type: F-Port for a device such as a node adapter or an array port, E-Port for a link to another switch, U-Port for a port without a device"`
	State       string `json:"state,omitempty" jsonschema:"the port state, such as Online or No_Light for a port without signal"`
	PortName    string `json:"port_wwn,omitempty" jsonschema:"the world wide name of the port"`
	RemotePort  string `json:"remote_port_wwn,omitempty" jsonschema:"the world wide name of the port plugged on the other end"`
	ConnectedTo string `json:"connected_to,omitempty" jsonschema:"what owns the port on the other end: a node, a storage array or another switch"`
	NodeID      string `json:"node_id,omitempty" jsonschema:"the collector ID of the node on the other end, when it is a node"`
	Updated     string `json:"updated,omitempty" jsonschema:"when the collector last received the port, in collector local time without a zone"`
}

type collectorSwitchPort struct {
	Fabric     string `json:"sw_fabric"`
	Name       string `json:"sw_name"`
	Index      int    `json:"sw_index"`
	Slot       int    `json:"sw_slot"`
	Port       int    `json:"sw_port"`
	Speed      int    `json:"sw_portspeed"`
	Nego       string `json:"sw_portnego"`
	Type       string `json:"sw_porttype"`
	State      string `json:"sw_portstate"`
	PortName   string `json:"sw_portname"`
	RemotePort string `json:"sw_rportname"`
	RemoteName string `json:"sw_rname"`
	NodeID     string `json:"node_id"`
	Updated    string `json:"sw_updated"`
}

// ListSANSwitchPorts reads one page of the ports of the SAN switches, with
// what is plugged on their other end.
func (s *Service) ListSANSwitchPorts(ctx context.Context, options ListSANSwitchPortsOptions) (SANSwitchPortList, error) {
	for name, value := range map[string]string{"fabric": options.Fabric, "switch": options.Switch, "state": options.State, "connected_to": options.ConnectedTo} {
		if err := validateFilterValue(name, value); err != nil {
			return SANSwitchPortList{}, err
		}
	}
	if options.Type != "" && !slices.Contains(switchPortTypes, options.Type) {
		return SANSwitchPortList{}, fmt.Errorf("type must be one of %s", strings.Join(switchPortTypes, ", "))
	}
	limit, err := pageLimit(options.Limit, defaultListSwitchPortsLimit, maxListSwitchPortsLimit)
	if err != nil {
		return SANSwitchPortList{}, err
	}
	result := SANSwitchPortList{Ports: []SANSwitchPort{}}
	var filters []string
	if options.Node != "" {
		node, err := s.lookupNode(ctx, options.Node, []string{"node_id", "nodename", "cluster_id"})
		if err != nil {
			return SANSwitchPortList{}, err
		}
		result.Node = &NodeReference{NodeID: node.NodeID, Nodename: node.Nodename, ClusterID: node.ClusterID}
		filters = append(filters, filterEquals("node_id", node.NodeID))
	}
	for _, filter := range [][2]string{{"sw_fabric", options.Fabric}, {"sw_name", options.Switch}, {"sw_portstate", options.State}, {"sw_porttype", options.Type}, {"sw_rname", options.ConnectedTo}} {
		if filter[1] != "" {
			filters = append(filters, filterEquals(filter[0], filter[1]))
		}
	}
	fingerprint := listFingerprint(url.Values{"filter": filters})
	offset, err := decodeListCursor(options.Cursor, fingerprint)
	if err != nil {
		return SANSwitchPortList{}, err
	}
	props := []string{"sw_fabric", "sw_name", "sw_index", "sw_slot", "sw_port", "sw_portspeed", "sw_portnego", "sw_porttype", "sw_portstate", "sw_portname", "sw_rportname", "sw_rname", "node_id", "sw_updated"}
	var response collectorList[collectorSwitchPort]
	if err := s.client.GetJSON(ctx, "/san-switches", pageQuery(props, "sw_fabric,sw_name,sw_slot,sw_port,id", filters, limit, offset), &response); err != nil {
		return SANSwitchPortList{}, fmt.Errorf("list SAN switch ports: %w", err)
	}
	if response.Meta.Total == nil || *response.Meta.Total < 0 || len(response.Data) > limit {
		return SANSwitchPortList{}, fmt.Errorf("list SAN switch ports: unexpected collector response")
	}
	result.Provenance = s.newProvenance()
	result.Total, result.Count = *response.Meta.Total, len(response.Data)
	for _, row := range response.Data {
		result.Ports = append(result.Ports, SANSwitchPort{
			Fabric: row.Fabric, Switch: row.Name, Slot: row.Slot, Port: row.Port, Index: row.Index, SpeedGbps: row.Speed, Negotiation: row.Nego,
			Type: row.Type, State: row.State, PortName: row.PortName, RemotePort: row.RemotePort, ConnectedTo: row.RemoteName, NodeID: row.NodeID, Updated: row.Updated,
		})
	}
	if next := offset + len(response.Data); len(response.Data) > 0 && next < result.Total {
		result.NextCursor = encodeListCursor(next, fingerprint)
		result.Truncated = true
	}
	if result.Total == 0 {
		for _, name := range []struct{ input, prop, value string }{
			{"fabric", "sw_fabric", options.Fabric}, {"switch", "sw_name", options.Switch}, {"connected_to", "sw_rname", options.ConnectedTo},
		} {
			if name.value == "" {
				continue
			}
			names, err := s.sanNamesContaining(ctx, name.input, name.prop, name.value)
			if err != nil {
				return SANSwitchPortList{}, err
			}
			if names != nil {
				if result.NamesContaining == nil {
					result.NamesContaining = map[string][]string{}
				}
				result.NamesContaining[name.input] = names
			}
		}
	}
	return result, nil
}

// maxSANNames bounds the names given for an unknown fabric, switch or
// connected_to.
const maxSANNames = 20

// sanNamesContaining gives, for a fabric, switch or connected_to name no
// switch port has, the names containing it, else the first names, and nil
// for a name a port has: a model tends to pass a short form, such as a for
// lyn-fabric-a, and would read the empty page as no port. The names are a
// result, not an error, so that the model may call again with one of them.
func (s *Service) sanNamesContaining(ctx context.Context, input, prop, value string) ([]string, error) {
	names := func(filters []string) ([]string, error) {
		var response collectorList[map[string]any]
		query := pageQuery([]string{prop}, prop, filters, maxSANNames, 0)
		query.Set("groupby", prop)
		if err := s.client.GetJSON(ctx, "/san-switches", query, &response); err != nil {
			return nil, fmt.Errorf("list SAN %s names: %w", input, err)
		}
		names := []string{}
		for _, row := range response.Data {
			if name := propText(row, prop); name != "" {
				names = append(names, name)
			}
		}
		return names, nil
	}
	matching, err := names([]string{filterContains(prop, value)})
	if err != nil || slices.Contains(matching, value) {
		return nil, err
	}
	if len(matching) > 0 {
		return matching, nil
	}
	return names(nil)
}
