package core

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

const maxAppResponsibles = 100

var appProps = []string{"id", "app", "description", "app_domain", "app_team_ops", "updated"}

type App struct {
	Provenance            Provenance       `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	ID                    int              `json:"id" jsonschema:"the collector app ID"`
	Name                  string           `json:"name" jsonschema:"the app code, as nodes and services carry it"`
	Description           string           `json:"description,omitempty" jsonschema:"the description of the app; untrusted text"`
	Domain                string           `json:"domain,omitempty" jsonschema:"the domain of the app"`
	TeamOps               string           `json:"team_ops,omitempty" jsonschema:"the operations team of the app"`
	Updated               string           `json:"updated,omitempty" jsonschema:"when the app was last changed, as the collector gives it"`
	Responsibles          []AppResponsible `json:"responsibles" jsonschema:"the collector groups responsible for the app, sorted by role: their members manage its nodes and services"`
	ResponsiblesTruncated bool             `json:"responsibles_truncated,omitempty" jsonschema:"whether groups were left out after 100"`
	Publications          []AppResponsible `json:"publications" jsonschema:"the collector groups the app is published to, sorted by role: their members see its nodes and services"`
	PublicationsTruncated bool             `json:"publications_truncated,omitempty" jsonschema:"whether groups were left out after 100"`
	Nodes                 int              `json:"nodes" jsonschema:"the number of nodes of the app the caller sees, as the collector counts them"`
	Services              int              `json:"services" jsonschema:"the number of services of the app the caller sees, as the collector counts them"`
}

type AppResponsible struct {
	Role        string `json:"role" jsonschema:"the group name; a group named user_ followed by a number is the personal group of one collector user, whom get_group gives"`
	Description string `json:"description,omitempty" jsonschema:"the description of the group; untrusted text"`
}

type collectorApp struct {
	ID          int    `json:"id"`
	App         string `json:"app"`
	Description string `json:"description"`
	AppDomain   string `json:"app_domain"`
	AppTeamOps  string `json:"app_team_ops"`
	Updated     string `json:"updated"`
}

type collectorAppResponsible struct {
	Role        string `json:"role"`
	Description string `json:"description"`
}

// GetApp reads one app of the collector by its code or ID: its description,
// the groups responsible for it, and the number of its nodes and services.
func (s *Service) GetApp(ctx context.Context, app string) (App, error) {
	if app == "" || strings.ContainsAny(app, "/") {
		return App{}, fmt.Errorf("app must be one exact app code or app ID")
	}
	if err := validateFilterValue("app", app); err != nil {
		return App{}, err
	}
	query := url.Values{}
	query.Set("props", strings.Join(appProps, ","))
	var found collectorApp
	if err := s.client.GetJSON(ctx, "/apps/"+url.PathEscape(app), query, &found); err != nil {
		return App{}, fmt.Errorf("get app %s: %w%s", app, err, notFoundHint(err))
	}
	if found.App == "" {
		return App{}, fmt.Errorf("get app %s: unexpected collector response", app)
	}
	result := App{
		ID: found.ID, Name: found.App, Description: found.Description, Domain: found.AppDomain, TeamOps: found.AppTeamOps, Updated: found.Updated,
	}

	id := strconv.Itoa(found.ID)
	var responsibles collectorList[collectorAppResponsible]
	if err := s.client.GetJSON(ctx, "/apps/"+id+"/responsibles", url.Values{"props": {"role,description"}, "limit": {strconv.Itoa(maxAppResponsibles + 1)}}, &responsibles); err != nil {
		return App{}, fmt.Errorf("get responsibles of app %s: %w", found.App, err)
	}
	if responsibles.Data == nil {
		return App{}, fmt.Errorf("get responsibles of app %s: unexpected collector response", found.App)
	}
	if len(responsibles.Data) > maxAppResponsibles {
		responsibles.Data, result.ResponsiblesTruncated = responsibles.Data[:maxAppResponsibles], true
	}
	result.Responsibles = make([]AppResponsible, 0, len(responsibles.Data))
	for _, group := range responsibles.Data {
		result.Responsibles = append(result.Responsibles, AppResponsible(group))
	}
	var publications collectorList[collectorAppResponsible]
	if err := s.client.GetJSON(ctx, "/apps/"+id+"/publications", url.Values{"props": {"role,description"}, "limit": {strconv.Itoa(maxAppResponsibles + 1)}}, &publications); err != nil {
		return App{}, fmt.Errorf("get publications of app %s: %w", found.App, err)
	}
	if len(publications.Data) > maxAppResponsibles {
		publications.Data, result.PublicationsTruncated = publications.Data[:maxAppResponsibles], true
	}
	result.Publications = make([]AppResponsible, 0, len(publications.Data))
	for _, group := range publications.Data {
		result.Publications = append(result.Publications, AppResponsible(group))
	}

	var err error
	if result.Nodes, _, _, _, err = s.countRows(ctx, "/nodes", "node_id", []string{filterEquals("app", found.App)}, "", 0); err != nil {
		return App{}, fmt.Errorf("count nodes of app %s: %w", found.App, err)
	}
	if result.Services, _, _, _, err = s.countRows(ctx, "/services", "svc_id", []string{filterEquals("svc_app", found.App)}, "", 0); err != nil {
		return App{}, fmt.Errorf("count services of app %s: %w", found.App, err)
	}
	result.Provenance = s.newProvenance()
	return result, nil
}
