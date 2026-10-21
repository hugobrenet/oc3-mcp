package core

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// Outcomes of a change of a tag.
const (
	TagCreated   = "created"
	TagChanged   = "changed"
	TagUnchanged = "unchanged"
	TagDeleted   = "deleted"
)

// TagFields are the fields of a tag a change sets, the others kept.
type TagFields struct {
	Name    *string
	Exclude *string
	Data    *string
}

type TagWrite struct {
	Provenance         Provenance `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Outcome            string     `json:"outcome" jsonschema:"created, changed, unchanged when the tag holds the values given already, or deleted"`
	Tag                Tag        `json:"tag" jsonschema:"the tag as the collector holds it after the change, as list_tags gives it; with delete_tag, the tag deleted"`
	NodeAttachments    *int       `json:"node_attachments_deleted,omitempty" jsonschema:"with delete_tag, the number of nodes the tag was detached from"`
	ServiceAttachments *int       `json:"service_attachments_deleted,omitempty" jsonschema:"with delete_tag, the number of services the tag was detached from"`
	Warning            string     `json:"warning,omitempty" jsonschema:"with delete_tag, when the collector tells the tag not deleted, its attachments being; tell the user"`
}

// deletedAttachments reads the numbers of attachments the collector tells
// it deleted with a tag, as DeleteTag of server/handlers/delete_tag.go of
// oc3 words them.
var deletedAttachments = regexp.MustCompile(`(\d+) node attachments deleted, (\d+) service attachments deleted`)

// CreateTag creates a tag: its name, the regular expression of the tag
// names it cannot be attached along with, and its data, sent as given.
func (s *Service) CreateTag(ctx context.Context, name string, exclude, data *string) (TagWrite, error) {
	body := map[string]any{"tag_name": name}
	if exclude != nil {
		body["tag_exclude"] = *exclude
	}
	if data != nil {
		body["tag_data"] = *data
	}
	var response collectorList[collectorTag]
	if err := s.client.PostJSON(ctx, "/tags", body, &response); err != nil {
		return TagWrite{}, fmt.Errorf("create tag %s: %w", name, err)
	}
	if len(response.Data) != 1 {
		return TagWrite{}, fmt.Errorf("create tag %s: unexpected collector response; list_tags tells whether it was created", name)
	}
	return TagWrite{Provenance: s.newProvenance(), Outcome: TagCreated, Tag: tagOf(response.Data[0])}, nil
}

// UpdateTag changes the fields given of a tag, the others kept. A change
// that leaves the tag as it was is told unchanged.
func (s *Service) UpdateTag(ctx context.Context, tag string, fields TagFields) (TagWrite, error) {
	before, err := s.readTag(ctx, tag)
	if err != nil {
		return TagWrite{}, err
	}
	body := map[string]any{}
	for key, value := range map[string]*string{"tag_name": fields.Name, "tag_exclude": fields.Exclude, "tag_data": fields.Data} {
		if value != nil {
			body[key] = *value
		}
	}
	var response collectorList[collectorTag]
	if err := s.client.PostJSON(ctx, "/tags/"+url.PathEscape(before.TagID), body, &response); err != nil {
		return TagWrite{}, fmt.Errorf("update tag %s: %w", before.TagName, err)
	}
	if len(response.Data) != 1 {
		return TagWrite{}, fmt.Errorf("update tag %s: unexpected collector response; list_tags reads it", before.TagName)
	}
	after := tagOf(response.Data[0])
	outcome := TagChanged
	if after == tagOf(before) {
		outcome = TagUnchanged
	}
	return TagWrite{Provenance: s.newProvenance(), Outcome: outcome, Tag: after}, nil
}

// DeleteTag deletes a tag, and detaches it from its nodes and services.
func (s *Service) DeleteTag(ctx context.Context, tag string) (TagWrite, error) {
	found, err := s.readTag(ctx, tag)
	if err != nil {
		return TagWrite{}, err
	}
	var response struct {
		Info string `json:"info"`
	}
	if err := s.client.DeleteJSON(ctx, "/tags/"+url.PathEscape(found.TagID), nil, &response); err != nil {
		return TagWrite{}, fmt.Errorf("delete tag %s: %w", found.TagName, err)
	}
	result := TagWrite{Outcome: TagDeleted, Tag: tagOf(found)}
	if match := deletedAttachments.FindStringSubmatch(response.Info); match != nil {
		nodes, _ := strconv.Atoi(match[1])
		services, _ := strconv.Atoi(match[2])
		result.NodeAttachments, result.ServiceAttachments = &nodes, &services
	}
	// The collector tells a tag it found no more, deleted meanwhile.
	if strings.Contains(response.Info, "' not deleted") {
		result.Outcome = TagUnchanged
		result.Warning = "the collector did not delete the tag, which another change may have deleted: " + response.Info
	}
	result.Provenance = s.newProvenance()
	return result, nil
}

// readTag reads one tag, by its name or its tag ID, with its fields.
func (s *Service) readTag(ctx context.Context, tag string) (collectorTag, error) {
	reference, err := s.lookupTag(ctx, tag)
	if err != nil {
		return collectorTag{}, err
	}
	var response collectorList[collectorTag]
	if err := s.client.GetJSON(ctx, "/tags/"+url.PathEscape(reference.TagID), url.Values{"props": {strings.Join(tagProps, ",")}, "meta": {"false"}}, &response); err != nil {
		return collectorTag{}, fmt.Errorf("get tag %s: %w", reference.Name, err)
	}
	if len(response.Data) != 1 {
		return collectorTag{}, fmt.Errorf("get tag %s: unexpected collector response", reference.Name)
	}
	return response.Data[0], nil
}

// tagOf gives a tag as list_tags gives it.
func tagOf(row collectorTag) Tag {
	data, dataCut := tagText(row.TagData)
	return Tag{TagID: row.TagID, Name: row.TagName, Created: row.TagCreated, Exclude: row.TagExclude, Data: data, DataTruncated: dataCut}
}
