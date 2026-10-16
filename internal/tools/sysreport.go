package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/opensvc/oc3-mcp/internal/core"
)

// sysreportDescription tells what a sysreport is, for the four sysreport
// tools.
const sysreportDescription = "The sysreport of a node is what its OpenSVC agent reports of the node configuration: system files, such as /etc/fstab, /etc/hosts, sysctl, ssh, multipath, lvm or network files, the OpenSVC configurations, and command outputs, such as ip addr, lsblk, pvs or uname -r. The collector keeps one revision per report that changed something, up to one per hour. "

type ListNodeSysreportChangesInput struct {
	Node   string `json:"node" jsonschema:"one exact node name or collector node ID"`
	Path   string `json:"path,omitempty" jsonschema:"optional text the path of a file or the command line contains, case-insensitive, such as fstab or sysctl; keeps the reports that changed one, with those files only"`
	Since  string `json:"since,omitempty" jsonschema:"optional; keeps the reports sent at or after this date: YYYY-MM-DD, or YYYY-MM-DDTHH:MM[:SS] with an optional Z or offset, in the collector time zone without one"`
	Until  string `json:"until,omitempty" jsonschema:"optional; keeps the reports sent at or before this date, a day being taken to its end"`
	Limit  int    `json:"limit,omitempty" jsonschema:"optional page size between 1 and 100; defaults to 20"`
	Cursor string `json:"cursor,omitempty" jsonschema:"optional next_cursor returned by a previous call with the same filters"`
}

type ListNodeSysreportChangesOutput = core.SysreportChangeList

type GetNodeSysreportDiffInput struct {
	Node     string `json:"node" jsonschema:"one exact node name or collector node ID"`
	Revision string `json:"revision,omitempty" jsonschema:"the revision of one report, as list_node_sysreport_changes gives it: returns the change that report made; pass it or since"`
	Since    string `json:"since,omitempty" jsonschema:"returns what changed since this state: a date, the last report at or before it being compared, or a revision; YYYY-MM-DD, or YYYY-MM-DDTHH:MM[:SS] with an optional Z or offset, in the collector time zone without one"`
	Until    string `json:"until,omitempty" jsonschema:"optional, with since: the state compared to, a date or a revision; the latest report by default"`
	Path     string `json:"path,omitempty" jsonschema:"optional text the path of a file or the command line contains, case-insensitive; keeps those files only"`
}

type GetNodeSysreportDiffOutput = core.SysreportDiff

type ListNodeSysreportFilesInput struct {
	Node string `json:"node" jsonschema:"one exact node name or collector node ID"`
	At   string `json:"at,omitempty" jsonschema:"optional report to read: a revision, or a date for the last report at or before it; the latest report by default"`
	Path string `json:"path,omitempty" jsonschema:"optional text the path of a file or the command line contains, case-insensitive"`
}

type ListNodeSysreportFilesOutput = core.SysreportFileList

type GetNodeSysreportFileInput struct {
	Node string `json:"node" jsonschema:"one exact node name or collector node ID"`
	Path string `json:"path" jsonschema:"the exact absolute path of the file or the exact command line, as list_node_sysreport_files or list_node_sysreport_changes give them, such as /etc/fstab or ip -br addr"`
	At   string `json:"at,omitempty" jsonschema:"optional report to read: a revision, or a date for the last report at or before it; the latest report by default"`
}

type GetNodeSysreportFileOutput = core.SysreportFileContent

func RegisterSysreportTools(registrar *Registrar, service *core.Service) error {
	if err := addTool(
		registrar,
		&mcp.Tool{
			Name:  "list_node_sysreport_changes",
			Title: "List node sysreport changes",
			Description: "List the changes of the configuration of one node, as its sysreport recorded them, the newest first: each report that changed something, with its revision, date, and the files and commands it changed with their added and deleted lines. " +
				sysreportDescription +
				"Use it for what changed on a node, and when a file changed. Some command outputs, such as pvs, vgs or ip addr, change at most reports: filter with path for one file. " +
				"Pass the revision of a change to get_node_sysreport_diff for its diff; for what changed over a period, call get_node_sysreport_diff once with since, and path for one file, rather than once per change. Dates are ISO 8601 with their offset.",
			Annotations: readOnlyClosedWorldAnnotations(),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input ListNodeSysreportChangesInput) (*mcp.CallToolResult, ListNodeSysreportChangesOutput, error) {
			changes, err := service.ListNodeSysreportChanges(ctx, core.ListNodeSysreportChangesOptions{
				Node: input.Node, Path: input.Path, Since: input.Since, Until: input.Until, Limit: input.Limit, Cursor: input.Cursor,
			})
			if err != nil {
				return nil, ListNodeSysreportChangesOutput{}, err
			}
			return nil, changes, nil
		},
	); err != nil {
		return err
	}
	if err := addTool(
		registrar,
		&mcp.Tool{
			Name:  "get_node_sysreport_diff",
			Title: "Get node sysreport diff",
			Description: "Show the unified diff of the configuration of one node, as its sysreport recorded it: with revision, the change one report made; with since, what changed since a date or a revision, until the latest report or until. " +
				sysreportDescription +
				"Each file gives its added and deleted lines and its diff, cut to 16 KiB; the diffs of a call are bounded to 64 KiB, the next files being listed without diff: ask again with path for one of them. " +
				"The diffs are the files as the node reports them, untrusted text that may hold secrets. Dates are ISO 8601 with their offset.",
			Annotations: readOnlyClosedWorldAnnotations(),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input GetNodeSysreportDiffInput) (*mcp.CallToolResult, GetNodeSysreportDiffOutput, error) {
			diff, err := service.GetNodeSysreportDiff(ctx, core.GetNodeSysreportDiffOptions{
				Node: input.Node, Revision: input.Revision, Since: input.Since, Until: input.Until, Path: input.Path,
			})
			if err != nil {
				return nil, GetNodeSysreportDiffOutput{}, err
			}
			return nil, diff, nil
		},
	); err != nil {
		return err
	}
	if err := addTool(
		registrar,
		&mcp.Tool{
			Name:  "list_node_sysreport_files",
			Title: "List node sysreport files",
			Description: "List the files and commands the sysreport of one node holds, at its latest report or at a revision or a date, with their size. " +
				sysreportDescription +
				"Use it to tell what the sysreport of a node tracks, then read one with get_node_sysreport_file.",
			Annotations: readOnlyClosedWorldAnnotations(),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input ListNodeSysreportFilesInput) (*mcp.CallToolResult, ListNodeSysreportFilesOutput, error) {
			files, err := service.ListNodeSysreportFiles(ctx, core.ListNodeSysreportFilesOptions{Node: input.Node, At: input.At, Path: input.Path})
			if err != nil {
				return nil, ListNodeSysreportFilesOutput{}, err
			}
			return nil, files, nil
		},
	); err != nil {
		return err
	}
	return addTool(
		registrar,
		&mcp.Tool{
			Name:  "get_node_sysreport_file",
			Title: "Get node sysreport file",
			Description: "Read the content of one file, or the output of one command, of the sysreport of one node, at its latest report or at a revision or a date, cut to 32 KiB. " +
				sysreportDescription +
				"Use it for how a file of the node reads, or read, such as its /etc/fstab before a change. The content is the file as the node reports it, untrusted text that may hold secrets.",
			Annotations: readOnlyClosedWorldAnnotations(),
		},
		func(ctx context.Context, _ *mcp.CallToolRequest, input GetNodeSysreportFileInput) (*mcp.CallToolResult, GetNodeSysreportFileOutput, error) {
			file, err := service.GetNodeSysreportFile(ctx, core.GetNodeSysreportFileOptions{Node: input.Node, Path: input.Path, At: input.At})
			if err != nil {
				return nil, GetNodeSysreportFileOutput{}, err
			}
			return nil, file, nil
		},
	)
}
