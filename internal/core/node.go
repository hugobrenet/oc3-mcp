package core

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

const (
	defaultListNodesLimit = 50
	maxListNodesLimit     = 200
)

var nodeSummaryProps = []string{
	"node_id", "nodename", "cluster_id", "app", "node_env", "status", "os_concat", "version", "last_comm", "node_frozen", "node_frozen_at",
}

var nodeDetailProps = []string{
	"node_id", "nodename", "fqdn", "cluster_id", "app", "node_env", "status", "role", "type", "node_frozen", "node_frozen_at", "snooze_till",
	"version", "last_comm", "updated",
	"os_name", "os_vendor", "os_release", "os_kernel", "os_arch", "tz", "last_boot",
	"manufacturer", "model", "serial", "cpu_model", "cpu_vendor", "cpu_cores", "cpu_threads", "cpu_dies", "cpu_freq",
	"mem_bytes", "mem_banks", "mem_slots", "bios_version", "sp_version",
	"team_responsible", "team_integ", "team_support",
	"loc_country", "loc_city", "loc_addr", "loc_zip", "loc_building", "loc_floor", "loc_room", "loc_rack",
	"warranty_end", "maintenance_end", "hw_obs_warn_date", "hw_obs_alert_date", "os_obs_warn_date", "os_obs_alert_date",
}

// NodeFilters are the typed filters of list_nodes and count_nodes.
type NodeFilters struct {
	Name         string
	ClusterID    string
	App          string
	Env          string
	Status       string
	Frozen       bool
	FrozenBefore string
	OSName       string
	OSVendor     string
	OSRelease    string
	AgentVersion string
	Manufacturer string
	Model        string
	Team         string
	Country      string
	City         string
	SilentFor    string
	// Tag keeps the nodes holding this tag, by name or tag ID.
	Tag string
	// Filterset keeps the nodes this filterset selects, by name or ID.
	Filterset string
}

type ListNodesOptions struct {
	NodeFilters
	Limit  int
	Cursor string
}

type NodeList struct {
	Provenance Provenance          `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Tag        *TagReference       `json:"tag,omitempty" jsonschema:"the tag the tag filter resolved to; only the nodes holding it are listed"`
	Filterset  *FiltersetReference `json:"filterset,omitempty" jsonschema:"the filterset the filterset filter resolved to; only the nodes it selects are listed"`
	Total      int                 `json:"total" jsonschema:"the number of nodes matching the filters, as the collector counts them"`
	Count      int                 `json:"count" jsonschema:"the number of nodes returned in this page"`
	Nodes      []NodeSummary       `json:"nodes" jsonschema:"the nodes sorted by node name, then node ID"`
	NextCursor string              `json:"next_cursor,omitempty" jsonschema:"the opaque cursor to pass with the same filters for the next page"`
	Truncated  bool                `json:"truncated" jsonschema:"whether matching nodes remain after this page"`
}

type NodeSummary struct {
	NodeID       string `json:"node_id" jsonschema:"the collector node ID"`
	Nodename     string `json:"nodename" jsonschema:"the node name; several clusters may hold the same name"`
	ClusterID    string `json:"cluster_id,omitempty" jsonschema:"the ID of the OpenSVC cluster of the node"`
	App          string `json:"app,omitempty" jsonschema:"the collector app the node belongs to"`
	Env          string `json:"env,omitempty" jsonschema:"the node environment, such as PRD or DEV"`
	Status       string `json:"status,omitempty" jsonschema:"the node status the collector Nodes page shows, a property the collector users set on the node, whose values each organization chooses, such as Ready, Not Ready, Operating, up or down; it is not reported by the node daemon"`
	OS           string `json:"os,omitempty" jsonschema:"the operating system name, vendor and release"`
	AgentVersion string `json:"agent_version,omitempty" jsonschema:"the OpenSVC agent version the node last reported"`
	LastComm     string `json:"last_comm,omitempty" jsonschema:"the last time the node daemon contacted the collector, in collector local time without a zone"`
	Frozen       bool   `json:"frozen" jsonschema:"whether the node is frozen, as the collector last received it"`
	FrozenAt     string `json:"frozen_at" jsonschema:"when the node was frozen, as the collector gives it; empty when the node is not frozen or when the collector does not store the date"`
}

type NodeDetails struct {
	Provenance Provenance    `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Node       NodeIdentity  `json:"node" jsonschema:"the node identity, classification and freeze state"`
	Agent      NodeAgent     `json:"agent" jsonschema:"the OpenSVC agent and its contact with the collector"`
	OS         NodeOS        `json:"os" jsonschema:"the operating system the node last reported"`
	Hardware   NodeHardware  `json:"hardware" jsonschema:"the hardware the node last reported"`
	Teams      NodeTeams     `json:"teams" jsonschema:"the teams in charge of the node"`
	Location   NodeLocation  `json:"location" jsonschema:"the location of the node"`
	Lifecycle  NodeLifecycle `json:"lifecycle" jsonschema:"warranty, maintenance and obsolescence dates"`
}

type NodeIdentity struct {
	NodeID     string `json:"node_id" jsonschema:"the collector node ID"`
	Nodename   string `json:"nodename" jsonschema:"the node name"`
	FQDN       string `json:"fqdn,omitempty" jsonschema:"the fully qualified domain name"`
	ClusterID  string `json:"cluster_id,omitempty" jsonschema:"the ID of the OpenSVC cluster of the node"`
	App        string `json:"app,omitempty" jsonschema:"the collector app the node belongs to"`
	Env        string `json:"env,omitempty" jsonschema:"the node environment, such as PRD or DEV"`
	Status     string `json:"status,omitempty" jsonschema:"the node status the collector Nodes page shows, a property the collector users set on the node, whose values each organization chooses, such as Ready, Not Ready, Operating, up or down; it is not reported by the node daemon"`
	Role       string `json:"role,omitempty" jsonschema:"the node role property"`
	Type       string `json:"type,omitempty" jsonschema:"the node type, such as physical or virtual"`
	Frozen     bool   `json:"frozen" jsonschema:"whether the node is frozen, as the collector last received it"`
	FrozenAt   string `json:"frozen_at,omitempty" jsonschema:"when the node was frozen, as the collector gives it"`
	SnoozeTill string `json:"snooze_till,omitempty" jsonschema:"the end of the snooze of the node alerts, as the collector gives it"`
}

type NodeAgent struct {
	Version  string `json:"version,omitempty" jsonschema:"the OpenSVC agent version the node last reported"`
	LastComm string `json:"last_comm,omitempty" jsonschema:"the last time the node daemon contacted the collector, in collector local time without a zone"`
	Updated  string `json:"updated,omitempty" jsonschema:"the last update of the node record, in collector local time without a zone"`
}

type NodeOS struct {
	Name     string `json:"name,omitempty" jsonschema:"the operating system name"`
	Vendor   string `json:"vendor,omitempty" jsonschema:"the operating system vendor"`
	Release  string `json:"release,omitempty" jsonschema:"the operating system release"`
	Kernel   string `json:"kernel,omitempty" jsonschema:"the kernel release"`
	Arch     string `json:"arch,omitempty" jsonschema:"the processor architecture"`
	Timezone string `json:"timezone,omitempty" jsonschema:"the time zone offset the node reported"`
	LastBoot string `json:"last_boot,omitempty" jsonschema:"the last boot time the node reported, as the collector gives it"`
}

type NodeHardware struct {
	Manufacturer string `json:"manufacturer,omitempty" jsonschema:"the hardware manufacturer"`
	Model        string `json:"model,omitempty" jsonschema:"the hardware model"`
	Serial       string `json:"serial,omitempty" jsonschema:"the hardware serial number"`
	CPUModel     string `json:"cpu_model,omitempty" jsonschema:"the processor model"`
	CPUVendor    string `json:"cpu_vendor,omitempty" jsonschema:"the processor vendor"`
	CPUCores     int    `json:"cpu_cores" jsonschema:"the number of processor cores the node reported"`
	CPUThreads   int    `json:"cpu_threads" jsonschema:"the number of processor threads the node reported"`
	CPUDies      int    `json:"cpu_dies" jsonschema:"the number of processor sockets the node reported"`
	CPUFreq      string `json:"cpu_freq,omitempty" jsonschema:"the processor frequency the node reported, in MHz"`
	MemMB        int64  `json:"mem_mb" jsonschema:"the memory size in MiB; the collector names it mem_bytes but stores MiB"`
	MemBanks     int    `json:"mem_banks" jsonschema:"the number of populated memory banks"`
	MemSlots     int    `json:"mem_slots" jsonschema:"the number of memory slots"`
	BIOSVersion  string `json:"bios_version,omitempty" jsonschema:"the BIOS version"`
	SPVersion    string `json:"sp_version,omitempty" jsonschema:"the service processor version"`
}

type NodeTeams struct {
	Responsible string `json:"responsible,omitempty" jsonschema:"the team responsible for the node"`
	Integration string `json:"integration,omitempty" jsonschema:"the integration team"`
	Support     string `json:"support,omitempty" jsonschema:"the support team"`
}

type NodeLocation struct {
	Country  string `json:"country,omitempty" jsonschema:"the country"`
	City     string `json:"city,omitempty" jsonschema:"the city"`
	Address  string `json:"address,omitempty" jsonschema:"the street address"`
	Zip      string `json:"zip,omitempty" jsonschema:"the postal code"`
	Building string `json:"building,omitempty" jsonschema:"the building"`
	Floor    string `json:"floor,omitempty" jsonschema:"the floor"`
	Room     string `json:"room,omitempty" jsonschema:"the room"`
	Rack     string `json:"rack,omitempty" jsonschema:"the rack"`
}

type NodeLifecycle struct {
	WarrantyEnd          string `json:"warranty_end,omitempty" jsonschema:"the end of the hardware warranty"`
	MaintenanceEnd       string `json:"maintenance_end,omitempty" jsonschema:"the end of the maintenance contract"`
	HardwareObsWarnDate  string `json:"hardware_obsolescence_warn_date,omitempty" jsonschema:"when the hardware model enters its obsolescence warning period"`
	HardwareObsAlertDate string `json:"hardware_obsolescence_alert_date,omitempty" jsonschema:"when the hardware model becomes obsolete"`
	OSObsWarnDate        string `json:"os_obsolescence_warn_date,omitempty" jsonschema:"when the operating system release enters its obsolescence warning period"`
	OSObsAlertDate       string `json:"os_obsolescence_alert_date,omitempty" jsonschema:"when the operating system release becomes obsolete"`
}

type collectorNode struct {
	NodeID          string `json:"node_id"`
	Nodename        string `json:"nodename"`
	FQDN            string `json:"fqdn"`
	ClusterID       string `json:"cluster_id"`
	App             string `json:"app"`
	NodeEnv         string `json:"node_env"`
	Status          string `json:"status"`
	Role            string `json:"role"`
	Type            string `json:"type"`
	NodeFrozen      string `json:"node_frozen"`
	NodeFrozenAt    string `json:"node_frozen_at"`
	SnoozeTill      string `json:"snooze_till"`
	Version         string `json:"version"`
	LastComm        string `json:"last_comm"`
	Updated         string `json:"updated"`
	OSConcat        string `json:"os_concat"`
	OSName          string `json:"os_name"`
	OSVendor        string `json:"os_vendor"`
	OSRelease       string `json:"os_release"`
	OSKernel        string `json:"os_kernel"`
	OSArch          string `json:"os_arch"`
	TZ              string `json:"tz"`
	LastBoot        string `json:"last_boot"`
	Manufacturer    string `json:"manufacturer"`
	Model           string `json:"model"`
	Serial          string `json:"serial"`
	CPUModel        string `json:"cpu_model"`
	CPUVendor       string `json:"cpu_vendor"`
	CPUCores        int    `json:"cpu_cores"`
	CPUThreads      int    `json:"cpu_threads"`
	CPUDies         int    `json:"cpu_dies"`
	CPUFreq         string `json:"cpu_freq"`
	MemBytes        int64  `json:"mem_bytes"`
	MemBanks        int    `json:"mem_banks"`
	MemSlots        int    `json:"mem_slots"`
	BIOSVersion     string `json:"bios_version"`
	SPVersion       string `json:"sp_version"`
	TeamResponsible string `json:"team_responsible"`
	TeamInteg       string `json:"team_integ"`
	TeamSupport     string `json:"team_support"`
	LocCountry      string `json:"loc_country"`
	LocCity         string `json:"loc_city"`
	LocAddr         string `json:"loc_addr"`
	LocZip          string `json:"loc_zip"`
	LocBuilding     string `json:"loc_building"`
	LocFloor        string `json:"loc_floor"`
	LocRoom         string `json:"loc_room"`
	LocRack         string `json:"loc_rack"`
	WarrantyEnd     string `json:"warranty_end"`
	MaintenanceEnd  string `json:"maintenance_end"`
	HWObsWarnDate   string `json:"hw_obs_warn_date"`
	HWObsAlertDate  string `json:"hw_obs_alert_date"`
	OSObsWarnDate   string `json:"os_obs_warn_date"`
	OSObsAlertDate  string `json:"os_obs_alert_date"`
}

// ListNodes reads one page of the nodes the collector account sees, sorted
// by node name, with typed filters and, with a tag, among the nodes holding
// it.
func (s *Service) ListNodes(ctx context.Context, options ListNodesOptions) (NodeList, error) {
	filters, err := options.NodeFilters.collectorFilters()
	if err != nil {
		return NodeList{}, err
	}
	limit, err := pageLimit(options.Limit, defaultListNodesLimit, maxListNodesLimit)
	if err != nil {
		return NodeList{}, err
	}
	path, tag, filterset, err := s.memberPath(ctx, options.Tag, options.Filterset, "nodes")
	if err != nil {
		return NodeList{}, err
	}
	fingerprint := listFingerprint(url.Values{"path": {path}, "filter": filters})
	offset, err := decodeListCursor(options.Cursor, fingerprint)
	if err != nil {
		return NodeList{}, err
	}

	var response collectorList[collectorNode]
	if err := s.client.GetJSON(ctx, path, pageQuery(nodeSummaryProps, "nodename,node_id", filters, limit, offset), &response); err != nil {
		return NodeList{}, fmt.Errorf("list nodes: %w", err)
	}
	if response.Meta.Total == nil || *response.Meta.Total < 0 || len(response.Data) > limit {
		return NodeList{}, fmt.Errorf("list nodes: unexpected collector response")
	}
	result := NodeList{Provenance: s.newProvenance(), Tag: tag, Filterset: filterset, Total: *response.Meta.Total, Count: len(response.Data), Nodes: make([]NodeSummary, 0, len(response.Data))}
	for _, node := range response.Data {
		result.Nodes = append(result.Nodes, NodeSummary{
			NodeID: node.NodeID, Nodename: node.Nodename, ClusterID: node.ClusterID, App: node.App, Env: node.NodeEnv,
			Status: node.Status, OS: node.OSConcat, AgentVersion: node.Version, LastComm: node.LastComm,
			Frozen: collectorBool(node.NodeFrozen), FrozenAt: node.NodeFrozenAt,
		})
	}
	if next := offset + len(response.Data); len(response.Data) > 0 && next < result.Total {
		result.NextCursor = encodeListCursor(next, fingerprint)
		result.Truncated = true
	}
	return result, nil
}

// silentForPattern is a duration of the collector relative dates: a number
// of days, hours or minutes.
var silentForPattern = regexp.MustCompile(`^[1-9][0-9]{0,4}[dhm]$`)

// collectorFilters translates the filters into collector filters.
func (f NodeFilters) collectorFilters() ([]string, error) {
	for name, value := range map[string]string{
		"name": f.Name, "app": f.App, "env": f.Env, "status": f.Status, "os_name": f.OSName, "os_vendor": f.OSVendor, "os_release": f.OSRelease,
		"agent_version": f.AgentVersion, "manufacturer": f.Manufacturer, "model": f.Model, "team": f.Team, "country": f.Country, "city": f.City,
	} {
		if err := validateFilterValue(name, value); err != nil {
			return nil, err
		}
	}
	if err := validateCollectorUUID("cluster_id", f.ClusterID); err != nil {
		return nil, err
	}
	if err := validateCollectorDate("frozen_before", f.FrozenBefore); err != nil {
		return nil, err
	}
	if f.SilentFor != "" && !silentForPattern.MatchString(f.SilentFor) {
		return nil, fmt.Errorf("silent_for must be a number of days, hours or minutes, such as 7d, 12h or 30m")
	}
	var filters []string
	contains := func(prop, value string) {
		if value != "" {
			filters = append(filters, filterContains(prop, value))
		}
	}
	equals := func(prop, value string) {
		if value != "" {
			filters = append(filters, filterEquals(prop, value))
		}
	}
	family := func(prop, value string) {
		if value != "" {
			filters = append(filters, filterEqualsOrFamily(prop, value))
		}
	}
	contains("nodename", f.Name)
	equals("cluster_id", f.ClusterID)
	equals("app", f.App)
	equals("node_env", f.Env)
	equals("status", f.Status)
	if f.Frozen || f.FrozenBefore != "" {
		filters = append(filters, filterEquals("node_frozen", "T"))
	}
	if f.FrozenBefore != "" {
		filters = append(filters, "node_frozen_at:lte:"+f.FrozenBefore)
	}
	contains("os_name", f.OSName)
	contains("os_vendor", f.OSVendor)
	family("os_release", f.OSRelease)
	family("version", f.AgentVersion)
	contains("manufacturer", f.Manufacturer)
	contains("model", f.Model)
	contains("team_responsible", f.Team)
	contains("loc_country", f.Country)
	contains("loc_city", f.City)
	if f.SilentFor != "" {
		filters = append(filters, "last_comm:lt:-"+f.SilentFor)
	}
	return filters, nil
}

// GetNode reads one node by its exact name or node ID. A name held by
// several nodes is refused with their IDs.
func (s *Service) GetNode(ctx context.Context, node string) (NodeDetails, error) {
	n, err := s.lookupNode(ctx, node, nodeDetailProps)
	if err != nil {
		return NodeDetails{}, err
	}
	return NodeDetails{
		Provenance: s.newProvenance(),
		Node: NodeIdentity{
			NodeID: n.NodeID, Nodename: n.Nodename, FQDN: n.FQDN, ClusterID: n.ClusterID, App: n.App, Env: n.NodeEnv,
			Status: n.Status, Role: n.Role, Type: n.Type, Frozen: collectorBool(n.NodeFrozen), FrozenAt: n.NodeFrozenAt,
			SnoozeTill: n.SnoozeTill,
		},
		Agent: NodeAgent{Version: n.Version, LastComm: n.LastComm, Updated: n.Updated},
		OS: NodeOS{
			Name: n.OSName, Vendor: n.OSVendor, Release: n.OSRelease, Kernel: n.OSKernel, Arch: n.OSArch,
			Timezone: n.TZ, LastBoot: n.LastBoot,
		},
		Hardware: NodeHardware{
			Manufacturer: n.Manufacturer, Model: n.Model, Serial: n.Serial, CPUModel: n.CPUModel, CPUVendor: n.CPUVendor,
			CPUCores: n.CPUCores, CPUThreads: n.CPUThreads, CPUDies: n.CPUDies, CPUFreq: n.CPUFreq, MemMB: n.MemBytes,
			MemBanks: n.MemBanks, MemSlots: n.MemSlots, BIOSVersion: n.BIOSVersion, SPVersion: n.SPVersion,
		},
		Teams: NodeTeams{Responsible: n.TeamResponsible, Integration: n.TeamInteg, Support: n.TeamSupport},
		Location: NodeLocation{
			Country: n.LocCountry, City: n.LocCity, Address: n.LocAddr, Zip: n.LocZip, Building: n.LocBuilding,
			Floor: n.LocFloor, Room: n.LocRoom, Rack: n.LocRack,
		},
		Lifecycle: NodeLifecycle{
			WarrantyEnd: n.WarrantyEnd, MaintenanceEnd: n.MaintenanceEnd, HardwareObsWarnDate: n.HWObsWarnDate,
			HardwareObsAlertDate: n.HWObsAlertDate, OSObsWarnDate: n.OSObsWarnDate, OSObsAlertDate: n.OSObsAlertDate,
		},
	}, nil
}

// lookupNode reads the props of one node by its exact name or node ID, and
// refuses a name several nodes hold.
func (s *Service) lookupNode(ctx context.Context, node string, props []string) (collectorNode, error) {
	if node == "" || strings.ContainsAny(node, "/ ") {
		return collectorNode{}, fmt.Errorf("node must be one exact node name or node ID")
	}
	if err := validateFilterValue("node", node); err != nil {
		return collectorNode{}, err
	}
	query := url.Values{}
	query.Set("props", strings.Join(props, ","))
	query.Set("meta", "false")
	var response collectorList[collectorNode]
	if err := s.client.GetJSON(ctx, "/nodes/"+url.PathEscape(node), query, &response); err != nil {
		return collectorNode{}, fmt.Errorf("get node %s: %w%s", node, err, notFoundHint(err))
	}
	switch len(response.Data) {
	case 0:
		return collectorNode{}, fmt.Errorf("get node %s: not found in the collector%s", node, findObjectsHint)
	case 1:
		return response.Data[0], nil
	default:
		ids := make([]string, 0, len(response.Data))
		for _, item := range response.Data {
			ids = append(ids, item.NodeID+" (cluster "+item.ClusterID+")")
		}
		return collectorNode{}, fmt.Errorf("get node %s: the name matches %d nodes, pass one node ID: %s", node, len(ids), strings.Join(ids, ", "))
	}
}
