package core

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// NodeRulesetOptions are a change of the rulesets attached to nodes.
type NodeRulesetOptions struct {
	Ruleset string
	NodeSelection
}

// NodeModulesetOptions are a change of the modulesets attached to nodes.
type NodeModulesetOptions struct {
	Moduleset string
	NodeSelection
}

type NodeRulesetChange struct {
	Provenance Provenance                 `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Ruleset    ComplianceRulesetReference `json:"ruleset" jsonschema:"the ruleset"`
	NodeChangeSummary
}

type NodeModulesetChange struct {
	Provenance Provenance                   `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Moduleset  ComplianceModulesetReference `json:"moduleset" jsonschema:"the moduleset"`
	NodeChangeSummary
}

type ComplianceRulesetReference struct {
	ID   int    `json:"id" jsonschema:"the collector ruleset ID"`
	Name string `json:"name" jsonschema:"the ruleset name"`
	Type string `json:"type" jsonschema:"explicit, attached to nodes and services, or contextual, applying through its filterset"`
}

type ComplianceModulesetReference struct {
	ID   int    `json:"id" jsonschema:"the collector moduleset ID"`
	Name string `json:"name" jsonschema:"the moduleset name"`
}

// AttachNodeRuleset attaches an explicit ruleset to the selected nodes. The
// collector also takes a contextual ruleset published to Everybody, which
// already applies through its filterset: the MCP refuses it.
func (s *Service) AttachNodeRuleset(ctx context.Context, options NodeRulesetOptions) (NodeRulesetChange, error) {
	return s.changeNodeRuleset(ctx, options, true)
}

// DetachNodeRuleset detaches a ruleset from the selected nodes.
func (s *Service) DetachNodeRuleset(ctx context.Context, options NodeRulesetOptions) (NodeRulesetChange, error) {
	return s.changeNodeRuleset(ctx, options, false)
}

// AttachNodeModuleset attaches a moduleset to the selected nodes.
func (s *Service) AttachNodeModuleset(ctx context.Context, options NodeModulesetOptions) (NodeModulesetChange, error) {
	return s.changeNodeModuleset(ctx, options, true)
}

// DetachNodeModuleset detaches a moduleset from the selected nodes.
func (s *Service) DetachNodeModuleset(ctx context.Context, options NodeModulesetOptions) (NodeModulesetChange, error) {
	return s.changeNodeModuleset(ctx, options, false)
}

func (s *Service) changeNodeRuleset(ctx context.Context, options NodeRulesetOptions, attach bool) (NodeRulesetChange, error) {
	if err := checkComplianceName("ruleset", options.Ruleset); err != nil {
		return NodeRulesetChange{}, err
	}
	filters, err := checkSelection(options.NodeSelection)
	if err != nil {
		return NodeRulesetChange{}, err
	}
	ruleset, err := s.lookupRuleset(ctx, options.Ruleset)
	if err != nil {
		return NodeRulesetChange{}, err
	}
	if attach && ruleset.Type != "explicit" {
		return NodeRulesetChange{}, fmt.Errorf("ruleset %s is %s: it applies to the nodes its filterset selects, and is not attached; nothing was changed; get_compliance_ruleset gives its filterset", ruleset.Name, ruleset.Type)
	}
	path := "/compliance/rulesets/" + strconv.Itoa(ruleset.ID)
	summary, err := s.changeSelectedNodes(ctx, options.NodeSelection, filters, func(ctx context.Context, node collectorNode) NodeChangeOutcome {
		return s.changeOneNodeCompliance(ctx, node, path, attach)
	})
	if err != nil {
		return NodeRulesetChange{}, err
	}
	return NodeRulesetChange{Provenance: s.newProvenance(), Ruleset: ruleset, NodeChangeSummary: summary}, nil
}

func (s *Service) changeNodeModuleset(ctx context.Context, options NodeModulesetOptions, attach bool) (NodeModulesetChange, error) {
	if err := checkComplianceName("moduleset", options.Moduleset); err != nil {
		return NodeModulesetChange{}, err
	}
	filters, err := checkSelection(options.NodeSelection)
	if err != nil {
		return NodeModulesetChange{}, err
	}
	moduleset, err := s.lookupModuleset(ctx, options.Moduleset)
	if err != nil {
		return NodeModulesetChange{}, err
	}
	path := "/compliance/modulesets/" + strconv.Itoa(moduleset.ID)
	summary, err := s.changeSelectedNodes(ctx, options.NodeSelection, filters, func(ctx context.Context, node collectorNode) NodeChangeOutcome {
		return s.changeOneNodeCompliance(ctx, node, path, attach)
	})
	if err != nil {
		return NodeModulesetChange{}, err
	}
	return NodeModulesetChange{Provenance: s.newProvenance(), Moduleset: moduleset, NodeChangeSummary: summary}, nil
}

// changeOneNodeCompliance attaches or detaches a ruleset or a moduleset,
// whose path is item, on one node.
func (s *Service) changeOneNodeCompliance(ctx context.Context, node collectorNode, item string, attach bool) NodeChangeOutcome {
	outcome := NodeChangeOutcome{NodeID: node.NodeID, Nodename: node.Nodename}
	outcome.Outcome, outcome.Reason = s.changeCompliance(ctx, "/nodes/"+url.PathEscape(node.NodeID)+item, attach)
	return outcome
}

// changeCompliance attaches or detaches the ruleset or moduleset of path,
// on a node or a service, and gives the outcome and the reason of a
// refusal. The collector refuses with 409 an attachment already there and a
// detachment of what is not attached: they change nothing.
func (s *Service) changeCompliance(ctx context.Context, path string, attach bool) (string, string) {
	var response struct {
		Info string `json:"info"`
	}
	var err error
	if attach {
		err = s.client.PostJSON(ctx, path, nil, &response)
	} else {
		err = s.client.DeleteJSON(ctx, path, &response)
	}
	text := ""
	if err != nil {
		text = strings.ToLower(err.Error())
	}
	switch {
	case err == nil && attach:
		return ChangeAttached, ""
	case err == nil:
		return ChangeDetached, ""
	case attach && strings.Contains(text, "already attached"):
		return ChangeAlreadyAttached, ""
	case !attach && strings.Contains(text, "not attached"):
		return ChangeAlreadyDetached, ""
	}
	return ChangeRefused, err.Error()
}

// lookupRuleset reads one ruleset the caller sees, by its name or ID.
func (s *Service) lookupRuleset(ctx context.Context, ruleset string) (ComplianceRulesetReference, error) {
	var found collectorList[collectorRuleset]
	err := s.client.GetJSON(ctx, "/compliance/rulesets/"+url.PathEscape(ruleset), url.Values{"props": {"id,ruleset_name,ruleset_type"}}, &found)
	if err != nil && !isNotFound(err) {
		return ComplianceRulesetReference{}, fmt.Errorf("get ruleset %s: %w", ruleset, err)
	}
	if err != nil || len(found.Data) != 1 {
		return ComplianceRulesetReference{}, fmt.Errorf("get ruleset %s: not found in the collector, or not published to the caller; list the rulesets with list_compliance_rulesets", ruleset)
	}
	return ComplianceRulesetReference{ID: found.Data[0].ID, Name: found.Data[0].Name, Type: found.Data[0].Type}, nil
}

// lookupModuleset reads one moduleset the caller sees, by its name or ID.
func (s *Service) lookupModuleset(ctx context.Context, moduleset string) (ComplianceModulesetReference, error) {
	var found collectorList[collectorModuleset]
	err := s.client.GetJSON(ctx, "/compliance/modulesets/"+url.PathEscape(moduleset), url.Values{"props": {"id,modset_name"}}, &found)
	if err != nil && !isNotFound(err) {
		return ComplianceModulesetReference{}, fmt.Errorf("get moduleset %s: %w", moduleset, err)
	}
	if err != nil || len(found.Data) != 1 {
		return ComplianceModulesetReference{}, fmt.Errorf("get moduleset %s: not found in the collector, or not published to the caller; list the modulesets with list_compliance_modulesets", moduleset)
	}
	return ComplianceModulesetReference{ID: found.Data[0].ID, Name: found.Data[0].Name}, nil
}

func checkComplianceName(kind, name string) error {
	if name == "" || strings.Contains(name, "/") {
		return fmt.Errorf("%s must be one exact %s name or %s ID", kind, kind, kind)
	}
	return validateFilterValue(kind, name)
}
