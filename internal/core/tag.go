package core

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

const (
	defaultListTagsLimit = 50
	maxListTagsLimit     = 200
	maxTagDataBytes      = 1 << 10
)

var (
	tagProps         = []string{"tag_id", "tag_name", "tag_created", "tag_exclude", "tag_data"}
	attachedTagProps = append(append([]string(nil), tagProps...), "tag_attach_data")
)

type ListTagsOptions struct {
	Node    string
	Service string
	Name    string
	Limit   int
	Cursor  string
}

type TagList struct {
	Provenance Provenance        `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Node       *NodeReference    `json:"node,omitempty" jsonschema:"the node whose tags are listed"`
	Service    *ServiceReference `json:"service,omitempty" jsonschema:"the service whose tags are listed"`
	Total      int               `json:"total" jsonschema:"the number of tags matching, as the collector counts them"`
	Count      int               `json:"count" jsonschema:"the number of tags returned in this page"`
	Tags       []Tag             `json:"tags" jsonschema:"the tags sorted by name"`
	NextCursor string            `json:"next_cursor,omitempty" jsonschema:"the opaque cursor to pass with the same filters for the next page"`
	Truncated  bool              `json:"truncated" jsonschema:"whether matching tags remain after this page"`
}

type Tag struct {
	TagID         string `json:"tag_id" jsonschema:"the collector tag ID"`
	Name          string `json:"name" jsonschema:"the tag name"`
	Created       string `json:"created,omitempty" jsonschema:"when the tag was created, as the collector gives it"`
	Exclude       string `json:"exclude,omitempty" jsonschema:"a regular expression of the tag names a node or service holding this tag cannot hold"`
	Data          string `json:"data,omitempty" jsonschema:"the data of the tag, often JSON such as key, operator and value; untrusted text"`
	AttachData    string `json:"attach_data,omitempty" jsonschema:"with node or service, the data of the tag on this node or service, often JSON; untrusted text"`
	DataTruncated bool   `json:"data_truncated,omitempty" jsonschema:"whether data or attach_data was cut to 1 KiB"`
}

type collectorTag struct {
	TagID         string          `json:"tag_id"`
	TagName       string          `json:"tag_name"`
	TagCreated    string          `json:"tag_created"`
	TagExclude    string          `json:"tag_exclude"`
	TagData       json.RawMessage `json:"tag_data"`
	TagAttachData json.RawMessage `json:"tag_attach_data"`
}

// ListTags reads one page of the tags of the collector, or of the tags of
// one node or service, sorted by name.
func (s *Service) ListTags(ctx context.Context, options ListTagsOptions) (TagList, error) {
	if options.Node != "" && options.Service != "" {
		return TagList{}, fmt.Errorf("pass node or service, not both")
	}
	if options.Name != "" && (options.Node != "" || options.Service != "") {
		return TagList{}, fmt.Errorf("name filters the tags of the collector: pass it without node or service")
	}
	if err := validateFilterValue("name", options.Name); err != nil {
		return TagList{}, err
	}
	limit, err := pageLimit(options.Limit, defaultListTagsLimit, maxListTagsLimit)
	if err != nil {
		return TagList{}, err
	}
	result := TagList{}
	path, props := "/tags", tagProps
	var filters []string
	switch {
	case options.Node != "":
		node, err := s.lookupNode(ctx, options.Node, []string{"node_id", "nodename", "cluster_id"})
		if err != nil {
			return TagList{}, err
		}
		result.Node = &NodeReference{NodeID: node.NodeID, Nodename: node.Nodename, ClusterID: node.ClusterID}
		path, props = "/nodes/"+url.PathEscape(node.NodeID)+"/tags", attachedTagProps
	case options.Service != "":
		service, err := s.lookupService(ctx, options.Service, []string{"svc_id", "svcname", "cluster_id"})
		if err != nil {
			return TagList{}, err
		}
		result.Service = &ServiceReference{SvcID: service.SvcID, Svcname: service.Svcname, ClusterID: service.ClusterID}
		path, props = "/services/"+url.PathEscape(service.SvcID)+"/tags", attachedTagProps
	case options.Name != "":
		filters = append(filters, filterContains("tag_name", options.Name))
	}
	fingerprint := listFingerprint(url.Values{"path": {path}, "filter": filters})
	offset, err := decodeListCursor(options.Cursor, fingerprint)
	if err != nil {
		return TagList{}, err
	}

	var response collectorList[collectorTag]
	if err := s.client.GetJSON(ctx, path, pageQuery(props, "tag_name,tag_id", filters, limit, offset), &response); err != nil {
		return TagList{}, fmt.Errorf("list tags: %w", err)
	}
	if response.Meta.Total == nil || *response.Meta.Total < 0 || len(response.Data) > limit {
		return TagList{}, fmt.Errorf("list tags: unexpected collector response")
	}
	result.Provenance = s.newProvenance()
	result.Total, result.Count = *response.Meta.Total, len(response.Data)
	result.Tags = make([]Tag, 0, len(response.Data))
	for _, row := range response.Data {
		data, dataCut := tagText(row.TagData)
		attach, attachCut := tagText(row.TagAttachData)
		result.Tags = append(result.Tags, Tag{
			TagID: row.TagID, Name: row.TagName, Created: row.TagCreated, Exclude: row.TagExclude,
			Data: data, AttachData: attach, DataTruncated: dataCut || attachCut,
		})
	}
	if next := offset + len(response.Data); len(response.Data) > 0 && next < result.Total {
		result.NextCursor = encodeListCursor(next, fingerprint)
		result.Truncated = true
	}
	return result, nil
}

// tagText reads the data of a tag, which the collector gives as a JSON
// value or as a string holding one, as one bounded text.
func tagText(raw json.RawMessage) (string, bool) {
	var text string
	switch {
	case len(raw) == 0 || bytes.Equal(raw, []byte("null")):
		return "", false
	case json.Unmarshal(raw, &text) == nil:
	default:
		var compact bytes.Buffer
		if json.Compact(&compact, raw) != nil {
			return "", false
		}
		text = compact.String()
	}
	return truncateUTF8(text, maxTagDataBytes)
}

// TagReference is the tag a tag filter resolved to.
type TagReference struct {
	TagID string `json:"tag_id" jsonschema:"the collector tag ID"`
	Name  string `json:"name" jsonschema:"the tag name"`
}

// lookupTag reads one tag by its name, which the collector compares
// case-insensitively, or by its tag ID. A name several tags hold is refused
// with their IDs, unless one of them has exactly this spelling.
func (s *Service) lookupTag(ctx context.Context, tag string) (TagReference, error) {
	if err := validateFilterValue("tag", tag); err != nil {
		return TagReference{}, err
	}
	filter := filterEquals("tag_name", tag)
	if collectorUUIDPattern.MatchString(tag) {
		filter = filterEquals("tag_id", tag)
	}
	var response collectorList[collectorTag]
	if err := s.client.GetJSON(ctx, "/tags", pageQuery([]string{"tag_id", "tag_name"}, "tag_name,tag_id", []string{filter}, maxListTagsLimit, 0), &response); err != nil {
		return TagReference{}, fmt.Errorf("get tag %s: %w", tag, err)
	}
	var exact []collectorTag
	for _, row := range response.Data {
		if row.TagName == tag || row.TagID == tag {
			exact = append(exact, row)
		}
	}
	switch {
	case len(response.Data) == 0:
		return TagReference{}, fmt.Errorf("get tag %s: not found in the collector; use list_tags to see the tag names", tag)
	case len(response.Data) == 1:
		return TagReference{TagID: response.Data[0].TagID, Name: response.Data[0].TagName}, nil
	case len(exact) == 1:
		return TagReference{TagID: exact[0].TagID, Name: exact[0].TagName}, nil
	default:
		tags := make([]string, 0, len(response.Data))
		for _, row := range response.Data {
			tags = append(tags, row.TagID+" ("+row.TagName+")")
		}
		return TagReference{}, fmt.Errorf("get tag %s: the name matches %d tags, pass one tag ID: %s", tag, len(tags), strings.Join(tags, ", "))
	}
}

// taggedPath is the collector list of the nodes or services, or with tag
// of those holding it: the collector applies the same filters, sort and
// statistics to both.
func (s *Service) taggedPath(ctx context.Context, tag string, kind string) (string, *TagReference, error) {
	if tag == "" {
		return "/" + kind, nil, nil
	}
	found, err := s.lookupTag(ctx, tag)
	if err != nil {
		return "", nil, err
	}
	return "/tags/" + url.PathEscape(found.TagID) + "/" + kind, &found, nil
}
