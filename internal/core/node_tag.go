package core

import (
	"context"
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
	info := strings.ToLower(response.Info)
	switch {
	case err != nil:
		outcome.Outcome, outcome.Reason = ChangeRefused, err.Error()
	case attach && strings.Contains(info, "already attached"):
		outcome.Outcome = ChangeAlreadyAttached
	case attach:
		outcome.Outcome = ChangeAttached
	case strings.Contains(info, "already detached"):
		outcome.Outcome = ChangeAlreadyDetached
	default:
		outcome.Outcome = ChangeDetached
	}
	return outcome
}

func validateTagAttachData(data string) error {
	if len(data) > 4096 || !utf8.ValidString(data) {
		return fmt.Errorf("attach_data must be at most 4096 bytes of text")
	}
	return nil
}
