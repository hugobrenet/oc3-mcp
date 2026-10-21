package core

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

type CheckThresholdChange struct {
	Provenance Provenance `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Outcome    string     `json:"outcome" jsonschema:"set, removed, or unchanged when the check had no settings to remove"`
	Check      *Check     `json:"read_back,omitempty" jsonschema:"the check read back after the change, as list_node_checks gives it: its thresholds and their source, and whether it is out of bounds now"`
	Warning    string     `json:"warning,omitempty" jsonschema:"what went otherwise than asked, the change being made: the check not read back, or the collector not applying the thresholds yet; tell the user"`
}

// collectorRefusal is a collector error with its HTTP status and message.
type collectorRefusal interface {
	HTTPStatus() int
	Message() string
}

// refusal gives the HTTP status and message of a collector error, 0 when it
// is none.
func refusal(err error) (int, string) {
	var refused collectorRefusal
	if errors.As(err, &refused) {
		return refused.HTTPStatus(), refused.Message()
	}
	return 0, ""
}

// SetCheckThreshold sets the thresholds of one check, which take precedence
// over the contextual and the default ones. A threshold not given keeps the
// value the check has now, as the collector takes it.
func (s *Service) SetCheckThreshold(ctx context.Context, options CheckKeyOptions, low, high *int64) (CheckThresholdChange, error) {
	key, err := s.resolveCheckKey(ctx, options)
	if err != nil {
		return CheckThresholdChange{}, err
	}
	body := key.body()
	if low != nil {
		body["chk_low"] = *low
	}
	if high != nil {
		body["chk_high"] = *high
	}
	result := CheckThresholdChange{Outcome: ChangeSet}
	var response struct {
		Info string `json:"info"`
	}
	if err := s.client.PostJSON(ctx, "/checks/settings", body, &response); err != nil {
		if result.Warning, err = s.thresholdRefusal(ctx, key, err); err != nil {
			return CheckThresholdChange{}, fmt.Errorf("set the thresholds of check %s:%s of node %s: %w", key.chkType, key.instance, key.node.Nodename, err)
		}
	}
	return s.thresholdWritten(ctx, key, result), nil
}

// UnsetCheckThreshold removes the settings of one check: it takes the
// contextual or the default thresholds again.
func (s *Service) UnsetCheckThreshold(ctx context.Context, options CheckKeyOptions) (CheckThresholdChange, error) {
	key, err := s.resolveCheckKey(ctx, options)
	if err != nil {
		return CheckThresholdChange{}, err
	}
	result := CheckThresholdChange{Outcome: ChangeRemoved}
	var response struct {
		Info string `json:"info"`
	}
	if err := s.client.DeleteJSONBody(ctx, "/checks/settings", key.body(), &response); err != nil {
		// The collector answers 404 to a check without settings, as
		// DeleteChecksSettings of server/handlers/checks.go words it.
		if status, message := refusal(err); status == http.StatusNotFound && strings.HasPrefix(message, "no thresholds set") {
			result.Outcome = ChangeUnchanged
		} else if result.Warning, err = s.thresholdRefusal(ctx, key, err); err != nil {
			return CheckThresholdChange{}, fmt.Errorf("remove the thresholds of check %s:%s of node %s: %w", key.chkType, key.instance, key.node.Nodename, err)
		}
	}
	return s.thresholdWritten(ctx, key, result), nil
}

// body is the key of a check as the collector takes it: the service ID
// only for a check of a service.
func (key checkKey) body() map[string]any {
	body := map[string]any{"node_id": key.node.NodeID, "chk_type": key.chkType, "chk_instance": key.instance}
	if key.svcID != "" {
		body["svc_id"] = key.svcID
	}
	return body
}

// thresholdRefusal reads an error of a change of thresholds: a check the
// collector does not hold is told with the instances of its type on the
// node; the 500 of a change saved but not applied yet is a warning, the
// change being made.
func (s *Service) thresholdRefusal(ctx context.Context, key checkKey, err error) (string, error) {
	// The messages of the collector, server/handlers/checks.go of oc3: a
	// message reworded there is returned as the error it is.
	status, message := refusal(err)
	switch {
	case status == http.StatusNotFound && strings.HasPrefix(message, "check instance not found"):
		return "", s.unknownCheck(ctx, key)
	case status == http.StatusInternalServerError && strings.HasPrefix(message, "thresholds saved"):
		return "the collector saved the change, but did not apply it to the checks yet: " + message, nil
	}
	return "", err
}

// thresholdWritten reads the check back after a change, which the collector
// applies to the checks of the node before it answers.
func (s *Service) thresholdWritten(ctx context.Context, key checkKey, result CheckThresholdChange) CheckThresholdChange {
	check, err := s.readCheck(ctx, key)
	warning := ""
	switch {
	case err != nil:
		warning = fmt.Sprintf("the check could not be read back: %v; get_check_threshold reads it", err)
	case check == nil:
		warning = "the check could not be read back: the collector holds it no more"
	default:
		result.Check = check
	}
	if warning != "" {
		result.Warning = strings.TrimPrefix(result.Warning+"; "+warning, "; ")
	}
	result.Provenance = s.newProvenance()
	return result
}
