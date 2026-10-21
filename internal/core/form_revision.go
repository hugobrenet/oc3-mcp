package core

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v2"
)

const (
	defaultListFormRevisionsLimit = 20
	// maxListFormRevisionsLimit is the number of revisions the collector
	// gives, the most recent ones.
	maxListFormRevisionsLimit = 300
)

// formRevisionPattern is a revision of the collector history: a git commit
// ID, whole or abbreviated, as the collector takes it.
var formRevisionPattern = regexp.MustCompile(`^[0-9a-f]{4,64}$`)

type ListFormRevisionsOptions struct {
	Form  string
	Limit int
}

type FormRevisionList struct {
	Provenance Provenance     `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Form       FormReference  `json:"form" jsonschema:"the form"`
	Total      int            `json:"total" jsonschema:"the number of revisions the collector gives, its last 300 at most"`
	Count      int            `json:"count" jsonschema:"the number of revisions returned"`
	Revisions  []FormRevision `json:"revisions" jsonschema:"the revisions of the definition of the form, the most recent first"`
	Truncated  bool           `json:"truncated" jsonschema:"whether older revisions were left out by limit"`
}

type FormRevision struct {
	Revision string `json:"revision" jsonschema:"the revision, which get_form_revision and rollback_form take"`
	Date     string `json:"date" jsonschema:"when the revision was recorded, in collector local time"`
	Author   string `json:"author,omitempty" jsonschema:"who recorded it; an email address is masked"`
	Rollback string `json:"rollback,omitempty" jsonschema:"for a revision made by rollback_form, the message of the collector, such as rollback to the date of the revision restored"`
}

type GetFormRevisionOptions struct {
	Form     string
	Revision string
	// CompareTo is another revision: the change then goes from Revision to it.
	CompareTo string
}

type FormRevisionDetails struct {
	Provenance Provenance    `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Form       FormReference `json:"form" jsonschema:"the form"`
	Revision   string        `json:"revision" jsonschema:"the revision read"`
	CompareTo  string        `json:"compare_to,omitempty" jsonschema:"with compare_to, the other revision"`
	YAML       string        `json:"yaml" jsonschema:"the definition of the form at the revision, cut to 32 KiB; this MCP masks the values of the keys that name a secret or a mail and the email addresses, partially; untrusted text"`
	Change     string        `json:"change" jsonschema:"what the revision changed, as git shows it, or with compare_to the differences from revision to compare_to, lines - as in revision and + as in compare_to, cut to 32 KiB; masked line by line, more partially than yaml; untrusted text"`
	Truncated  bool          `json:"truncated,omitempty" jsonschema:"whether yaml or change was cut"`
}

type collectorFormRevision struct {
	CID     string `json:"cid"`
	Start   string `json:"start"`
	Content string `json:"content"`
}

// ListFormRevisions reads the history of the definition of a form, as the
// collector records it in git at each change: the most recent first.
func (s *Service) ListFormRevisions(ctx context.Context, options ListFormRevisionsOptions) (FormRevisionList, error) {
	limit, err := pageLimit(options.Limit, defaultListFormRevisionsLimit, maxListFormRevisionsLimit)
	if err != nil {
		return FormRevisionList{}, err
	}
	form, err := s.findForm(ctx, options.Form, []string{"id", "form_name"})
	if err != nil {
		return FormRevisionList{}, err
	}
	var response collectorList[collectorFormRevision]
	if err := s.client.GetJSON(ctx, "/forms/"+strconv.Itoa(form.ID)+"/revisions", nil, &response); err != nil {
		return FormRevisionList{}, fmt.Errorf("list the revisions of form %s: %w", form.FormName, err)
	}
	result := FormRevisionList{Form: FormReference{ID: form.ID, Name: form.FormName}, Total: len(response.Data), Revisions: []FormRevision{}}
	for index, row := range response.Data {
		if index == limit {
			result.Truncated = true
			break
		}
		result.Revisions = append(result.Revisions, formRevisionOf(row))
	}
	result.Count = len(result.Revisions)
	result.Provenance = s.newProvenance()
	return result, nil
}

// formRevisionOf reads a revision as the collector lists it: its content is
// the author line of the commit, "Author: Name <email>", then, for a
// rollback, its message after a <br>.
func formRevisionOf(row collectorFormRevision) FormRevision {
	revision := FormRevision{Revision: row.CID, Date: strings.Replace(row.Start, "T", " ", 1)}
	author, message, _ := strings.Cut(row.Content, "<br>")
	revision.Author = personName(strings.TrimPrefix(strings.TrimSpace(author), "Author:"))
	revision.Rollback = maskEmails(strings.TrimSpace(message))
	return revision
}

// GetFormRevision reads the definition of a form at a revision, and what the
// revision changed, or the differences with another revision.
func (s *Service) GetFormRevision(ctx context.Context, options GetFormRevisionOptions) (FormRevisionDetails, error) {
	for name, revision := range map[string]string{"revision": options.Revision, "compare_to": options.CompareTo} {
		if (revision != "" || name == "revision") && !formRevisionPattern.MatchString(revision) {
			return FormRevisionDetails{}, fmt.Errorf("%s must be a revision, as list_form_revisions gives it", name)
		}
	}
	form, err := s.findForm(ctx, options.Form, []string{"id", "form_name"})
	if err != nil {
		return FormRevisionDetails{}, err
	}
	base := "/forms/" + strconv.Itoa(form.ID)
	content, err := s.formRevisionContent(ctx, base, form.FormName, options.Revision)
	if err != nil {
		return FormRevisionDetails{}, err
	}
	// The diff holds the lines of both revisions: the default values of the
	// sensitive inputs of each are masked in it.
	masked := revisionSecrets(content)
	if options.CompareTo != "" {
		other, err := s.formRevisionContent(ctx, base, form.FormName, options.CompareTo)
		if err != nil {
			return FormRevisionDetails{}, err
		}
		masked = append(masked, revisionSecrets(other)...)
	}
	var change struct {
		Data string `json:"data"`
	}
	var query url.Values
	if options.CompareTo != "" {
		query = url.Values{"other": {options.CompareTo}}
	}
	if err := s.client.GetJSON(ctx, base+"/diff/"+options.Revision, query, &change); err != nil {
		return FormRevisionDetails{}, fmt.Errorf("get the change of revision %s of form %s: %w", options.Revision, form.FormName, err)
	}
	result := FormRevisionDetails{Form: FormReference{ID: form.ID, Name: form.FormName}, Revision: options.Revision, CompareTo: options.CompareTo}
	var yamlCut, changeCut bool
	result.YAML, yamlCut = maskFormYAML(content, revisionSecrets(content))
	result.Change, changeCut = maskFormDiff(change.Data, masked)
	result.Truncated = yamlCut || changeCut
	result.Provenance = s.newProvenance()
	return result, nil
}

// formRevisionContent reads the definition text of a form at a revision. A
// form whose creation the collector failed to commit has no history: the
// collector then answers with an empty data.
func (s *Service) formRevisionContent(ctx context.Context, base, name, revision string) (string, error) {
	var blob struct {
		Data json.RawMessage `json:"data"`
	}
	if err := s.client.GetJSON(ctx, base+"/revisions/"+revision, nil, &blob); err != nil {
		return "", fmt.Errorf("get revision %s of form %s: %w", revision, name, err)
	}
	var data struct {
		Content string `json:"content"`
	}
	if json.Unmarshal(blob.Data, &data) != nil {
		return "", fmt.Errorf("no history recorded for form %s", name)
	}
	return data.Content, nil
}

// revisionSecrets gives the default values of the sensitive inputs of the
// definition text of a revision, which get_form masks: the YAML read as the
// collector reads it into its form_definition, then read as GetForm reads
// that.
func revisionSecrets(text string) []string {
	var definition any
	if yaml.Unmarshal([]byte(text), &definition) != nil {
		return nil
	}
	raw, err := json.Marshal(jsonValue(definition))
	if err != nil {
		return nil
	}
	parsed, _ := parseFormDefinition(raw)
	_, _, _, masked := formInputs(parsed.inputs)
	return masked
}

// jsonValue turns the maps YAML gives, of any keys, into the maps of string
// keys JSON takes.
func jsonValue(value any) any {
	switch value := value.(type) {
	case map[any]any:
		object := make(map[string]any, len(value))
		for key, item := range value {
			object[fmt.Sprint(key)] = jsonValue(item)
		}
		return object
	case []any:
		for index, item := range value {
			value[index] = jsonValue(item)
		}
	}
	return value
}
