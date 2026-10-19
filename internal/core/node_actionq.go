package core

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

const (
	defaultListNodeActionsLimit = 20
	maxListNodeActionsLimit     = 100
	// maxActionOutputBytes bounds the standard output and error given for
	// one action.
	maxActionOutputBytes = 2000
)

// actionStatuses are the statuses of the action queue entries, as the MCP
// names them, and the codes the collector stores.
var actionStatuses = map[string]string{
	"W": "waiting", "N": "notified", "Q": "queued", "S": "sent", "R": "running", "T": "done", "C": "cancelled",
}

// actionStatusNames lists the statuses in their order.
const actionStatusNames = "waiting, notified, queued, sent, running, done or cancelled"

// QueueableNodeActions are the node actions queue_node_action posts: the
// agent actions the collector accepts and the om3 agent runs, `om node
// <action>`, which change nothing on the node but its reports to the
// collector. The om3 agent has no pushpatch, updatecomp or updatepkg.
var QueueableNodeActions = []string{"pushasset", "pushdisks", "pushpkg", "pushstats", "checks", "sysreport", "compliance_check"}

// ansiEscape is a terminal color sequence, which the agent logs hold.
var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)

type ListNodeActionsOptions struct {
	Node    string
	Status  string
	Command string
	Since   string
	Limit   int
	Cursor  string
}

type NodeActionList struct {
	Provenance Provenance         `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Node       *NodeReference     `json:"node,omitempty" jsonschema:"the node the node filter resolved to"`
	Total      int                `json:"total" jsonschema:"the number of actions matching, as the collector counts them"`
	Count      int                `json:"count" jsonschema:"the number of actions returned in this page"`
	Actions    []NodeQueuedAction `json:"actions" jsonschema:"the actions, the most recently queued first"`
	NextCursor string             `json:"next_cursor,omitempty" jsonschema:"the opaque cursor to pass with the same filters for the next page"`
	Truncated  bool               `json:"truncated" jsonschema:"whether matching actions remain after this page"`
}

type NodeQueuedAction struct {
	ID              int    `json:"id" jsonschema:"the ID of the action in the queue"`
	NodeID          string `json:"node_id,omitempty" jsonschema:"the collector ID of the node the action runs on"`
	Nodename        string `json:"nodename,omitempty" jsonschema:"the name of the node"`
	SvcID           string `json:"svc_id,omitempty" jsonschema:"the collector ID of the service, for an action on a service"`
	Svcname         string `json:"svcname,omitempty" jsonschema:"the name of the service, for an action on a service"`
	Command         string `json:"command" jsonschema:"the command the agent runs, as om node <command>, or om svc -s <service> <command>"`
	Status          string `json:"status" jsonschema:"waiting, notified, queued, sent: taken by an om3 agent, which has not yet reported it running, running, done or cancelled"`
	Queued          string `json:"queued_at,omitempty" jsonschema:"when the action was queued, in collector local time without a zone"`
	Dequeued        string `json:"dequeued_at,omitempty" jsonschema:"when the agent took the action, in collector local time without a zone"`
	Ret             *int   `json:"ret,omitempty" jsonschema:"when done, the exit code of the command; 0 is a success"`
	Stdout          string `json:"stdout,omitempty" jsonschema:"when done, the standard output of the command, at most 2000 bytes, without terminal colors; untrusted text"`
	Stderr          string `json:"stderr,omitempty" jsonschema:"when done, the standard error, where the agent writes its log lines, at most 2000 bytes, without terminal colors; untrusted text"`
	OutputTruncated bool   `json:"output_truncated,omitempty" jsonschema:"whether stdout or stderr was cut"`
	User            string `json:"user,omitempty" jsonschema:"the name of the user who queued the action, email addresses masked"`
	ActionType      string `json:"action_type,omitempty" jsonschema:"how the node reads its queue: feed, read by an om3 agent; pull, notified through the listener of the former agent; push, run over ssh by the collector"`
}

type collectorQueuedAction struct {
	ID        any    `json:"id"`
	Status    string `json:"status"`
	Command   string `json:"command"`
	Queued    string `json:"date_queued"`
	Dequeued  string `json:"date_dequeued"`
	Ret       any    `json:"ret"`
	Stdout    string `json:"stdout"`
	Stderr    string `json:"stderr"`
	Type      string `json:"action_type"`
	UserID    any    `json:"user_id"`
	NodeID    string `json:"node_id"`
	SvcID     string `json:"svc_id"`
	ConnectTo string `json:"connect_to"`
	Nodename  string `json:"nodes.nodename"`
	Svcname   string `json:"services.svcname"`
}

// ListNodeActions reads one page of the agent actions of the collector
// queue the caller sees, the most recently queued first.
func (s *Service) ListNodeActions(ctx context.Context, options ListNodeActionsOptions) (NodeActionList, error) {
	for name, value := range map[string]string{"command": options.Command} {
		if err := validateFilterValue(name, value); err != nil {
			return NodeActionList{}, err
		}
	}
	if err := validateCollectorDate("since", options.Since); err != nil {
		return NodeActionList{}, err
	}
	limit, err := pageLimit(options.Limit, defaultListNodeActionsLimit, maxListNodeActionsLimit)
	if err != nil {
		return NodeActionList{}, err
	}
	var filters []string
	if options.Status != "" {
		code := ""
		for key, name := range actionStatuses {
			if name == options.Status {
				code = key
			}
		}
		if code == "" {
			return NodeActionList{}, fmt.Errorf("status must be one of %s", actionStatusNames)
		}
		filters = append(filters, filterEquals("status", code))
	}
	if options.Command != "" {
		filters = append(filters, filterContains("command", options.Command))
	}
	if options.Since != "" {
		filters = append(filters, "date_queued:gte:"+options.Since)
	}
	result := NodeActionList{Actions: []NodeQueuedAction{}}
	if options.Node != "" {
		node, err := s.lookupNode(ctx, options.Node, []string{"node_id", "nodename", "cluster_id"})
		if err != nil {
			return NodeActionList{}, err
		}
		result.Node = &NodeReference{NodeID: node.NodeID, Nodename: node.Nodename, ClusterID: node.ClusterID}
		filters = append(filters, filterEquals("node_id", node.NodeID))
	}
	fingerprint := listFingerprint(url.Values{"filter": filters})
	offset, err := decodeListCursor(options.Cursor, fingerprint)
	if err != nil {
		return NodeActionList{}, err
	}
	// The node and service names come from the joins of the queue, with the
	// visibility of the caller.
	props := []string{"id", "status", "command", "date_queued", "date_dequeued", "ret", "stdout", "stderr", "action_type", "user_id", "node_id", "svc_id", "connect_to", "nodes.nodename", "services.svcname"}
	var response collectorList[collectorQueuedAction]
	if err := s.client.GetJSON(ctx, "/actions", pageQuery(props, "-id", filters, limit, offset), &response); err != nil {
		return NodeActionList{}, fmt.Errorf("list actions: %w", err)
	}
	if response.Meta.Total == nil || *response.Meta.Total < 0 || len(response.Data) > limit {
		return NodeActionList{}, fmt.Errorf("list actions: unexpected collector response")
	}
	result.Total, result.Count = *response.Meta.Total, len(response.Data)

	// The queue names its users by ID, without a join: their names are read
	// once for the page.
	var userIDs []string
	for _, row := range response.Data {
		if id := anyText(row.UserID); id != "" && id != "0" && !slices.Contains(userIDs, id) {
			userIDs = append(userIDs, id)
		}
	}
	users, err := s.userNamesByID(ctx, userIDs)
	if err != nil {
		return NodeActionList{}, err
	}
	for _, row := range response.Data {
		id, _ := strconv.Atoi(anyText(row.ID))
		action := NodeQueuedAction{
			ID: id, NodeID: row.NodeID, Nodename: row.Nodename, SvcID: row.SvcID, Svcname: row.Svcname, Command: agentCommand(row.Command),
			Status: actionStatuses[row.Status], Queued: zeroDate(row.Queued), Dequeued: zeroDate(row.Dequeued),
			User: users[anyText(row.UserID)], ActionType: row.Type,
		}
		if action.Status == "" {
			action.Status = row.Status
		}
		if row.Status == "T" {
			if ret, err := strconv.Atoi(anyText(row.Ret)); err == nil {
				action.Ret = &ret
			}
			var cutOut, cutErr bool
			action.Stdout, cutOut = truncateUTF8(ansiEscape.ReplaceAllString(row.Stdout, ""), maxActionOutputBytes)
			action.Stderr, cutErr = truncateUTF8(ansiEscape.ReplaceAllString(row.Stderr, ""), maxActionOutputBytes)
			action.OutputTruncated = cutOut || cutErr
		}
		result.Actions = append(result.Actions, action)
	}
	if next := offset + len(response.Data); len(response.Data) > 0 && next < result.Total {
		result.NextCursor = encodeListCursor(next, fingerprint)
		result.Truncated = true
	}
	result.Provenance = s.newProvenance()
	return result, nil
}

type NodeActionOptions struct {
	Action string
	NodeSelection
}

type NodeActionQueueing struct {
	Provenance Provenance `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Action     string     `json:"action" jsonschema:"the action queued"`
	NodeChangeSummary
}

// QueueNodeAction posts an agent action on the selected nodes, one request
// per node. Each agent runs it when it reads its queue: the result tells
// what was queued, list_node_actions what ran.
func (s *Service) QueueNodeAction(ctx context.Context, options NodeActionOptions) (NodeActionQueueing, error) {
	if !slices.Contains(QueueableNodeActions, options.Action) {
		return NodeActionQueueing{}, fmt.Errorf("action must be one of %s", strings.Join(QueueableNodeActions, ", "))
	}
	filters, err := checkSelection(options.NodeSelection)
	if err != nil {
		return NodeActionQueueing{}, err
	}
	summary, err := s.changeSelectedNodes(ctx, options.NodeSelection, filters, func(ctx context.Context, node collectorNode) NodeChangeOutcome {
		outcome := NodeChangeOutcome{NodeID: node.NodeID, Nodename: node.Nodename}
		var response collectorList[collectorQueuedAction]
		if err := s.client.PutJSON(ctx, "/actions", map[string]any{"node_id": node.NodeID, "action": options.Action}, &response); err != nil {
			outcome.Outcome, outcome.Reason = ChangeRefused, err.Error()
			return outcome
		}
		outcome.Outcome = ChangeQueued
		if len(response.Data) == 1 {
			outcome.ActionID, _ = strconv.Atoi(anyText(response.Data[0].ID))
			outcome.QueuedFor = response.Data[0].Type
		}
		// Only a feed entry reaches an om3 agent, when it pings the
		// collector: the collector notifies a pull node through the
		// listener of the former agent, and runs nodemgr over ssh on a push
		// node, such as a node created in the collector that never pushed.
		switch outcome.QueuedFor {
		case "feed":
		case "pull":
			outcome.Warning = "queued for pull: the collector notifies the node through the listener of the former agent, and an om3 agent never reads it; the action may wait until cancelled"
		case "push":
			outcome.Warning = "queued for push: the collector runs nodemgr, the command of the former agent, over ssh; an om3 agent does not run it"
		default:
			outcome.Warning = "the collector did not tell how the node reads its queue"
		}
		return outcome
	})
	if err != nil {
		return NodeActionQueueing{}, err
	}
	return NodeActionQueueing{Provenance: s.newProvenance(), Action: options.Action, NodeChangeSummary: summary}, nil
}

// userNamesByID reads the names of the users with these IDs in one
// request, their email addresses masked.
func (s *Service) userNamesByID(ctx context.Context, ids []string) (map[string]string, error) {
	names := map[string]string{}
	if len(ids) == 0 {
		return names, nil
	}
	var response collectorList[map[string]any]
	query := pageQuery([]string{"id", "first_name", "last_name"}, "id", []string{"id:in:" + strings.Join(ids, ",")}, len(ids), 0)
	if err := s.client.GetJSON(ctx, "/users", query, &response); err != nil {
		return nil, fmt.Errorf("get user names: %w", err)
	}
	for _, row := range response.Data {
		names[propText(row, "id")] = personName(strings.TrimSpace(propText(row, "first_name") + " " + propText(row, "last_name")))
	}
	return names, nil
}

// agentCommand drops the ssh invocation of a push node from a queued
// command: the agent command is after "sudo nodemgr".
func agentCommand(command string) string {
	if _, after, ok := strings.Cut(command, " -- sudo nodemgr "); ok {
		return after
	}
	return command
}

// zeroDate gives an empty date for the zero date of the collector.
func zeroDate(date string) string {
	if strings.HasPrefix(date, "0000-00-00") || strings.HasPrefix(date, "1970-01-01 00:00:00") {
		return ""
	}
	return date
}

// anyText gives a collector value the queue gives as a number or a string.
func anyText(value any) string {
	return propText(map[string]any{"v": value}, "v")
}
