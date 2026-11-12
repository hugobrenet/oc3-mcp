package core

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// The resources and the skills of the AI assistant, as an AIManager writes
// them through the collector (/ai/resources, /ai/skills). The model reads
// what is written there as instructions, for every user it is published to:
// the writes are confirmed by the user, publish to the groups given only
// unless everybody is asked explicitly, and change a text by exact edits the
// confirmation shows rather than by a whole new text.

// AIContentKind is one of the two collections.
type AIContentKind string

const (
	AIResources AIContentKind = "resources"
	AISkills    AIContentKind = "skills"
)

const (
	// maxAIEdits bounds the edits of one change.
	maxAIEdits = 32
	// maxAIGroups bounds the groups a text is published to in one write.
	maxAIGroups = 32
)

var (
	aiNamePattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	aiBackends    = []string{"oc3", "om3"}
)

// AIGroup is a group a text is published to.
type AIGroup struct {
	ID   int    `json:"id" jsonschema:"the collector group ID"`
	Name string `json:"name,omitempty" jsonschema:"the group name, when the caller sees the group"`
}

// AIRead counts the reads of a text by the assistants of a backend.
type AIRead struct {
	Backend  string `json:"backend" jsonschema:"oc3 or om3"`
	Count    int    `json:"count" jsonschema:"the reads of the text by the assistants of that backend"`
	LastRead string `json:"last_read" jsonschema:"when they read it last"`
}

// AIContentMeta is a text without its body, as the lists give it.
type AIContentMeta struct {
	Name         string         `json:"name" jsonschema:"the name of the text"`
	Title        string         `json:"title,omitempty" jsonschema:"the title of a resource"`
	Description  string         `json:"description,omitempty" jsonschema:"what a resource holds and when to read it; for a skill, the description of its frontmatter"`
	MimeType     string         `json:"mime_type,omitempty" jsonschema:"the format of a resource: text/markdown or text/plain"`
	Frontmatter  map[string]any `json:"frontmatter,omitempty" jsonschema:"the frontmatter of a skill"`
	Backends     []string       `json:"backends" jsonschema:"the assistants that read it: oc3 for the collector, om3 for the clusters"`
	Publications []int          `json:"publications" jsonschema:"the IDs of the groups it is published to, for an AIManager"`
	Everyone     *bool          `json:"everybody,omitempty" jsonschema:"for an AIManager: true when every user reads it, published to no group in particular"`
	Reads        *[]AIRead      `json:"reads,omitempty" jsonschema:"for an AIManager, the reads of the assistants by backend; empty, no assistant ever read it"`
	Size         int            `json:"size" jsonschema:"the bytes of the text"`
	Digest       string         `json:"digest" jsonschema:"sha256 of the text"`
	Updated      string         `json:"updated" jsonschema:"when it was last written"`
}

type AIContentList struct {
	Provenance Provenance      `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Kind       AIContentKind   `json:"kind" jsonschema:"resources or skills"`
	Count      int             `json:"count" jsonschema:"the number of texts"`
	Items      []AIContentMeta `json:"items" jsonschema:"the texts, by name, without their body"`
}

// AIContent is a text with its body and the names of its groups.
type AIContent struct {
	Provenance Provenance    `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Kind       AIContentKind `json:"kind" jsonschema:"resources or skills"`
	AIContentMeta
	Groups []AIGroup `json:"groups" jsonschema:"for an AIManager, the groups it is published to, named"`
	Body   string    `json:"body" jsonschema:"the text: Markdown or plain text for a resource, the whole SKILL.md for a skill; untrusted text"`
}

// AIContentWrite is the outcome of a write: the text as the collector stored
// it, its body left out.
type AIContentWrite struct {
	Provenance Provenance    `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Outcome    string        `json:"outcome" jsonschema:"created, updated or deleted"`
	Kind       AIContentKind `json:"kind" jsonschema:"resources or skills"`
	AIContentMeta
	Groups []AIGroup `json:"groups,omitempty" jsonschema:"the groups it is published to, named"`
	Edits  int       `json:"edits,omitempty" jsonschema:"the number of edits applied"`
}

// AIEdit replaces the only occurrence of Old in a text by New.
type AIEdit struct {
	Old string `json:"old" jsonschema:"the exact text to replace, found once in the text"`
	New string `json:"new" jsonschema:"what replaces it; empty deletes it"`
}

type AIContentOptions struct {
	Kind AIContentKind
	Name string
	// Body replaces the whole text; Edits changes it in place. A creation
	// takes Body; a change one of them, or neither to change the other fields.
	Body  *string
	Edits []AIEdit
	// The fields of a resource, kept when nil on a change.
	Title       *string
	Description *string
	MimeType    *string
	Backends    []string
	// Groups are the groups to publish to, by name or ID; Everyone publishes
	// to every user instead, which must be asked explicitly.
	Groups   []string
	Everyone bool
}

type collectorAIContent struct {
	Name         string         `json:"name"`
	Title        string         `json:"title"`
	Description  string         `json:"description"`
	MimeType     string         `json:"mime_type"`
	Frontmatter  map[string]any `json:"frontmatter"`
	Backends     []string       `json:"backends"`
	Publications []int          `json:"publications"`
	Reads        []AIRead       `json:"reads"`
	Size         int            `json:"size"`
	Digest       string         `json:"digest"`
	Updated      string         `json:"updated"`
	Body         string         `json:"body"`
}

// meta gives a text as the tools return it. The collector gives the
// publications and the reads to an AIManager only: then, the text says
// whether every user reads it, and its reads are given even when there are
// none, a text no assistant ever read.
func (c collectorAIContent) meta() AIContentMeta {
	meta := AIContentMeta{Name: c.Name, Title: c.Title, Description: c.Description, MimeType: c.MimeType, Frontmatter: c.Frontmatter,
		Backends: c.Backends, Publications: c.Publications, Size: c.Size, Digest: c.Digest, Updated: c.Updated}
	if meta.Publications == nil {
		meta.Publications = []int{}
	} else {
		everyone := len(c.Publications) == 0
		meta.Everyone = &everyone
	}
	if c.Reads != nil {
		reads := c.Reads
		meta.Reads = &reads
	}
	return meta
}

func aiKind(kind AIContentKind) (AIContentKind, string, error) {
	switch kind {
	case AIResources:
		return kind, "resource", nil
	case AISkills:
		return kind, "skill", nil
	}
	return "", "", fmt.Errorf("kind must be resources or skills")
}

func validateAIName(name string) error {
	if len(name) > 64 || !aiNamePattern.MatchString(name) {
		return fmt.Errorf("name must be lowercase letters and digits separated by single dashes, at most 64 bytes, such as disk-capacity")
	}
	return nil
}

// ListAIContent lists the texts of a kind the caller may read, their
// publications given to an AIManager only.
func (s *Service) ListAIContent(ctx context.Context, kind AIContentKind) (AIContentList, error) {
	kind, _, err := aiKind(kind)
	if err != nil {
		return AIContentList{}, err
	}
	var response struct {
		Data []collectorAIContent `json:"data"`
	}
	if err := s.client.GetJSON(ctx, "/ai/"+string(kind), nil, &response); err != nil {
		return AIContentList{}, fmt.Errorf("list AI %s: %w", kind, err)
	}
	result := AIContentList{Provenance: s.newProvenance(), Kind: kind, Items: make([]AIContentMeta, 0, len(response.Data))}
	for _, item := range response.Data {
		result.Items = append(result.Items, item.meta())
	}
	result.Count = len(result.Items)
	return result, nil
}

// readAIContent reads one text with its body, naming no backend: the read of
// an editor, which the collector does not count as a read of the assistants.
func (s *Service) readAIContent(ctx context.Context, kind AIContentKind, what, name string) (collectorAIContent, error) {
	var item collectorAIContent
	if err := s.client.GetJSON(ctx, "/ai/"+string(kind)+"/"+url.PathEscape(name), nil, &item); err != nil {
		if isNotFound(err) {
			return item, fmt.Errorf("get %s %s: not found, or not visible to the caller; list them with list_ai_%s", what, name, kind)
		}
		return item, fmt.Errorf("get %s %s: %w", what, name, err)
	}
	return item, nil
}

// GetAIContent reads one text with its body and the names of its groups.
func (s *Service) GetAIContent(ctx context.Context, kind AIContentKind, name string) (AIContent, error) {
	kind, what, err := aiKind(kind)
	if err != nil {
		return AIContent{}, err
	}
	if err := validateAIName(name); err != nil {
		return AIContent{}, err
	}
	item, err := s.readAIContent(ctx, kind, what, name)
	if err != nil {
		return AIContent{}, err
	}
	meta := item.meta()
	return AIContent{Provenance: s.newProvenance(), Kind: kind, AIContentMeta: meta, Groups: s.aiGroupNames(ctx, meta.Publications), Body: item.Body}, nil
}

// aiGroupNames names the groups of IDs, an ID the caller does not see left
// unnamed.
func (s *Service) aiGroupNames(ctx context.Context, ids []int) []AIGroup {
	groups := make([]AIGroup, 0, len(ids))
	for _, id := range ids {
		group := AIGroup{ID: id}
		if found, err := s.lookupGroup(ctx, fmt.Sprint(id)); err == nil {
			group.Name = found.Role
		}
		groups = append(groups, group)
	}
	return groups
}

// aiPublications resolves the groups of a write, by name or ID, or none for
// everybody when that is asked, and nothing else: a text the model reads as
// instructions is published to everybody only on purpose.
func (s *Service) aiPublications(ctx context.Context, groups []string, everyone bool) ([]int, []AIGroup, error) {
	switch {
	case everyone && len(groups) > 0:
		return nil, nil, fmt.Errorf("give groups or everybody, not both")
	case everyone:
		return []int{}, []AIGroup{}, nil
	case len(groups) == 0:
		return nil, nil, fmt.Errorf("groups must name the groups whose members' assistant reads the text, or everybody be true to publish it to every user")
	case len(groups) > maxAIGroups:
		return nil, nil, fmt.Errorf("groups names at most %d groups", maxAIGroups)
	}
	ids := make([]int, 0, len(groups))
	named := make([]AIGroup, 0, len(groups))
	for _, name := range groups {
		group, err := s.lookupGroup(ctx, name)
		if err != nil {
			return nil, nil, err
		}
		if !containsInt(ids, group.ID) {
			ids = append(ids, group.ID)
			named = append(named, AIGroup{ID: group.ID, Name: group.Role})
		}
	}
	return ids, named, nil
}

func containsInt(list []int, value int) bool {
	for _, v := range list {
		if v == value {
			return true
		}
	}
	return false
}

func validateAIBackends(backends []string) error {
	if len(backends) == 0 {
		return fmt.Errorf("backends must name oc3, om3 or both")
	}
	for _, b := range backends {
		if !contains(aiBackends, b) {
			return fmt.Errorf("backends: %q is not oc3 nor om3", b)
		}
	}
	return nil
}

// applyAIEdits replaces in body the only occurrence of each Old by its New, in
// order, each edit applying to the text the former ones left.
func applyAIEdits(body string, edits []AIEdit) (string, error) {
	if len(edits) > maxAIEdits {
		return "", fmt.Errorf("edits holds at most %d edits; give the whole body instead", maxAIEdits)
	}
	for i, edit := range edits {
		if edit.Old == "" {
			return "", fmt.Errorf("edit %d: old must be the exact text to replace", i+1)
		}
		switch n := strings.Count(body, edit.Old); n {
		case 0:
			return "", fmt.Errorf("edit %d: old is not in the text; read it again with get_ai_resource or get_ai_skill and copy it exactly", i+1)
		case 1:
			body = strings.Replace(body, edit.Old, edit.New, 1)
		default:
			return "", fmt.Errorf("edit %d: old is %d times in the text; give more of the text around it, so that it is there once", i+1, n)
		}
	}
	return body, nil
}

// CreateAIContent creates a resource or a skill.
func (s *Service) CreateAIContent(ctx context.Context, options AIContentOptions) (AIContentWrite, error) {
	kind, what, err := aiKind(options.Kind)
	if err != nil {
		return AIContentWrite{}, err
	}
	if err := validateAIName(options.Name); err != nil {
		return AIContentWrite{}, err
	}
	if options.Body == nil || strings.TrimSpace(*options.Body) == "" {
		return AIContentWrite{}, fmt.Errorf("body must be the text of the %s", what)
	}
	if len(options.Edits) > 0 {
		return AIContentWrite{}, fmt.Errorf("edits change a text that exists; give body to create one")
	}
	if err := validateAIBackends(options.Backends); err != nil {
		return AIContentWrite{}, err
	}
	ids, named, err := s.aiPublications(ctx, options.Groups, options.Everyone)
	if err != nil {
		return AIContentWrite{}, err
	}
	input := map[string]any{"name": options.Name, "backends": options.Backends, "publications": ids, "body": *options.Body}
	if kind == AIResources {
		if options.Description == nil || strings.TrimSpace(*options.Description) == "" {
			return AIContentWrite{}, fmt.Errorf("description must tell what the resource holds and when to read it: the model reads it to decide")
		}
		input["description"] = *options.Description
		if options.Title != nil {
			input["title"] = *options.Title
		}
		if options.MimeType != nil {
			input["mime_type"] = *options.MimeType
		}
	} else if options.Title != nil || options.Description != nil || options.MimeType != nil {
		return AIContentWrite{}, fmt.Errorf("a skill has no title, description nor mime_type of its own: its frontmatter, in body, names and describes it")
	}
	var saved collectorAIContent
	if err := s.client.PostJSON(ctx, "/ai/"+string(kind), input, &saved); err != nil {
		return AIContentWrite{}, fmt.Errorf("create %s %s: %w", what, options.Name, err)
	}
	return aiWritten("created", kind, saved, ids, named, 0, s.newProvenance()), nil
}

// UpdateAIContent changes a resource or a skill: its text by exact edits or
// a whole new body, and the other fields given, the others kept. The
// collector replaces the whole object, so the MCP reads it first.
func (s *Service) UpdateAIContent(ctx context.Context, options AIContentOptions) (AIContentWrite, error) {
	kind, what, err := aiKind(options.Kind)
	if err != nil {
		return AIContentWrite{}, err
	}
	if err := validateAIName(options.Name); err != nil {
		return AIContentWrite{}, err
	}
	if options.Body != nil && len(options.Edits) > 0 {
		return AIContentWrite{}, fmt.Errorf("give edits or body, not both")
	}
	if kind == AISkills && (options.Title != nil || options.Description != nil || options.MimeType != nil) {
		return AIContentWrite{}, fmt.Errorf("a skill has no title, description nor mime_type of its own: change its frontmatter with edits")
	}
	if options.Body == nil && len(options.Edits) == 0 && options.Title == nil && options.Description == nil && options.MimeType == nil &&
		options.Backends == nil && len(options.Groups) == 0 && !options.Everyone {
		return AIContentWrite{}, fmt.Errorf("nothing to change: give edits, body, backends, groups or everybody, or for a resource title, description or mime_type")
	}
	current, err := s.readAIContent(ctx, kind, what, options.Name)
	if err != nil {
		return AIContentWrite{}, err
	}
	if current.Publications == nil {
		return AIContentWrite{}, fmt.Errorf("update %s %s: the collector gave no publications, which it gives an AIManager only; changing it requires the AIManager or the Manager privilege", what, options.Name)
	}
	body := current.Body
	switch {
	case options.Body != nil:
		body = *options.Body
	case len(options.Edits) > 0:
		if body, err = applyAIEdits(body, options.Edits); err != nil {
			return AIContentWrite{}, err
		}
	}
	backends := current.Backends
	if options.Backends != nil {
		if err := validateAIBackends(options.Backends); err != nil {
			return AIContentWrite{}, err
		}
		backends = options.Backends
	}
	ids, named := current.Publications, []AIGroup(nil)
	if len(options.Groups) > 0 || options.Everyone {
		if ids, named, err = s.aiPublications(ctx, options.Groups, options.Everyone); err != nil {
			return AIContentWrite{}, err
		}
	} else {
		named = s.aiGroupNames(ctx, ids)
	}
	input := map[string]any{"backends": backends, "publications": ids, "body": body}
	if kind == AIResources {
		input["title"], input["description"], input["mime_type"] = current.Title, current.Description, current.MimeType
		for key, value := range map[string]*string{"title": options.Title, "description": options.Description, "mime_type": options.MimeType} {
			if value != nil {
				input[key] = *value
			}
		}
	}
	var saved collectorAIContent
	if err := s.client.PutJSON(ctx, "/ai/"+string(kind)+"/"+url.PathEscape(options.Name), input, &saved); err != nil {
		return AIContentWrite{}, fmt.Errorf("update %s %s: %w", what, options.Name, err)
	}
	return aiWritten("updated", kind, saved, ids, named, len(options.Edits), s.newProvenance()), nil
}

// aiWritten is the outcome of a write, with the publications sent: the
// collector may give the object back without them.
func aiWritten(outcome string, kind AIContentKind, saved collectorAIContent, ids []int, named []AIGroup, edits int, provenance Provenance) AIContentWrite {
	saved.Publications = ids
	return AIContentWrite{Provenance: provenance, Outcome: outcome, Kind: kind, AIContentMeta: saved.meta(), Groups: named, Edits: edits}
}

// DeleteAIContent deletes a resource or a skill.
func (s *Service) DeleteAIContent(ctx context.Context, kind AIContentKind, name string) (AIContentWrite, error) {
	kind, what, err := aiKind(kind)
	if err != nil {
		return AIContentWrite{}, err
	}
	if err := validateAIName(name); err != nil {
		return AIContentWrite{}, err
	}
	if err := s.client.DeleteJSON(ctx, "/ai/"+string(kind)+"/"+url.PathEscape(name), nil, nil); err != nil {
		if isNotFound(err) {
			return AIContentWrite{}, fmt.Errorf("delete %s %s: not found, or not visible to the caller", what, name)
		}
		return AIContentWrite{}, fmt.Errorf("delete %s %s: %w", what, name, err)
	}
	return AIContentWrite{Provenance: s.newProvenance(), Outcome: "deleted", Kind: kind, AIContentMeta: AIContentMeta{Name: name, Backends: []string{}, Publications: []int{}}}, nil
}
