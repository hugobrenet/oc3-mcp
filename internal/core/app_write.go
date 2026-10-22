package core

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// maxAppGroupChanges bounds the groups one call attaches to or detaches
// from an app.
const maxAppGroupChanges = 50

// Outcomes of a change of an app.
const (
	AppCreated   = "created"
	AppChanged   = "changed"
	AppUnchanged = "unchanged"
	AppDeleted   = "deleted"
)

// AppFields are the fields of an app a change sets, the others kept.
type AppFields struct {
	Name        *string
	Description *string
	Domain      *string
	TeamOps     *string
}

type AppWrite struct {
	Provenance Provenance `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Outcome    string     `json:"outcome" jsonschema:"created, changed, unchanged when the app holds the values given already, or deleted"`
	App        *App       `json:"read_back,omitempty" jsonschema:"the app read back after the change, as get_app gives it; with delete_app, as it was before the deletion"`
	Warning    string     `json:"warning,omitempty" jsonschema:"why the app could not be read back, the change being made; get_app reads it"`
}

type AppGroupOptions struct {
	App          string
	Responsibles []string
	Publications []string
}

type AppGroupChange struct {
	Provenance Provenance       `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	App        AppReference     `json:"app" jsonschema:"the app"`
	Changed    int              `json:"changed" jsonschema:"the number of groups attached or detached"`
	Unchanged  int              `json:"unchanged" jsonschema:"the number of groups already as asked"`
	Refused    int              `json:"refused" jsonschema:"the number of groups refused by the collector or not found"`
	Groups     []AppGroupResult `json:"groups" jsonschema:"the outcome on each group, responsibles first, in the order given"`
	// Responsibles are read back after a detachment of responsibles.
	Responsibles []string `json:"responsibles,omitempty" jsonschema:"with detach_app_groups of responsibles, the groups still responsible for the app, read back"`
	Warning      string   `json:"warning,omitempty" jsonschema:"with detach_app_groups, when no group is responsible for the app any more: only a Manager may then change it, or its nodes and services; tell the user"`
}

type AppReference struct {
	ID   int    `json:"id" jsonschema:"the collector app ID"`
	Name string `json:"name" jsonschema:"the app code"`
}

type AppGroupResult struct {
	Group   string `json:"group" jsonschema:"the group, as given"`
	Role    string `json:"role" jsonschema:"responsible: its members manage the nodes and services of the app; publication: its members see them"`
	Outcome string `json:"outcome" jsonschema:"attached, already_attached, detached, already_detached or refused"`
	Reason  string `json:"reason,omitempty" jsonschema:"with refused, why"`
}

// CreateApp creates an app code: the collector makes the default group of
// the caller responsible for it and publishes it to that group. The fields
// go as given, the collector checking none of them at the creation. The app
// is read back by the ID the collector answers with, as get_app gives it.
func (s *Service) CreateApp(ctx context.Context, name string, fields AppFields) (AppWrite, error) {
	body := fields.body()
	body["app"] = name
	var response collectorApp
	if err := s.client.PostJSON(ctx, "/apps", body, &response); err != nil {
		return AppWrite{}, fmt.Errorf("create app %s: %w", name, err)
	}
	key := name
	if response.ID > 0 {
		key = strconv.Itoa(response.ID)
	}
	return s.appWritten(ctx, AppCreated, key), nil
}

// UpdateApp changes the fields given of an app, the others kept. A new
// code renames the app on its nodes and services too.
func (s *Service) UpdateApp(ctx context.Context, app string, fields AppFields) (AppWrite, error) {
	before, err := s.GetApp(ctx, app)
	if err != nil {
		return AppWrite{}, err
	}
	var response any
	if err := s.client.PostJSON(ctx, "/apps/"+strconv.Itoa(before.ID), fields.body(), &response); err != nil {
		return AppWrite{}, fmt.Errorf("update app %s: %w", before.Name, err)
	}
	result := s.appWritten(ctx, AppChanged, strconv.Itoa(before.ID))
	if after := result.App; after != nil && after.Name == before.Name && after.Description == before.Description && after.Domain == before.Domain && after.TeamOps == before.TeamOps {
		result.Outcome = AppUnchanged
	}
	return result, nil
}

// DeleteApp deletes an app code, with its responsibles and publications;
// the collector refuses it while nodes or services carry it. It is read
// first, for the result to tell what it was.
func (s *Service) DeleteApp(ctx context.Context, app string) (AppWrite, error) {
	before, err := s.GetApp(ctx, app)
	if err != nil {
		return AppWrite{}, err
	}
	var response any
	if err := s.client.DeleteJSON(ctx, "/apps/"+strconv.Itoa(before.ID), nil, &response); err != nil {
		return AppWrite{}, fmt.Errorf("delete app %s: %w", before.Name, err)
	}
	return AppWrite{Provenance: s.newProvenance(), Outcome: AppDeleted, App: &before}, nil
}

func (fields AppFields) body() map[string]any {
	body := map[string]any{}
	for key, value := range map[string]*string{"app": fields.Name, "description": fields.Description, "app_domain": fields.Domain, "app_team_ops": fields.TeamOps} {
		if value != nil {
			body[key] = *value
		}
	}
	return body
}

// appWritten reads an app back after a change, which is made: a read back
// that fails is told, not returned as an error.
func (s *Service) appWritten(ctx context.Context, outcome, app string) AppWrite {
	result := AppWrite{Outcome: outcome}
	details, err := s.GetApp(ctx, app)
	if err != nil {
		result.Warning = fmt.Sprintf("the app could not be read back: %v", err)
	} else {
		result.App = &details
	}
	result.Provenance = s.newProvenance()
	return result
}

// AttachAppGroups makes groups responsible for an app and publishes it to
// groups, one request per group, as the collector takes them.
func (s *Service) AttachAppGroups(ctx context.Context, options AppGroupOptions) (AppGroupChange, error) {
	return s.changeAppGroups(ctx, options, true)
}

// DetachAppGroups withdraws groups from the responsibles of an app and
// unpublishes it from groups, one request per group.
func (s *Service) DetachAppGroups(ctx context.Context, options AppGroupOptions) (AppGroupChange, error) {
	return s.changeAppGroups(ctx, options, false)
}

func (s *Service) changeAppGroups(ctx context.Context, options AppGroupOptions, attach bool) (AppGroupChange, error) {
	total := len(options.Responsibles) + len(options.Publications)
	if total == 0 {
		return AppGroupChange{}, fmt.Errorf("pass responsibles or publications, the groups to change")
	}
	if total > maxAppGroupChanges {
		return AppGroupChange{}, fmt.Errorf("pass at most %d groups", maxAppGroupChanges)
	}
	app, err := s.GetApp(ctx, options.App)
	if err != nil {
		return AppGroupChange{}, err
	}
	result := AppGroupChange{App: AppReference{ID: app.ID, Name: app.Name}, Groups: []AppGroupResult{}}
	base := "/apps/" + strconv.Itoa(app.ID)
	for _, team := range []struct {
		role, path string
		groups     []string
	}{{"responsible", "responsibles", options.Responsibles}, {"publication", "publications", options.Publications}} {
		for _, group := range team.groups {
			change := AppGroupResult{Group: group, Role: team.role}
			if group == "" || strings.Contains(group, "/") {
				change.Outcome, change.Reason = ChangeRefused, "a group is one exact group name or group ID"
			} else {
				// The collector resolves the group, by its ID or its name.
				path := base + "/" + team.path + "/" + url.PathEscape(group)
				var response struct {
					Info string `json:"info"`
				}
				if attach {
					err = s.client.PostJSON(ctx, path, nil, &response)
				} else {
					err = s.client.DeleteJSON(ctx, path, nil, &response)
				}
				change.Outcome, change.Reason = appGroupOutcome(err, attach)
			}
			switch change.Outcome {
			case ChangeAttached, ChangeDetached:
				result.Changed++
			case ChangeAlreadyAttached, ChangeAlreadyDetached:
				result.Unchanged++
			default:
				result.Refused++
			}
			result.Groups = append(result.Groups, change)
		}
	}
	// An app without responsible group is changed by a Manager only: the
	// groups left are read back after a detachment of responsibles.
	if !attach && len(options.Responsibles) > 0 {
		var groups collectorList[struct {
			Role string `json:"role"`
		}]
		if err := s.client.GetJSON(ctx, base+"/responsibles", url.Values{"props": {"role"}, "limit": {strconv.Itoa(maxAppResponsibles + 1)}}, &groups); err == nil {
			for _, group := range groups.Data {
				result.Responsibles = append(result.Responsibles, group.Role)
			}
			if len(groups.Data) == 0 {
				result.Warning = fmt.Sprintf("no group is responsible for app %s any more: only a Manager may change it, or its nodes and services", app.Name)
			}
		}
		// A caller who withdrew their own group no longer manages the app.
		var responsible bool
		if err := s.client.GetJSON(ctx, base+"/am_i_responsible", nil, &responsible); err != nil {
			if status, _ := refusal(err); status == http.StatusForbidden && result.Warning == "" {
				result.Warning = fmt.Sprintf("the caller is no longer responsible for app %s: a group still responsible, or a Manager, may attach theirs back", app.Name)
			}
		}
	}
	result.Provenance = s.newProvenance()
	return result, nil
}

// appGroupOutcome reads the answer of the collector to a change of the
// groups of an app, which refuses a group already as asked: 409 to an
// attachment, 404 to a detachment, as server/handlers/post_app_responsible.go
// and its siblings of oc3 word them.
func appGroupOutcome(err error, attach bool) (outcome, reason string) {
	if err == nil {
		if attach {
			return ChangeAttached, ""
		}
		return ChangeDetached, ""
	}
	status, message := refusal(err)
	switch {
	case attach && status == http.StatusConflict && strings.Contains(message, "already"):
		return ChangeAlreadyAttached, ""
	case !attach && status == http.StatusNotFound && (strings.Contains(message, " is not responsible for ") || strings.Contains(message, " is not published to ")):
		return ChangeAlreadyDetached, ""
	}
	return ChangeRefused, err.Error()
}
