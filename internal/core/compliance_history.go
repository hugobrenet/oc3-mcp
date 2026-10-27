package core

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

const (
	// maxComplianceVersions bounds the versions one history read gives.
	maxComplianceVersions = 100
	// maxComplianceVersionBody bounds the message body of a version.
	maxComplianceVersionBody = 1024
	// maxComplianceDiffBytes bounds the diff of a version.
	maxComplianceDiffBytes = 32 << 10
	// maxComplianceVersionChanges bounds the objects a version lists as
	// changed.
	maxComplianceVersionChanges = 200
)

// complianceCommitPattern is a commit of the compliance history, full or
// abbreviated.
var complianceCommitPattern = regexp.MustCompile(`^[0-9a-f]{4,40}$`)

type ComplianceHistory struct {
	Provenance Provenance          `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Object     string              `json:"object,omitempty" jsonschema:"the object the versions changed, as kind:name, when one is asked"`
	Versions   []ComplianceVersion `json:"versions" jsonschema:"the versions, the newest first"`
}

type ComplianceVersion struct {
	Commit      string `json:"commit" jsonschema:"the commit of the version, which get_compliance_version reads"`
	Date        string `json:"date" jsonschema:"when it was recorded, RFC 3339"`
	Author      string `json:"author" jsonschema:"who recorded it; an email address is masked"`
	Subject     string `json:"subject" jsonschema:"the first line of its message; an email address is masked"`
	Body        string `json:"body,omitempty" jsonschema:"the rest of its message, cut to 1 KiB; an email address is masked"`
	Source      string `json:"source,omitempty" jsonschema:"designer, for a change made with the designer of the collector or with apply_compliance_changes, elsewhere, for the changes made by other means it gathers, or empty"`
	ByAssistant bool   `json:"by_assistant,omitempty" jsonschema:"true when apply_compliance_changes recorded it: its subject starts with the mark this MCP gives"`
}

type ComplianceVersionDetails struct {
	Provenance       Provenance               `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Version          ComplianceVersion        `json:"version" jsonschema:"the version"`
	Previous         string                   `json:"previous_commit,omitempty" jsonschema:"the version before it, which the changes compare with; absent for the first one"`
	Changes          []ComplianceObjectChange `json:"changes" jsonschema:"the rulesets, modulesets and filtersets the version added, removed or changed, sorted by kind and name, at most 200"`
	ChangesTruncated bool                     `json:"changes_truncated,omitempty" jsonschema:"whether objects were left out beyond 200"`
	Diff             string                   `json:"diff" jsonschema:"the line diff of the compliance configuration with the version before, cut to 32 KiB; the values of the variables naming a secret, the sensitive keys of their JSON values and the email addresses are masked, but partially; untrusted text"`
	DiffTruncated    bool                     `json:"diff_truncated,omitempty" jsonschema:"whether the diff was cut to 32 KiB"`
}

type ComplianceObjectChange struct {
	Kind   string `json:"kind" jsonschema:"ruleset, moduleset or filterset"`
	Name   string `json:"name" jsonschema:"its name in the version, or in the one before when removed"`
	ID     int    `json:"id" jsonschema:"its collector ID"`
	Change string `json:"change" jsonschema:"added, removed or changed"`
}

type collectorComplianceVersion struct {
	ID      string `json:"id"`
	Date    string `json:"date"`
	Author  string `json:"author"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
	Source  string `json:"source"`
}

func (version collectorComplianceVersion) version() ComplianceVersion {
	body, _ := truncateUTF8(maskEmails(strings.TrimSpace(version.Body)), maxComplianceVersionBody)
	return ComplianceVersion{
		Commit:      version.ID,
		Date:        version.Date,
		Author:      personName(version.Author),
		Subject:     maskEmails(version.Subject),
		Body:        body,
		Source:      version.Source,
		ByAssistant: strings.HasPrefix(version.Subject, ComplianceChangeMark),
	}
}

// ListComplianceHistory reads the versions of the compliance configuration
// the collector records, the newest first: all, or those in which one
// ruleset, moduleset or filterset changed, among the last 300.
func (s *Service) ListComplianceHistory(ctx context.Context, ruleset, moduleset, filterset string, limit int) (ComplianceHistory, error) {
	if limit == 0 {
		limit = 20
	}
	if limit < 1 || limit > maxComplianceVersions {
		return ComplianceHistory{}, fmt.Errorf("limit must be 1 to %d", maxComplianceVersions)
	}
	given := 0
	for _, value := range []string{ruleset, moduleset, filterset} {
		if value != "" {
			given++
		}
	}
	if given > 1 {
		return ComplianceHistory{}, fmt.Errorf("pass one of ruleset, moduleset and filterset, not several")
	}
	query := url.Values{"limit": {strconv.Itoa(limit)}}
	result := ComplianceHistory{}
	switch {
	case ruleset != "":
		object, err := s.lookupComplianceObject(ctx, "ruleset", ruleset)
		if err != nil {
			return ComplianceHistory{}, err
		}
		query.Set("object", "ruleset:"+strconv.Itoa(object.id))
		result.Object = "ruleset:" + object.name
	case moduleset != "":
		object, err := s.lookupComplianceObject(ctx, "moduleset", moduleset)
		if err != nil {
			return ComplianceHistory{}, err
		}
		query.Set("object", "moduleset:"+strconv.Itoa(object.id))
		result.Object = "moduleset:" + object.name
	case filterset != "":
		found, err := s.findFilterset(ctx, filterset)
		if err != nil {
			return ComplianceHistory{}, err
		}
		query.Set("object", "filterset:"+strconv.Itoa(found.ID))
		result.Object = "filterset:" + found.Name
	}
	var response struct {
		Data []collectorComplianceVersion `json:"data"`
	}
	if err := s.client.GetJSON(ctx, "/compliance/history", query, &response); err != nil {
		return ComplianceHistory{}, fmt.Errorf("read the compliance history: %w", err)
	}
	result.Provenance = s.newProvenance()
	result.Versions = make([]ComplianceVersion, 0, len(response.Data))
	for _, version := range response.Data {
		result.Versions = append(result.Versions, version.version())
	}
	return result, nil
}

// GetComplianceVersion reads one version of the compliance history: the
// objects it changed, compared with the version before, and the line diff,
// masked and cut. The exports of the two versions are not given.
func (s *Service) GetComplianceVersion(ctx context.Context, commit string) (ComplianceVersionDetails, error) {
	commit = strings.ToLower(strings.TrimSpace(commit))
	if !complianceCommitPattern.MatchString(commit) {
		return ComplianceVersionDetails{}, fmt.Errorf("commit must be a commit of the compliance history, 4 to 40 hexadecimal digits, as list_compliance_history gives it")
	}
	var response struct {
		Version    collectorComplianceVersion `json:"version"`
		Export     json.RawMessage            `json:"export"`
		Previous   json.RawMessage            `json:"previous"`
		PreviousID string                     `json:"previous_id"`
		Diff       string                     `json:"diff"`
	}
	if err := s.client.GetJSON(ctx, "/compliance/history/"+url.PathEscape(commit), url.Values{}, &response); err != nil {
		if isNotFound(err) {
			return ComplianceVersionDetails{}, fmt.Errorf("compliance version %s not found; list_compliance_history lists them", commit)
		}
		return ComplianceVersionDetails{}, fmt.Errorf("read the compliance version %s: %w", commit, err)
	}
	current, err := readComplianceExport(response.Export)
	if err != nil {
		return ComplianceVersionDetails{}, fmt.Errorf("read the compliance version %s: %w", commit, err)
	}
	previous, err := readComplianceExport(response.Previous)
	if err != nil {
		return ComplianceVersionDetails{}, fmt.Errorf("read the version before %s: %w", commit, err)
	}
	changes := compareComplianceExports(previous, current)
	result := ComplianceVersionDetails{Provenance: s.newProvenance(), Version: response.Version.version(), Previous: response.PreviousID, Changes: changes}
	if len(changes) > maxComplianceVersionChanges {
		result.Changes, result.ChangesTruncated = changes[:maxComplianceVersionChanges], true
	}
	result.Diff, result.DiffTruncated = truncateUTF8(maskComplianceDiff(response.Diff, previous, current), maxComplianceDiffBytes)
	return result, nil
}

// complianceExportObject is an object of a compliance export: its name, its
// variables for a ruleset, and its whole JSON, which tells a change.
type complianceExportObject struct {
	name      string
	variables []collectorRulesetVariable
	raw       string
}

// readComplianceExport reads the objects of an export by kind and ID; an
// empty export, that of no version, holds none.
func readComplianceExport(raw json.RawMessage) (map[string]map[int]complianceExportObject, error) {
	objects := map[string]map[int]complianceExportObject{"filterset": {}, "ruleset": {}, "moduleset": {}}
	if len(bytes.TrimSpace(raw)) == 0 || string(bytes.TrimSpace(raw)) == "null" {
		return objects, nil
	}
	var export map[string][]json.RawMessage
	if err := json.Unmarshal(raw, &export); err != nil {
		return nil, err
	}
	for kind, list := range map[string]string{"filterset": "filtersets", "ruleset": "rulesets", "moduleset": "modulesets"} {
		for _, entry := range export[list] {
			var head struct {
				ID        int                        `json:"id"`
				Fset      string                     `json:"fset_name"`
				Ruleset   string                     `json:"ruleset_name"`
				Moduleset string                     `json:"modset_name"`
				Variables []collectorRulesetVariable `json:"variables"`
			}
			if err := json.Unmarshal(entry, &head); err != nil {
				return nil, err
			}
			name := map[string]string{"filterset": head.Fset, "ruleset": head.Ruleset, "moduleset": head.Moduleset}[kind]
			objects[kind][head.ID] = complianceExportObject{name: name, variables: head.Variables, raw: string(entry)}
		}
	}
	return objects, nil
}

// compareComplianceExports lists the objects added, removed or changed from
// one export to the next, by kind and name.
func compareComplianceExports(before, after map[string]map[int]complianceExportObject) []ComplianceObjectChange {
	var changes []ComplianceObjectChange
	for _, kind := range []string{"filterset", "moduleset", "ruleset"} {
		for id, object := range after[kind] {
			old, existed := before[kind][id]
			switch {
			case !existed:
				changes = append(changes, ComplianceObjectChange{Kind: kind, Name: object.name, ID: id, Change: "added"})
			case old.raw != object.raw:
				changes = append(changes, ComplianceObjectChange{Kind: kind, Name: object.name, ID: id, Change: "changed"})
			}
		}
		for id, object := range before[kind] {
			if _, kept := after[kind][id]; !kept {
				changes = append(changes, ComplianceObjectChange{Kind: kind, Name: object.name, ID: id, Change: "removed"})
			}
		}
	}
	slices.SortFunc(changes, func(a, b ComplianceObjectChange) int {
		if a.Kind != b.Kind {
			return strings.Compare(a.Kind, b.Kind)
		}
		if a.Name != b.Name {
			return strings.Compare(a.Name, b.Name)
		}
		return a.ID - b.ID
	})
	return changes
}

// maskComplianceDiff masks, in the diff of two exports, the values of the
// ruleset variables as get_compliance_ruleset masks them, found as the
// "var_value" lines of either export write them, then the email addresses
// left.
func maskComplianceDiff(diff string, exports ...map[string]map[int]complianceExportObject) string {
	replacements := map[string]string{}
	for _, export := range exports {
		for _, ruleset := range export["ruleset"] {
			for _, variable := range ruleset.variables {
				masked := maskVariableValue(variable.Name, variable.Value)
				if masked == variable.Value || variable.Value == "" {
					continue
				}
				replacements[`"var_value": "`+jsonStringText(variable.Value)+`"`] = `"var_value": "` + jsonStringText(masked) + `"`
			}
		}
	}
	// The longest first: a value holding another is masked whole.
	originals := make([]string, 0, len(replacements))
	for original := range replacements {
		originals = append(originals, original)
	}
	slices.SortFunc(originals, func(a, b string) int { return len(b) - len(a) })
	pairs := make([]string, 0, 2*len(originals))
	for _, original := range originals {
		pairs = append(pairs, original, replacements[original])
	}
	if len(pairs) > 0 {
		diff = strings.NewReplacer(pairs...).Replace(diff)
	}
	return maskEmails(diff)
}

// jsonStringText is a string as the export writes it inside its quotes:
// escaped as JSON, without HTML escaping.
func jsonStringText(value string) string {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(value)
	text := strings.TrimSuffix(buffer.String(), "\n")
	return strings.TrimSuffix(strings.TrimPrefix(text, `"`), `"`)
}
