package core

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/opensvc/oc3-mcp/internal/client"
)

const (
	// maxComplianceOperations bounds the operations of one change, far below
	// the 5000 of the collector: a longer list is no request a person reads
	// and confirms.
	maxComplianceOperations = 100
	// maxComplianceMessageBytes bounds the message of the version recording
	// a change.
	maxComplianceMessageBytes = 2048
	// maxComplianceReadBack bounds the objects read back after a change.
	maxComplianceReadBack = 10
	// ComplianceChangeMark starts the message of every version a change of
	// this MCP records: the collector names their source "designer", as for
	// its own designer, and the mark tells them apart in the history.
	ComplianceChangeMark = "[AI assistant] "
)

// The kinds, operations and team roles of a compliance change, as the
// designer commits of the collector take them.
var (
	ComplianceKinds        = []string{"ruleset", "moduleset"}
	ComplianceTeamRoles    = []string{"publication", "responsible"}
	ComplianceRulesetTypes = []string{"explicit", "contextual"}
	ComplianceOperations   = []string{
		"create", "rename", "clone", "delete",
		"set_type", "set_public", "set_filterset",
		"add_variable", "update_variable", "delete_variable", "copy_variable",
		"include", "detach",
		"add_module", "rename_module", "set_autofix", "delete_module",
		"add_team", "remove_team", "move_team",
	}
)

// Outcomes of a compliance change.
const (
	ComplianceChangeApplied   = "applied"
	ComplianceChangeUnchanged = "unchanged"
)

// complianceTmpPattern is a temporary reference, which an operation creating
// an object gives it for the next ones.
var complianceTmpPattern = regexp.MustCompile(`^tmp:[A-Za-z0-9_.-]{1,64}$`)

// ComplianceOperation is one operation of a compliance change: its op, the
// kind of the object it changes, the object by ref, and the fields of the op.
// The fields an op does not read are left out of what is sent.
type ComplianceOperation struct {
	Op            string
	Kind          string
	Ref           string
	As            string
	Name          *string
	RulesetType   string
	RulesetPublic *bool
	Filterset     *string
	Variable      string
	VarName       *string
	VarClass      *string
	VarValue      *string
	Dst           string
	Move          bool
	ChildKind     string
	Child         string
	Module        string
	Autofix       *bool
	Role          string
	ToRole        string
	Team          string
}

type ComplianceChanges struct {
	Provenance   Provenance                   `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Outcome      string                       `json:"outcome" jsonschema:"applied, all the operations at once, or unchanged when none had anything to do and nothing was sent"`
	Applied      int                          `json:"applied" jsonschema:"the number of operations the collector applied"`
	Version      *ComplianceVersionRecorded   `json:"version,omitempty" jsonschema:"the version of the compliance history recording the change"`
	VersionError string                       `json:"version_error,omitempty" jsonschema:"why no version recorded the change, which is saved all the same"`
	Created      []ComplianceObjectCreated    `json:"created,omitempty" jsonschema:"the objects created or cloned, by their temporary reference"`
	Unchanged    []ComplianceOperationNoop    `json:"unchanged_operations,omitempty" jsonschema:"the operations that had nothing to do, the link or the group they remove being absent"`
	Deleted      []string                     `json:"deleted,omitempty" jsonschema:"the objects deleted, as kind:name"`
	Rulesets     []ComplianceRulesetDetails   `json:"read_back_rulesets,omitempty" jsonschema:"the rulesets changed, read back after the change as get_compliance_ruleset gives them, their variables masked"`
	Modulesets   []ComplianceModulesetDetails `json:"read_back_modulesets,omitempty" jsonschema:"the modulesets changed, read back after the change as get_compliance_moduleset gives them"`
	Warning      string                       `json:"warning,omitempty" jsonschema:"the objects changed that were not read back, and why; get_compliance_ruleset and get_compliance_moduleset read them"`
}

type ComplianceVersionRecorded struct {
	Commit   string `json:"commit" jsonschema:"the commit of the version, which get_compliance_version reads"`
	Recorded bool   `json:"recorded" jsonschema:"false when the configuration was already as recorded and no version was added"`
}

type ComplianceObjectCreated struct {
	Ref  string `json:"ref" jsonschema:"the temporary reference the operation gave it"`
	Kind string `json:"kind" jsonschema:"ruleset or moduleset"`
	ID   int    `json:"id" jsonschema:"its collector ID"`
}

type ComplianceOperationNoop struct {
	Index     int    `json:"index" jsonschema:"the position of the operation in the list, from 0"`
	Operation string `json:"operation" jsonschema:"the operation, as op kind object"`
	Reason    string `json:"reason" jsonschema:"why it had nothing to do"`
}

// complianceObject is an object an operation names, resolved before the
// change when the caller sees it.
type complianceObject struct {
	kind string
	id   int
	name string
}

func (object complianceObject) key() string { return object.kind + ":" + strconv.Itoa(object.id) }

type complianceTargets struct {
	ref, child, dst *complianceObject
}

// sent is how a reference goes to the collector: by the ID of the object
// resolved, which a rename earlier in the list leaves valid, else as given.
func (object *complianceObject) sent(given string) string {
	if object == nil {
		return given
	}
	return strconv.Itoa(object.id)
}

// complianceChange is a change being prepared: the operations, the objects
// they name, resolved, and the temporary references they create.
type complianceChange struct {
	operations []ComplianceOperation
	// targets holds, by operation, the objects its ref, child and dst name,
	// resolved before the change; nil for a tmp: reference or a name an
	// earlier operation gives.
	targets []complianceTargets
	// tmp holds the kind of each temporary reference created.
	tmp map[string]string
	// sent holds the index in operations of each operation sent: those
	// with nothing to do are not, their absent group or child making the
	// collector refuse the whole list otherwise.
	sent []int
}

// ApplyComplianceChanges applies a list of operations to the compliance
// rulesets and modulesets at once, through a designer commit of the
// collector: all of them or none. The collector records a version of the
// compliance history with message, which the MCP starts with
// ComplianceChangeMark. The objects named by name or ID are resolved first
// among those the caller sees, and the list is checked for what the
// collector would take silently. The objects changed are read back.
func (s *Service) ApplyComplianceChanges(ctx context.Context, message string, operations []ComplianceOperation) (ComplianceChanges, error) {
	message = strings.TrimSpace(message)
	if message == "" {
		return ComplianceChanges{}, fmt.Errorf("message is required: it names the version recording the change, its first line a summary")
	}
	if len(ComplianceChangeMark)+len(message) > maxComplianceMessageBytes || !utf8.ValidString(message) {
		return ComplianceChanges{}, fmt.Errorf("message must be UTF-8 text of at most %d bytes", maxComplianceMessageBytes-len(ComplianceChangeMark))
	}
	if len(operations) == 0 || len(operations) > maxComplianceOperations {
		return ComplianceChanges{}, fmt.Errorf("pass 1 to %d operations; split a longer change into several, each confirmed", maxComplianceOperations)
	}
	change := &complianceChange{operations: operations, targets: make([]complianceTargets, len(operations)), tmp: map[string]string{}}
	if err := change.validate(); err != nil {
		return ComplianceChanges{}, err
	}
	if err := s.resolveComplianceObjects(ctx, change); err != nil {
		return ComplianceChanges{}, err
	}
	if err := change.checkDeleted(); err != nil {
		return ComplianceChanges{}, err
	}
	unchanged := s.complianceNoops(ctx, change)
	if len(unchanged) == len(operations) {
		return ComplianceChanges{Provenance: s.newProvenance(), Outcome: ComplianceChangeUnchanged, Unchanged: unchanged}, nil
	}
	noop := map[int]bool{}
	for _, operation := range unchanged {
		noop[operation.Index] = true
	}
	for index := range operations {
		if !noop[index] {
			change.sent = append(change.sent, index)
		}
	}
	body := map[string]any{"message": ComplianceChangeMark + message, "operations": change.payload()}
	var response struct {
		Applied int            `json:"applied"`
		IDs     map[string]int `json:"ids"`
		Version *struct {
			Commit  string `json:"commit"`
			Changed bool   `json:"changed"`
		} `json:"version"`
		VersionError string `json:"version_error"`
	}
	if err := s.client.PostJSON(ctx, "/compliance/designer/commits", body, &response); err != nil {
		return ComplianceChanges{}, change.refusal(err)
	}
	result := ComplianceChanges{Provenance: s.newProvenance(), Outcome: ComplianceChangeApplied, Applied: response.Applied, Unchanged: unchanged, VersionError: response.VersionError}
	if response.Version != nil {
		result.Version = &ComplianceVersionRecorded{Commit: response.Version.Commit, Recorded: response.Version.Changed}
	}
	for ref, id := range response.IDs {
		if kind, ok := change.tmp[ref]; ok {
			result.Created = append(result.Created, ComplianceObjectCreated{Ref: ref, Kind: kind, ID: id})
		}
	}
	slices.SortFunc(result.Created, func(a, b ComplianceObjectCreated) int { return strings.Compare(a.Ref, b.Ref) })
	s.readBackComplianceChange(ctx, change, response.IDs, &result)
	return result, nil
}

// complianceKindOnly names the operations of one kind of object only.
var complianceKindOnly = map[string]string{
	"set_type": "ruleset", "set_public": "ruleset", "set_filterset": "ruleset",
	"add_variable": "ruleset", "update_variable": "ruleset", "delete_variable": "ruleset", "copy_variable": "ruleset",
	"add_module": "moduleset", "rename_module": "moduleset", "set_autofix": "moduleset", "delete_module": "moduleset",
}

// validate checks each operation for the fields its op requires, and the
// temporary references: given only by the operations creating an object,
// each once, and named only after the operation creating it. The collector
// would take an "as" without the prefix, and a reference given to a
// ruleset and a moduleset at once, without telling.
func (change *complianceChange) validate() error {
	for index, operation := range change.operations {
		fail := func(format string, args ...any) error {
			return fmt.Errorf("operation %d (%s %s): %s; nothing was sent", index, operation.Op, operation.Kind, fmt.Sprintf(format, args...))
		}
		if !slices.Contains(ComplianceOperations, operation.Op) {
			return fail("op must be one of %s", strings.Join(ComplianceOperations, ", "))
		}
		if !slices.Contains(ComplianceKinds, operation.Kind) {
			return fail("kind must be ruleset or moduleset")
		}
		if only, ok := complianceKindOnly[operation.Op]; ok && only != operation.Kind {
			return fail("%s applies to a %s only", operation.Op, only)
		}
		// needs takes pairs of a field name and whether it is given, in
		// order, and refuses the first missing.
		needs := func(fields ...any) error {
			for i := 0; i+1 < len(fields); i += 2 {
				if !fields[i+1].(bool) {
					return fail("%s is required", fields[i])
				}
			}
			return nil
		}
		given := func(value *string) bool { return value != nil && strings.TrimSpace(*value) != "" }
		var err error
		switch operation.Op {
		case "create":
			err = needs("name", given(operation.Name), "as", operation.As != "")
		case "clone":
			err = needs("as", operation.As != "")
		case "rename", "add_module":
			err = needs("name", given(operation.Name))
		case "set_type":
			if !slices.Contains(ComplianceRulesetTypes, operation.RulesetType) {
				err = fail("ruleset_type must be explicit or contextual")
			}
		case "set_public":
			err = needs("ruleset_public", operation.RulesetPublic != nil)
		case "set_filterset":
			err = needs("filterset (empty to remove it)", operation.Filterset != nil)
		case "add_variable":
			err = needs("var_name", given(operation.VarName))
		case "update_variable":
			err = needs("variable", operation.Variable != "", "var_name, var_class or var_value", operation.VarName != nil || operation.VarClass != nil || operation.VarValue != nil)
			if err == nil && operation.VarName != nil && !given(operation.VarName) {
				err = fail("var_name must not be empty")
			}
		case "delete_variable":
			err = needs("variable", operation.Variable != "")
		case "copy_variable":
			err = needs("variable", operation.Variable != "", "dst", operation.Dst != "")
		case "include", "detach":
			err = needs("child", operation.Child != "")
			if err == nil && !slices.Contains(ComplianceKinds, operation.ChildKind) {
				err = fail("child_kind must be ruleset or moduleset")
			}
			if err == nil && operation.Kind == "ruleset" && operation.ChildKind != "ruleset" {
				err = fail("a ruleset includes rulesets only")
			}
		case "rename_module":
			err = needs("module", operation.Module != "", "name", given(operation.Name))
		case "set_autofix":
			err = needs("module", operation.Module != "", "autofix", operation.Autofix != nil)
		case "delete_module":
			err = needs("module", operation.Module != "")
		case "add_team", "remove_team", "move_team":
			err = needs("team", operation.Team != "")
			if err == nil && !slices.Contains(ComplianceTeamRoles, operation.Role) {
				err = fail("role must be publication or responsible")
			}
			if err == nil && operation.Op == "move_team" && (!slices.Contains(ComplianceTeamRoles, operation.ToRole) || operation.ToRole == operation.Role) {
				err = fail("to_role must be the other role, publication or responsible")
			}
		}
		if err != nil {
			return err
		}
		if operation.Op != "create" && operation.Ref == "" {
			return fail("ref is required: the object, by its exact name, its ID or the temporary reference an earlier operation gave it")
		}
		// The references, named after the operation creating them.
		for _, reference := range []struct{ kind, ref string }{{operation.Kind, operation.Ref}, {operation.ChildKind, operation.Child}, {"ruleset", operation.Dst}} {
			if !strings.HasPrefix(reference.ref, "tmp:") {
				continue
			}
			kind, ok := change.tmp[reference.ref]
			if !ok {
				return fail("%s is not created by an earlier operation", reference.ref)
			}
			if kind != reference.kind {
				return fail("%s is a %s, not a %s", reference.ref, kind, reference.kind)
			}
		}
		if operation.As != "" {
			if operation.Op != "create" && operation.Op != "clone" {
				return fail("as is given by create and clone only")
			}
			if !complianceTmpPattern.MatchString(operation.As) {
				return fail("as must be tmp: followed by 1 to 64 letters, digits, dots, dashes or underscores, such as tmp:web")
			}
			if _, taken := change.tmp[operation.As]; taken {
				return fail("%s is given by an earlier operation already: give each object created its own", operation.As)
			}
			change.tmp[operation.As] = operation.Kind
		}
	}
	return nil
}

// resolveComplianceObjects reads, among the objects the caller sees, those
// the operations name by name or ID: a clone of an object the caller does
// not see is refused here, the collector cloning it otherwise. A name an
// earlier operation of the list gives an object, by create, rename or
// clone, is left to the collector, which resolves it in the change; the
// same name before it is resolved.
func (s *Service) resolveComplianceObjects(ctx context.Context, change *complianceChange) error {
	given := map[string]bool{}
	cache := map[string]*complianceObject{}
	for index, operation := range change.operations {
		targets := &change.targets[index]
		for _, reference := range []struct {
			kind, ref string
			target    **complianceObject
		}{{operation.Kind, operation.Ref, &targets.ref}, {operation.ChildKind, operation.Child, &targets.child}, {"ruleset", operation.Dst, &targets.dst}} {
			key := reference.kind + ":" + reference.ref
			if reference.ref == "" || strings.HasPrefix(reference.ref, "tmp:") || given[key] {
				continue
			}
			if object, done := cache[key]; done {
				*reference.target = object
				continue
			}
			object, err := s.lookupComplianceObject(ctx, reference.kind, reference.ref)
			if err != nil {
				return fmt.Errorf("operation %d (%s %s %s): %w; nothing was sent", index, operation.Op, operation.Kind, operation.Ref, err)
			}
			cache[key] = &object
			*reference.target = &object
		}
		if operation.Name != nil && (operation.Op == "create" || operation.Op == "rename" || operation.Op == "clone") {
			given[operation.Kind+":"+strings.TrimSpace(*operation.Name)] = true
		}
		// A clone without name is named after its source, as the collector
		// names it.
		if operation.Op == "clone" && operation.Name == nil && targets.ref != nil {
			given[operation.Kind+":"+targets.ref.name+"_clone"] = true
		}
	}
	return nil
}

// checkDeleted refuses an operation naming an object a delete earlier in
// the list removed, which the collector would refuse, and the whole list
// with it: but the detach of a child deleted, which has nothing to do.
func (change *complianceChange) checkDeleted() error {
	deleted := map[string]bool{}
	for index, operation := range change.operations {
		targets := change.targets[index]
		for _, object := range []*complianceObject{targets.ref, targets.child, targets.dst} {
			if object == nil || !deleted[object.key()] || (operation.Op == "detach" && object == targets.child) {
				continue
			}
			return fmt.Errorf("operation %d (%s): the %s %s is deleted earlier in the list; nothing was sent", index, describeComplianceOperation(operation), object.kind, object.name)
		}
		if operation.Op == "delete" && targets.ref != nil {
			deleted[targets.ref.key()] = true
		}
	}
	return nil
}

func (s *Service) lookupComplianceObject(ctx context.Context, kind, ref string) (complianceObject, error) {
	if err := checkComplianceName(kind, ref); err != nil {
		return complianceObject{}, err
	}
	if kind == "ruleset" {
		found, err := s.lookupRuleset(ctx, ref)
		return complianceObject{kind: kind, id: found.ID, name: found.Name}, err
	}
	found, err := s.lookupModuleset(ctx, ref)
	return complianceObject{kind: kind, id: found.ID, name: found.Name}, err
}

// complianceNoops tells the operations on the links and the groups of an
// object with nothing to do: an include or an add_team of what is there, a
// detach or a remove_team of what is not, which the collector answers as
// done, or refuses for a link it holds already or a group it does not know,
// making it refuse the whole list. They are not sent. The links and the
// groups of each object are read from its export once, then changed in
// memory by the operations of the list, in their order: each operation is
// judged on what the ones before it leave. An export that cannot be read,
// a child or a group it cannot tell, created by the list or given by ID,
// leave the operation to the collector.
func (s *Service) complianceNoops(ctx context.Context, change *complianceChange) []ComplianceOperationNoop {
	var noops []ComplianceOperationNoop
	objects := map[string]*complianceObjectLinks{}
	deleted := map[string]bool{}
	for index, operation := range change.operations {
		object := change.targets[index].ref
		if object == nil {
			continue
		}
		key := object.key()
		if operation.Op == "delete" {
			deleted[key] = true
			continue
		}
		switch operation.Op {
		case "include", "detach", "add_team", "remove_team", "move_team":
		default:
			continue
		}
		if deleted[key] {
			continue
		}
		links, ok := objects[key]
		if !ok {
			if read, err := s.complianceLinks(ctx, *object); err == nil {
				links = &read
			}
			// nil when the export cannot be read: not read again, the
			// operations on the object left to the collector.
			objects[key] = links
		}
		if links == nil {
			continue
		}
		if reason := links.apply(operation, change.targets[index].child, deleted); reason != "" {
			noops = append(noops, ComplianceOperationNoop{Index: index, Operation: describeComplianceOperation(operation), Reason: reason})
		}
	}
	return noops
}

// complianceObjectLinks are the direct links and the groups of an object, as
// its export gives them, by name.
type complianceObjectLinks struct {
	rulesets, modulesets, publications, responsibles []string
}

func (s *Service) complianceLinks(ctx context.Context, object complianceObject) (complianceObjectLinks, error) {
	var export struct {
		Rulesets []struct {
			ID           int      `json:"id"`
			Rulesets     []string `json:"rulesets"`
			Publications []string `json:"publications"`
			Responsibles []string `json:"responsibles"`
		} `json:"rulesets"`
		Modulesets []struct {
			ID           int      `json:"id"`
			Modulesets   []string `json:"modulesets"`
			Rulesets     []string `json:"rulesets"`
			Publications []string `json:"publications"`
			Responsibles []string `json:"responsibles"`
		} `json:"modulesets"`
	}
	path := "/compliance/" + object.kind + "s/" + strconv.Itoa(object.id) + "/export"
	if err := s.client.GetJSON(ctx, path, url.Values{}, &export); err != nil {
		return complianceObjectLinks{}, fmt.Errorf("read %s %s: %w", object.kind, object.name, err)
	}
	if object.kind == "ruleset" {
		for _, entry := range export.Rulesets {
			if entry.ID == object.id {
				return complianceObjectLinks{rulesets: entry.Rulesets, publications: entry.Publications, responsibles: entry.Responsibles}, nil
			}
		}
	} else {
		for _, entry := range export.Modulesets {
			if entry.ID == object.id {
				return complianceObjectLinks{rulesets: entry.Rulesets, modulesets: entry.Modulesets, publications: entry.Publications, responsibles: entry.Responsibles}, nil
			}
		}
	}
	return complianceObjectLinks{}, fmt.Errorf("read %s %s: not in its export", object.kind, object.name)
}

// apply makes an operation on the links or the groups, and tells why it
// has nothing to do, empty when it has, or when they cannot tell: a group
// given by its ID, a child created or named by the list. A child deleted
// earlier in the list has lost its links.
func (links *complianceObjectLinks) apply(operation ComplianceOperation, child *complianceObject, deleted map[string]bool) string {
	switch operation.Op {
	case "add_team", "remove_team", "move_team":
		if _, err := strconv.Atoi(operation.Team); err == nil {
			return ""
		}
		groups := map[string]*[]string{"publication": &links.publications, "responsible": &links.responsibles}
		from := groups[operation.Role]
		switch operation.Op {
		case "add_team":
			if slices.Contains(*from, operation.Team) {
				return fmt.Sprintf("the group %s is already a %s of the %s", operation.Team, operation.Role, operation.Kind)
			}
			*from = append(*from, operation.Team)
		case "remove_team":
			if !slices.Contains(*from, operation.Team) {
				return fmt.Sprintf("the group %s is not a %s of the %s", operation.Team, operation.Role, operation.Kind)
			}
			*from = slices.DeleteFunc(*from, func(group string) bool { return group == operation.Team })
		default:
			to := groups[operation.ToRole]
			if !slices.Contains(*from, operation.Team) && slices.Contains(*to, operation.Team) {
				return fmt.Sprintf("the group %s is already a %s, and not a %s, of the %s", operation.Team, operation.ToRole, operation.Role, operation.Kind)
			}
			*from = slices.DeleteFunc(*from, func(group string) bool { return group == operation.Team })
			if !slices.Contains(*to, operation.Team) {
				*to = append(*to, operation.Team)
			}
		}
		return ""
	}
	if child == nil {
		return ""
	}
	children := &links.rulesets
	if operation.ChildKind == "moduleset" {
		children = &links.modulesets
	}
	linked := slices.Contains(*children, child.name)
	if deleted[child.key()] {
		if operation.Op == "detach" {
			return fmt.Sprintf("the %s %s is deleted earlier in the list, with its links", child.kind, child.name)
		}
		return ""
	}
	if operation.Op == "include" {
		if linked {
			return fmt.Sprintf("the %s %s is already linked to the %s", child.kind, child.name, operation.Kind)
		}
		*children = append(*children, child.name)
		return ""
	}
	if !linked {
		return fmt.Sprintf("the %s %s is not linked to the %s", child.kind, child.name, operation.Kind)
	}
	*children = slices.DeleteFunc(*children, func(name string) bool { return name == child.name })
	return ""
}

// payload is the operations as the collector takes them, each with the
// fields of its op.
func (change *complianceChange) payload() []map[string]any {
	out := make([]map[string]any, 0, len(change.sent))
	for _, index := range change.sent {
		operation, targets := change.operations[index], change.targets[index]
		entry := map[string]any{"op": operation.Op, "kind": operation.Kind}
		set := func(key, value string) {
			if value != "" {
				entry[key] = value
			}
		}
		set("ref", targets.ref.sent(operation.Ref))
		set("as", operation.As)
		if operation.Name != nil {
			entry["name"] = strings.TrimSpace(*operation.Name)
		}
		switch operation.Op {
		case "set_type":
			entry["ruleset_type"] = operation.RulesetType
		case "set_public":
			entry["ruleset_public"] = *operation.RulesetPublic
		case "set_filterset":
			entry["filterset"] = *operation.Filterset
		case "add_variable", "update_variable":
			set("variable", operation.Variable)
			for key, value := range map[string]*string{"var_name": operation.VarName, "var_class": operation.VarClass, "var_value": operation.VarValue} {
				if value != nil {
					entry[key] = *value
				}
			}
		case "delete_variable":
			set("variable", operation.Variable)
		case "copy_variable":
			set("variable", operation.Variable)
			set("dst", targets.dst.sent(operation.Dst))
			entry["move"] = operation.Move
		case "include", "detach":
			entry["child_kind"] = operation.ChildKind
			set("child", targets.child.sent(operation.Child))
		case "rename_module", "delete_module":
			set("module", operation.Module)
		case "set_autofix":
			set("module", operation.Module)
			entry["autofix"] = *operation.Autofix
		case "add_team", "remove_team":
			entry["role"] = operation.Role
			set("team", operation.Team)
		case "move_team":
			entry["role"] = operation.Role
			entry["to_role"] = operation.ToRole
			set("team", operation.Team)
		}
		out = append(out, entry)
	}
	return out
}

// refusal is the error of a change the collector refused, which it rolls
// back whole: the operation it names is described, for the model to fix it
// and for the person reading the steps to see which.
func (change *complianceChange) refusal(err error) error {
	var apiError *client.APIError
	if !errors.As(err, &apiError) {
		return fmt.Errorf("apply the compliance changes: %w; whether they were applied is unknown, get_compliance_ruleset and get_compliance_moduleset read the objects", err)
	}
	if apiError.Index != nil && *apiError.Index < len(change.sent) {
		index := change.sent[*apiError.Index]
		return fmt.Errorf("operation %d (%s), refused by the collector with HTTP %d: %s; nothing was applied, fix this operation and pass the list again", index, describeComplianceOperation(change.operations[index]), apiError.StatusCode, apiError.Text)
	}
	return fmt.Errorf("the collector refused the compliance changes with HTTP %d: %s; nothing was applied", apiError.StatusCode, apiError.Text)
}

// describeComplianceOperation is an operation in a few words: its op, its
// kind and object, and what it names.
func describeComplianceOperation(operation ComplianceOperation) string {
	words := []string{operation.Op, operation.Kind}
	if operation.Ref != "" {
		words = append(words, operation.Ref)
	}
	if operation.Name != nil {
		words = append(words, "name "+strings.TrimSpace(*operation.Name))
	}
	for _, field := range [][2]string{{"variable", operation.Variable}, {"child", operation.Child}, {"module", operation.Module}, {"team", operation.Team}, {"dst", operation.Dst}} {
		if field[1] != "" {
			words = append(words, field[0]+" "+field[1])
		}
	}
	if operation.VarName != nil {
		words = append(words, "var_name "+*operation.VarName)
	}
	return strings.Join(words, " ")
}

// readBackComplianceChange reads the objects the operations changed, at
// most maxComplianceReadBack, as get_compliance_ruleset and
// get_compliance_moduleset give them; the deleted ones are named.
func (s *Service) readBackComplianceChange(ctx context.Context, change *complianceChange, ids map[string]int, result *ComplianceChanges) {
	type target struct {
		kind, ref string
	}
	var targets []target
	seen := map[string]bool{}
	add := func(kind, ref string) {
		key := kind + ":" + ref
		if ref == "" || seen[key] {
			return
		}
		seen[key] = true
		targets = append(targets, target{kind, ref})
	}
	deleted := map[string]bool{}
	for index, operation := range change.operations {
		targets := change.targets[index]
		ref := operation.Ref
		if operation.Op == "create" {
			ref = operation.As
		}
		if targets.ref != nil {
			ref = strconv.Itoa(targets.ref.id)
		} else if id, ok := ids[ref]; ok {
			ref = strconv.Itoa(id)
		}
		if operation.Op == "clone" {
			if id, ok := ids[operation.As]; ok {
				add(operation.Kind, strconv.Itoa(id))
			}
			continue
		}
		if operation.Op == "delete" {
			deleted[operation.Kind+":"+ref] = true
			name := operation.Ref
			if targets.ref != nil {
				name = targets.ref.name
			}
			result.Deleted = append(result.Deleted, operation.Kind+":"+name)
			continue
		}
		add(operation.Kind, ref)
		if operation.Op == "copy_variable" {
			dst := operation.Dst
			if targets.dst != nil {
				dst = strconv.Itoa(targets.dst.id)
			} else if id, ok := ids[dst]; ok {
				dst = strconv.Itoa(id)
			}
			add("ruleset", dst)
		}
	}
	var skipped, failed []string
	read := 0
	for _, target := range targets {
		if deleted[target.kind+":"+target.ref] {
			continue
		}
		if read == maxComplianceReadBack {
			skipped = append(skipped, target.kind+":"+target.ref)
			continue
		}
		read++
		var err error
		if target.kind == "ruleset" {
			var details ComplianceRulesetDetails
			if details, err = s.GetComplianceRuleset(ctx, target.ref); err == nil {
				result.Rulesets = append(result.Rulesets, details)
			}
		} else {
			var details ComplianceModulesetDetails
			if details, err = s.GetComplianceModuleset(ctx, target.ref); err == nil {
				result.Modulesets = append(result.Modulesets, details)
			}
		}
		if err != nil {
			failed = append(failed, target.kind+":"+target.ref)
		}
	}
	var warnings []string
	if len(failed) > 0 {
		warnings = append(warnings, "not read back after the change, which is made: "+strings.Join(failed, ", "))
	}
	if len(skipped) > 0 {
		warnings = append(warnings, fmt.Sprintf("%d more objects changed, not read back beyond %d: %s", len(skipped), maxComplianceReadBack, strings.Join(skipped, ", ")))
	}
	if len(warnings) > 0 {
		result.Warning = strings.Join(warnings, "; ") + "; get_compliance_ruleset and get_compliance_moduleset read them by ID"
	}
}
