package core

import (
	"context"
	"fmt"
	"net/url"
)

type ServiceTagOptions struct {
	Tag string
	ServiceSelection
	// AttachData is the data of the attachment, such as the fields of a
	// form, when attaching.
	AttachData *string
}

type ServiceTagChange struct {
	Provenance Provenance   `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Tag        TagReference `json:"tag" jsonschema:"the tag"`
	ServiceChangeSummary
}

// AttachServiceTag attaches a tag to the selected services, one request per
// service: the collector has no bulk route, and a change is not atomic.
func (s *Service) AttachServiceTag(ctx context.Context, options ServiceTagOptions) (ServiceTagChange, error) {
	if options.AttachData != nil {
		if err := validateTagAttachData(*options.AttachData); err != nil {
			return ServiceTagChange{}, err
		}
	}
	return s.changeServiceTag(ctx, options, true)
}

// DetachServiceTag detaches a tag from the selected services.
func (s *Service) DetachServiceTag(ctx context.Context, options ServiceTagOptions) (ServiceTagChange, error) {
	if options.AttachData != nil {
		return ServiceTagChange{}, fmt.Errorf("attach_data is for an attachment only")
	}
	return s.changeServiceTag(ctx, options, false)
}

func (s *Service) changeServiceTag(ctx context.Context, options ServiceTagOptions, attach bool) (ServiceTagChange, error) {
	if options.Tag == "" {
		return ServiceTagChange{}, fmt.Errorf("tag must be one exact tag name or tag ID")
	}
	if err := checkServiceSelection(options.ServiceSelection); err != nil {
		return ServiceTagChange{}, err
	}
	tag, err := s.lookupTag(ctx, options.Tag)
	if err != nil {
		return ServiceTagChange{}, err
	}
	summary, err := s.changeSelectedServices(ctx, options.ServiceSelection, func(ctx context.Context, service selectedService) ServiceChangeOutcome {
		outcome := ServiceChangeOutcome{SvcID: service.SvcID, Svcname: service.Svcname, ClusterID: service.ClusterID}
		path := "/tags/" + url.PathEscape(tag.TagID) + "/services/" + url.PathEscape(service.SvcID)
		var response struct {
			Info string `json:"info"`
		}
		var err error
		if attach {
			body := map[string]any{}
			if options.AttachData != nil {
				body["tag_attach_data"] = *options.AttachData
			}
			err = s.client.PostJSON(ctx, path, body, &response)
		} else {
			err = s.client.DeleteJSON(ctx, path, nil, &response)
		}
		outcome.Outcome, outcome.Reason = tagOutcome(err, response.Info, attach)
		return outcome
	})
	if err != nil {
		return ServiceTagChange{}, err
	}
	return ServiceTagChange{Provenance: s.newProvenance(), Tag: tag, ServiceChangeSummary: summary}, nil
}
