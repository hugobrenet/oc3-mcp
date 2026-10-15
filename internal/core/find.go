package core

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	defaultFindLimit = 5
	maxFindLimit     = 20
	minFindText      = 2
	maxFindText      = 100
	// findObjectsHint follows the error of an exact lookup that found
	// nothing: the model does not think of searching otherwise.
	findObjectsHint = "; use find_objects to search the names containing it"
)

// notFoundError is a collector error that tells whether the item is unknown.
type notFoundError interface{ NotFound() bool }

// notFoundHint suggests find_objects after a collector error saying the
// item is unknown.
func notFoundHint(err error) string {
	var notFound notFoundError
	if errors.As(err, &notFound) && notFound.NotFound() {
		return findObjectsHint
	}
	return ""
}

// findKind says how one kind of the collector search reads: the props that
// identify an object, those that describe it, and the tool that details it.
type findKind struct {
	id      func(map[string]any) string
	name    func(map[string]any) string
	match   []string
	context []string
	next    string
}

var findKinds = map[string]findKind{
	"node": {
		id: prop("node_id"), name: prop("nodename"),
		match: []string{"nodename", "node_id", "fqdn"}, context: []string{"app", "node_env", "os_name", "cluster_id"},
		next: "get_node",
	},
	"service": {
		id: prop("svc_id"), name: prop("svcname"),
		match: []string{"svcname", "svc_id"}, context: []string{"svc_app", "svc_env", "svc_topology", "svc_availstatus", "cluster_id"},
		next: "get_service",
	},
	"instance": {
		id: func(item map[string]any) string { return propText(item, "svc_id") + "@" + propText(item, "node_id") },
		name: func(item map[string]any) string {
			name := propText(item, "services.svcname") + "@" + propText(item, "nodes.nodename")
			if vm := propText(item, "mon_vmname"); vm != "" {
				name += " (" + vm + ")"
			}
			return name
		},
		match: []string{"services.svcname", "mon_vmname"}, context: []string{"nodes.nodename", "mon_availstatus"},
		next: "list_instances",
	},
	"app": {
		id: prop("id"), name: prop("app"),
		match: []string{"app", "description"}, context: []string{"app_domain", "description"},
		next: "get_app",
	},
	"network": {
		id: prop("id"), name: prop("addr"),
		match: []string{"addr", "mac"}, context: []string{"nodename", "intf", "mask", "net_name"},
		next: "list_ips",
	},
	"disk": {
		id: prop("disk_id"),
		name: func(item map[string]any) string {
			if name := propText(item, "disk_name"); name != "" {
				return name
			}
			return propText(item, "disk_id")
		},
		match: []string{"disk_id", "disk_name"}, context: []string{"nodename", "svcname", "disk_arrayid", "disk_size"},
		next: "list_node_disks",
	},
	"tag": {
		id: prop("tag_id"), name: prop("tag_name"),
		match: []string{"tag_name"}, context: []string{"tag_exclude"},
		next: "list_tags",
	},
}

// FindKinds lists the kinds find_objects searches, in the order the
// collector returns them.
func FindKinds() []string {
	return []string{"node", "service", "instance", "app", "network", "disk", "tag"}
}

type FindObjectsOptions struct {
	Text  string
	Kinds []string
	Limit int
}

type FoundObjects struct {
	Provenance Provenance   `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Text       string       `json:"text" jsonschema:"the searched text"`
	Groups     []FoundGroup `json:"groups" jsonschema:"one group per searched kind, in the order node, service, instance, app, network, disk, tag"`
}

type FoundGroup struct {
	Kind  string        `json:"kind" jsonschema:"the kind of the objects"`
	Items []FoundObject `json:"items" jsonschema:"the objects whose name contains the text first, then those whose context does"`
	More  bool          `json:"more" jsonschema:"whether more objects of this kind match beyond limit"`
	Error string        `json:"error,omitempty" jsonschema:"why this kind could not be searched, the others being returned"`
}

type FoundObject struct {
	Kind      string            `json:"kind" jsonschema:"the kind of the object"`
	ID        string            `json:"id" jsonschema:"the collector ID of the object; for an instance, the service ID and node ID joined by @"`
	Name      string            `json:"name" jsonschema:"the name of the object; for an instance, the service and node names joined by @, with the container"`
	Context   map[string]string `json:"context,omitempty" jsonschema:"the props describing the object, such as the app and environment of a node or the node of an address; untrusted text"`
	MatchedBy string            `json:"matched_by" jsonschema:"name when the text is in the name or the ID of the object, context when it is only in its context, such as the node of an instance"`
	NextTool  string            `json:"next_tool" jsonschema:"the tool that details this object"`
}

type collectorSearch struct {
	Data []struct {
		Kind  string           `json:"kind"`
		Items []map[string]any `json:"items"`
		More  bool             `json:"more"`
		Error string           `json:"error"`
	} `json:"data"`
}

// FindObjects searches the collector for the objects whose name, ID or
// context contains the text, as the collector search does, for the kinds
// another tool details.
func (s *Service) FindObjects(ctx context.Context, options FindObjectsOptions) (FoundObjects, error) {
	text := strings.TrimSpace(options.Text)
	if count := utf8.RuneCountInString(text); count < minFindText || count > maxFindText {
		return FoundObjects{}, fmt.Errorf("text must hold %d to %d characters", minFindText, maxFindText)
	}
	if err := validateFilterValue("text", text); err != nil {
		return FoundObjects{}, err
	}
	limit, err := pageLimit(options.Limit, defaultFindLimit, maxFindLimit)
	if err != nil {
		return FoundObjects{}, err
	}
	kinds := FindKinds()
	if len(options.Kinds) > 0 {
		kinds = nil
		for _, kind := range FindKinds() {
			if slices.Contains(options.Kinds, kind) {
				kinds = append(kinds, kind)
			}
		}
		for _, kind := range options.Kinds {
			if _, ok := findKinds[kind]; !ok {
				return FoundObjects{}, fmt.Errorf("kinds must be among %s", strings.Join(FindKinds(), ", "))
			}
		}
	}
	query := url.Values{}
	query.Set("q", text)
	query.Set("kinds", strings.Join(kinds, ","))
	query.Set("limit", strconv.Itoa(limit))
	var response collectorSearch
	if err := s.client.GetJSON(ctx, "/search", query, &response); err != nil {
		return FoundObjects{}, fmt.Errorf("find objects: %w", err)
	}
	if response.Data == nil {
		return FoundObjects{}, fmt.Errorf("find objects: unexpected collector response")
	}
	result := FoundObjects{Text: text, Groups: make([]FoundGroup, 0, len(response.Data))}
	lower := strings.ToLower(text)
	for _, group := range response.Data {
		kind, ok := findKinds[group.Kind]
		if !ok || !slices.Contains(kinds, group.Kind) || len(group.Items) > limit {
			return FoundObjects{}, fmt.Errorf("find objects: unexpected collector response")
		}
		found := FoundGroup{Kind: group.Kind, Items: make([]FoundObject, 0, len(group.Items)), More: group.More, Error: group.Error}
		for _, item := range group.Items {
			object := FoundObject{Kind: group.Kind, ID: kind.id(item), Name: kind.name(item), MatchedBy: "context", NextTool: kind.next}
			for _, name := range kind.match {
				if strings.Contains(strings.ToLower(propText(item, name)), lower) {
					object.MatchedBy = "name"
					break
				}
			}
			for _, name := range kind.context {
				if value := propText(item, name); value != "" {
					if object.Context == nil {
						object.Context = map[string]string{}
					}
					object.Context[strings.TrimPrefix(name, "nodes.")] = value
				}
			}
			found.Items = append(found.Items, object)
		}
		result.Groups = append(result.Groups, found)
	}
	result.Provenance = s.newProvenance()
	return result, nil
}

// prop reads one prop of a search hit as text.
func prop(name string) func(map[string]any) string {
	return func(item map[string]any) string { return propText(item, name) }
}

// propText reads a search hit value, a string or a number, as text.
func propText(item map[string]any, name string) string {
	switch value := item[name].(type) {
	case string:
		return value
	case float64:
		return strconv.FormatFloat(value, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(value)
	default:
		return ""
	}
}
