package core

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
)

// ServiceRulesetOptions are a change of the rulesets attached to services.
type ServiceRulesetOptions struct {
	Ruleset string
	ServiceSelection
}

// ServiceModulesetOptions are a change of the modulesets attached to
// services.
type ServiceModulesetOptions struct {
	Moduleset string
	ServiceSelection
}

type ServiceRulesetChange struct {
	Provenance Provenance                 `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Ruleset    ComplianceRulesetReference `json:"ruleset" jsonschema:"the ruleset"`
	ServiceChangeSummary
}

type ServiceModulesetChange struct {
	Provenance Provenance                   `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Moduleset  ComplianceModulesetReference `json:"moduleset" jsonschema:"the moduleset"`
	ServiceChangeSummary
}

// AttachServiceRuleset attaches an explicit ruleset to the selected
// services. The collector also takes a contextual ruleset published to
// Everybody, which already applies through its filterset: the MCP refuses
// it.
//
// The changes are those of the services themselves: the collector keeps
// apart the attachments to their encapsulated services, which the MCP
// leaves as they are.
func (s *Service) AttachServiceRuleset(ctx context.Context, options ServiceRulesetOptions) (ServiceRulesetChange, error) {
	return s.changeServiceRuleset(ctx, options, true)
}

// DetachServiceRuleset detaches a ruleset from the selected services.
func (s *Service) DetachServiceRuleset(ctx context.Context, options ServiceRulesetOptions) (ServiceRulesetChange, error) {
	return s.changeServiceRuleset(ctx, options, false)
}

// AttachServiceModuleset attaches a moduleset to the selected services.
func (s *Service) AttachServiceModuleset(ctx context.Context, options ServiceModulesetOptions) (ServiceModulesetChange, error) {
	return s.changeServiceModuleset(ctx, options, true)
}

// DetachServiceModuleset detaches a moduleset from the selected services.
func (s *Service) DetachServiceModuleset(ctx context.Context, options ServiceModulesetOptions) (ServiceModulesetChange, error) {
	return s.changeServiceModuleset(ctx, options, false)
}

func (s *Service) changeServiceRuleset(ctx context.Context, options ServiceRulesetOptions, attach bool) (ServiceRulesetChange, error) {
	if err := checkComplianceName("ruleset", options.Ruleset); err != nil {
		return ServiceRulesetChange{}, err
	}
	if err := checkServiceSelection(options.ServiceSelection); err != nil {
		return ServiceRulesetChange{}, err
	}
	ruleset, err := s.lookupRuleset(ctx, options.Ruleset)
	if err != nil {
		return ServiceRulesetChange{}, err
	}
	if attach && ruleset.Type != "explicit" {
		return ServiceRulesetChange{}, fmt.Errorf("ruleset %s is %s: it applies to the services its filterset selects, and is not attached; nothing was changed; get_compliance_ruleset gives its filterset", ruleset.Name, ruleset.Type)
	}
	summary, err := s.changeSelectedServices(ctx, options.ServiceSelection, s.changeOneServiceCompliance("/compliance/rulesets/"+strconv.Itoa(ruleset.ID), attach))
	if err != nil {
		return ServiceRulesetChange{}, err
	}
	return ServiceRulesetChange{Provenance: s.newProvenance(), Ruleset: ruleset, ServiceChangeSummary: summary}, nil
}

func (s *Service) changeServiceModuleset(ctx context.Context, options ServiceModulesetOptions, attach bool) (ServiceModulesetChange, error) {
	if err := checkComplianceName("moduleset", options.Moduleset); err != nil {
		return ServiceModulesetChange{}, err
	}
	if err := checkServiceSelection(options.ServiceSelection); err != nil {
		return ServiceModulesetChange{}, err
	}
	moduleset, err := s.lookupModuleset(ctx, options.Moduleset)
	if err != nil {
		return ServiceModulesetChange{}, err
	}
	summary, err := s.changeSelectedServices(ctx, options.ServiceSelection, s.changeOneServiceCompliance("/compliance/modulesets/"+strconv.Itoa(moduleset.ID), attach))
	if err != nil {
		return ServiceModulesetChange{}, err
	}
	return ServiceModulesetChange{Provenance: s.newProvenance(), Moduleset: moduleset, ServiceChangeSummary: summary}, nil
}

// changeOneServiceCompliance attaches or detaches the ruleset or moduleset
// whose path is item on one service, by its ID: the collector resolves a
// name to the first service holding it.
func (s *Service) changeOneServiceCompliance(item string, attach bool) func(context.Context, selectedService) ServiceChangeOutcome {
	return func(ctx context.Context, service selectedService) ServiceChangeOutcome {
		outcome := ServiceChangeOutcome{SvcID: service.SvcID, Svcname: service.Svcname, ClusterID: service.ClusterID}
		outcome.Outcome, outcome.Reason = s.changeCompliance(ctx, "/services/"+url.PathEscape(service.SvcID)+item, attach)
		return outcome
	}
}
