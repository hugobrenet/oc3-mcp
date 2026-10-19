package core

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"unicode/utf8"
)

// NodeTagOptions are a tag change: the tag, the selected nodes and, when
// attaching, the data of the attachment, such as the fields of a form.
type NodeTagOptions struct {
	Tag string
	NodeSelection
	AttachData *string
}

type NodeTagChange struct {
	Provenance Provenance   `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Tag        TagReference `json:"tag" jsonschema:"the tag"`
	NodeChangeSummary
}

// AttachNodeTag attaches a tag to the selected nodes, one request per node:
// the collector has no bulk route, and a change is not atomic.
func (s *Service) AttachNodeTag(ctx context.Context, options NodeTagOptions) (NodeTagChange, error) {
	if options.AttachData != nil {
		if err := validateTagAttachData(*options.AttachData); err != nil {
			return NodeTagChange{}, err
		}
	}
	return s.changeNodeTag(ctx, options, true)
}

// DetachNodeTag detaches a tag from the selected nodes, one request per node.
func (s *Service) DetachNodeTag(ctx context.Context, options NodeTagOptions) (NodeTagChange, error) {
	if options.AttachData != nil {
		return NodeTagChange{}, fmt.Errorf("attach_data is for an attachment only")
	}
	return s.changeNodeTag(ctx, options, false)
}

func (s *Service) changeNodeTag(ctx context.Context, options NodeTagOptions, attach bool) (NodeTagChange, error) {
	if options.Tag == "" {
		return NodeTagChange{}, fmt.Errorf("tag must be one exact tag name or tag ID")
	}
	filters, err := checkSelection(options.NodeSelection)
	if err != nil {
		return NodeTagChange{}, err
	}
	tag, err := s.lookupTag(ctx, options.Tag)
	if err != nil {
		return NodeTagChange{}, err
	}
	summary, err := s.changeSelectedNodes(ctx, options.NodeSelection, filters, func(ctx context.Context, node collectorNode) NodeChangeOutcome {
		return s.changeOneNodeTag(ctx, tag, node, attach, options.AttachData)
	})
	if err != nil {
		return NodeTagChange{}, err
	}
	return NodeTagChange{Provenance: s.newProvenance(), Tag: tag, NodeChangeSummary: summary}, nil
}

// changeOneNodeTag attaches or detaches the tag on one node. The collector
// answers an attachment or a detachment that changes nothing with 200 and
// an info saying so.
func (s *Service) changeOneNodeTag(ctx context.Context, tag TagReference, node collectorNode, attach bool, attachData *string) NodeChangeOutcome {
	outcome := NodeChangeOutcome{NodeID: node.NodeID, Nodename: node.Nodename}
	path := "/tags/" + url.PathEscape(tag.TagID) + "/nodes/" + url.PathEscape(node.NodeID)
	var response struct {
		Info string `json:"info"`
	}
	var err error
	if attach {
		body := map[string]any{}
		if attachData != nil {
			body["tag_attach_data"] = *attachData
		}
		err = s.client.PostJSON(ctx, path, body, &response)
	} else {
		err = s.client.DeleteJSON(ctx, path, &response)
	}
	outcome.Outcome, outcome.Reason = tagOutcome(err, response.Info, attach)
	return outcome
}

// tagOutcome reads the outcome of an attachment or a detachment of a tag,
// on a node or a service: the collector answers one that changes nothing
// with 200 and an info saying so.
func tagOutcome(err error, info string, attach bool) (outcome, reason string) {
	info = strings.ToLower(info)
	switch {
	case err != nil:
		return ChangeRefused, err.Error()
	case attach && strings.Contains(info, "already attached"):
		return ChangeAlreadyAttached, ""
	case attach:
		return ChangeAttached, ""
	case strings.Contains(info, "already detached"):
		return ChangeAlreadyDetached, ""
	}
	return ChangeDetached, ""
}

// validateTagAttachData checks the data of an attachment, free JSON text:
// an empty one would clear the data of an attachment already there.
func validateTagAttachData(data string) error {
	if data == "" || len(data) > 4096 || !utf8.ValidString(data) || !json.Valid([]byte(data)) {
		return fmt.Errorf("attach_data must be 1 to 4096 bytes of JSON text; leave it out to keep the data of an attachment already there")
	}
	return nil
}
