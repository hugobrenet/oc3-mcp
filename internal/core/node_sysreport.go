package core

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

const (
	defaultSysreportChangesLimit = 20
	maxSysreportChangesLimit     = 100
	// maxSysreportChangeFiles bounds the files listed for one change: the
	// first report of a node adds every file it tracks.
	maxSysreportChangeFiles = 50
	// maxSysreportDiffFiles bounds the files of a diff.
	maxSysreportDiffFiles = 200
	// maxSysreportDiffBytes bounds the diff of one file, and
	// maxSysreportDiffTotalBytes the diffs of a call: the diff of a file
	// that does not fit is left out, its file listed all the same.
	maxSysreportDiffBytes      = 16 << 10
	maxSysreportDiffTotalBytes = 64 << 10
	maxSysreportFiles          = 200
	maxSysreportContentBytes   = 32 << 10
	// maxSysreportSuggestions bounds the paths proposed for an unknown one.
	maxSysreportSuggestions = 5
)

var (
	// sysreportDatePattern is a date the collector passes to git, in its
	// time zone without one.
	sysreportDatePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}(T\d{2}:\d{2}(:\d{2})?(Z|[+-]\d{2}:\d{2})?)?$`)
	// sysreportDayPattern is a date without a time.
	sysreportDayPattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	// sysreportRevisionPattern is a revision of a sysreport, a git commit
	// ID, abbreviated or not.
	sysreportRevisionPattern = regexp.MustCompile(`^[0-9a-f]{7,64}$`)
	sysreportObjectPattern   = regexp.MustCompile(`^[0-9a-f]{40,64}$`)
)

// SysreportPoint is a state of the sysreport of a node: the report it was
// left in.
type SysreportPoint struct {
	Revision string `json:"revision,omitempty" jsonschema:"the revision of the report; empty before the first report of the node"`
	Date     string `json:"date,omitempty" jsonschema:"when the node sent the report, ISO 8601 with its offset; empty when the revision was given and not dated"`
}

type ListNodeSysreportChangesOptions struct {
	Node   string
	Path   string
	Since  string
	Until  string
	Limit  int
	Cursor string
}

type SysreportChangeList struct {
	Provenance Provenance    `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Node       NodeReference `json:"node" jsonschema:"the node whose sysreport changes are listed"`
	// NoSysreport tells a node that never reported from filters matching
	// nothing: the collector counts no change for both.
	NoSysreport bool              `json:"no_sysreport,omitempty" jsonschema:"whether the node never sent a sysreport: the collector holds nothing of its configuration, which says nothing of its changes"`
	Total       int               `json:"total" jsonschema:"the number of reports that changed a matching file, as the collector counts them"`
	Count       int               `json:"count" jsonschema:"the number of changes returned in this page"`
	Changes     []SysreportChange `json:"changes" jsonschema:"the reports that changed something, the newest first"`
	NextCursor  string            `json:"next_cursor,omitempty" jsonschema:"the opaque cursor to pass with the same filters for the next page"`
	Truncated   bool              `json:"truncated" jsonschema:"whether matching changes remain after this page"`
}

type SysreportChange struct {
	Revision       string                `json:"revision" jsonschema:"the revision of the report: pass it as revision to get_node_sysreport_diff for its diff, or as at to the file tools"`
	Date           string                `json:"date" jsonschema:"when the node sent the report, ISO 8601 with its offset"`
	Initial        bool                  `json:"initial,omitempty" jsonschema:"whether this is the first report of the node, which adds every file"`
	FilesCount     int                   `json:"files_count" jsonschema:"the number of matching files the report changed"`
	Files          []SysreportFileChange `json:"files" jsonschema:"the matching files the report changed, at most 50"`
	FilesTruncated bool                  `json:"files_truncated,omitempty" jsonschema:"whether files were left out beyond 50"`
}

type SysreportFileChange struct {
	Path    string `json:"path" jsonschema:"the absolute path of the file, or the command line of the command whose output the node reports"`
	Kind    string `json:"kind" jsonschema:"file or command"`
	Added   int    `json:"added" jsonschema:"the number of lines added"`
	Deleted int    `json:"deleted" jsonschema:"the number of lines deleted"`
	Binary  bool   `json:"binary,omitempty" jsonschema:"whether the content is binary, without line counts"`
}

type collectorSysreportChange struct {
	CID     string                   `json:"cid"`
	Date    string                   `json:"date"`
	Initial bool                     `json:"initial"`
	Files   []collectorSysreportFile `json:"files"`
}

type collectorSysreportFile struct {
	OID        string  `json:"oid"`
	Path       string  `json:"path"`
	Kind       string  `json:"kind"`
	Size       int64   `json:"size"`
	Added      int     `json:"added"`
	Deleted    int     `json:"deleted"`
	Binary     bool    `json:"binary"`
	Restricted bool    `json:"restricted"`
	Truncated  bool    `json:"truncated"`
	Diff       *string `json:"diff"`
	Content    *string `json:"content"`
}

// ListNodeSysreportChanges reads one page of the changes of the files and
// command outputs one node reports, the newest first.
func (s *Service) ListNodeSysreportChanges(ctx context.Context, options ListNodeSysreportChangesOptions) (SysreportChangeList, error) {
	if err := validateFilterValue("path", options.Path); err != nil {
		return SysreportChangeList{}, err
	}
	since, err := sysreportDate("since", options.Since, false)
	if err != nil {
		return SysreportChangeList{}, err
	}
	until, err := sysreportDate("until", options.Until, true)
	if err != nil {
		return SysreportChangeList{}, err
	}
	limit, err := pageLimit(options.Limit, defaultSysreportChangesLimit, maxSysreportChangesLimit)
	if err != nil {
		return SysreportChangeList{}, err
	}
	node, err := s.lookupNode(ctx, options.Node, []string{"node_id", "nodename", "cluster_id"})
	if err != nil {
		return SysreportChangeList{}, err
	}
	query := url.Values{}
	for name, value := range map[string]string{"path": options.Path, "begin": since, "end": until} {
		if value != "" {
			query.Set(name, value)
		}
	}
	fingerprint := listFingerprint(url.Values{"node": {node.NodeID}, "path": {strings.ToLower(options.Path)}, "begin": {since}, "end": {until}})
	offset, err := decodeListCursor(options.Cursor, fingerprint)
	if err != nil {
		return SysreportChangeList{}, err
	}
	query.Set("limit", strconv.Itoa(limit))
	query.Set("offset", strconv.Itoa(offset))

	var response collectorList[collectorSysreportChange]
	if err := s.client.GetJSON(ctx, sysreportPath(node.NodeID), query, &response); err != nil {
		return SysreportChangeList{}, fmt.Errorf("list sysreport changes of node %s: %w", node.Nodename, err)
	}
	if response.Meta.Total == nil || *response.Meta.Total < 0 || len(response.Data) > limit {
		return SysreportChangeList{}, fmt.Errorf("list sysreport changes of node %s: unexpected collector response", node.Nodename)
	}
	result := SysreportChangeList{
		Provenance: s.newProvenance(), Node: NodeReference{NodeID: node.NodeID, Nodename: node.Nodename, ClusterID: node.ClusterID},
		Total: *response.Meta.Total, Count: len(response.Data), Changes: make([]SysreportChange, 0, len(response.Data)),
	}
	for _, row := range response.Data {
		change := SysreportChange{
			Revision: row.CID, Date: row.Date, Initial: row.Initial, FilesCount: len(row.Files),
			Files: make([]SysreportFileChange, 0, min(len(row.Files), maxSysreportChangeFiles)),
		}
		for _, file := range row.Files {
			if len(change.Files) == maxSysreportChangeFiles {
				change.FilesTruncated = true
				break
			}
			change.Files = append(change.Files, SysreportFileChange{Path: file.Path, Kind: file.Kind, Added: file.Added, Deleted: file.Deleted, Binary: file.Binary})
		}
		result.Changes = append(result.Changes, change)
	}
	if next := offset + len(response.Data); len(response.Data) > 0 && next < result.Total {
		result.NextCursor = encodeListCursor(next, fingerprint)
		result.Truncated = true
	}
	if result.Total == 0 {
		hasReport, err := s.hasSysreport(ctx, node.NodeID, query.Get("path") != "" || since != "" || until != "")
		if err != nil {
			return SysreportChangeList{}, fmt.Errorf("list sysreport changes of node %s: %w", node.Nodename, err)
		}
		result.NoSysreport = !hasReport
	}
	return result, nil
}

// hasSysreport tells whether a node sent a sysreport, when a listing found
// no change: without filters, it did not.
func (s *Service) hasSysreport(ctx context.Context, nodeID string, filtered bool) (bool, error) {
	if !filtered {
		return false, nil
	}
	var response collectorList[collectorSysreportChange]
	if err := s.client.GetJSON(ctx, sysreportPath(nodeID), url.Values{"limit": {"1"}}, &response); err != nil {
		return false, err
	}
	return len(response.Data) > 0, nil
}

type GetNodeSysreportDiffOptions struct {
	Node     string
	Revision string
	Since    string
	Until    string
	Path     string
}

type SysreportDiff struct {
	Provenance     Provenance          `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Node           NodeReference       `json:"node" jsonschema:"the node whose sysreport is compared"`
	From           *SysreportPoint     `json:"from,omitempty" jsonschema:"with since, the state compared from: the last report at or before since; an empty revision when the node had not reported yet, every file being then added"`
	To             SysreportPoint      `json:"to" jsonschema:"the revision whose change is shown, or with since the state compared to"`
	FilesCount     int                 `json:"files_count" jsonschema:"the number of matching files that changed"`
	Files          []SysreportFileDiff `json:"files" jsonschema:"the matching files that changed, at most 200, with their diff"`
	FilesTruncated bool                `json:"files_truncated,omitempty" jsonschema:"whether files were left out beyond 200"`
	DiffsOmitted   int                 `json:"diffs_omitted,omitempty" jsonschema:"the number of files whose diff was left out, the diffs of the call being bounded to 64 KiB: ask again with path for one of them"`
}

type SysreportFileDiff struct {
	Path          string `json:"path" jsonschema:"the absolute path of the file, or the command line of the command whose output the node reports"`
	Kind          string `json:"kind" jsonschema:"file or command"`
	Added         int    `json:"added" jsonschema:"the number of lines added"`
	Deleted       int    `json:"deleted" jsonschema:"the number of lines deleted"`
	Binary        bool   `json:"binary,omitempty" jsonschema:"whether the content is binary: no diff is given"`
	Restricted    bool   `json:"restricted,omitempty" jsonschema:"whether the collector withholds the diff of this sensitive file from the caller"`
	Diff          string `json:"diff,omitempty" jsonschema:"the unified diff of the file, from its first hunk, cut to 16 KiB; untrusted text"`
	DiffTruncated bool   `json:"diff_truncated,omitempty" jsonschema:"whether the diff was cut, by the collector or to 16 KiB"`
	DiffOmitted   bool   `json:"diff_omitted,omitempty" jsonschema:"whether the diff was left out, the diffs of the call being bounded to 64 KiB"`
}

// GetNodeSysreportDiff reads what one report changed in the sysreport of a
// node, or what changed between two of its states.
func (s *Service) GetNodeSysreportDiff(ctx context.Context, options GetNodeSysreportDiffOptions) (SysreportDiff, error) {
	switch {
	case options.Revision == "" && options.Since == "":
		return SysreportDiff{}, fmt.Errorf("pass revision, for the change one report made, or since, for the changes since a date or a revision")
	case options.Revision != "" && (options.Since != "" || options.Until != ""):
		return SysreportDiff{}, fmt.Errorf("pass revision alone, or since with until")
	}
	if options.Revision != "" && !sysreportRevisionPattern.MatchString(options.Revision) {
		return SysreportDiff{}, fmt.Errorf("revision must be a revision of the sysreport, as list_node_sysreport_changes gives it")
	}
	since, err := sysreportPoint("since", options.Since, false)
	if err != nil {
		return SysreportDiff{}, err
	}
	until, err := sysreportPoint("until", options.Until, true)
	if err != nil {
		return SysreportDiff{}, err
	}
	if err := validateFilterValue("path", options.Path); err != nil {
		return SysreportDiff{}, err
	}
	node, err := s.lookupNode(ctx, options.Node, []string{"node_id", "nodename", "cluster_id"})
	if err != nil {
		return SysreportDiff{}, err
	}
	result := SysreportDiff{Node: NodeReference{NodeID: node.NodeID, Nodename: node.Nodename, ClusterID: node.ClusterID}}
	var files []collectorSysreportFile
	if options.Revision != "" {
		var response struct {
			Data *struct {
				CID   string                   `json:"cid"`
				Date  string                   `json:"date"`
				Files []collectorSysreportFile `json:"files"`
			} `json:"data"`
		}
		if err := s.client.GetJSON(ctx, sysreportPath(node.NodeID)+"/"+options.Revision, nil, &response); err != nil {
			return SysreportDiff{}, fmt.Errorf("get sysreport change %s of node %s: %w", options.Revision, node.Nodename, err)
		}
		if response.Data == nil {
			return SysreportDiff{}, fmt.Errorf("get sysreport change of node %s: unexpected collector response", node.Nodename)
		}
		result.To = SysreportPoint{Revision: response.Data.CID, Date: response.Data.Date}
		files = response.Data.Files
	} else {
		query := url.Values{"begin": {since}}
		if until != "" {
			query.Set("end", until)
		}
		var response struct {
			Data *struct {
				Begin collectorSysreportPoint  `json:"begin"`
				End   collectorSysreportPoint  `json:"end"`
				Files []collectorSysreportFile `json:"files"`
			} `json:"data"`
		}
		if err := s.client.GetJSON(ctx, sysreportPath(node.NodeID)+"/timediff", query, &response); err != nil {
			return SysreportDiff{}, fmt.Errorf("compare sysreport states of node %s: %w", node.Nodename, err)
		}
		if response.Data == nil {
			return SysreportDiff{}, fmt.Errorf("compare sysreport states of node %s: unexpected collector response", node.Nodename)
		}
		result.From = &SysreportPoint{Revision: response.Data.Begin.CID, Date: response.Data.Begin.Date}
		result.To = SysreportPoint{Revision: response.Data.End.CID, Date: response.Data.End.Date}
		files = response.Data.Files
	}

	needle := strings.ToLower(options.Path)
	remaining := maxSysreportDiffTotalBytes
	result.Files = []SysreportFileDiff{}
	for _, file := range files {
		if needle != "" && !strings.Contains(strings.ToLower(file.Path), needle) {
			continue
		}
		result.FilesCount++
		if len(result.Files) == maxSysreportDiffFiles {
			result.FilesTruncated = true
			continue
		}
		diff := SysreportFileDiff{
			Path: file.Path, Kind: file.Kind, Added: file.Added, Deleted: file.Deleted, Binary: file.Binary,
			Restricted: file.Restricted, DiffTruncated: file.Truncated,
		}
		if file.Diff != nil && *file.Diff != "" {
			text, cut := truncateLines(*file.Diff, maxSysreportDiffBytes)
			if len(text) > remaining {
				diff.DiffOmitted = true
				result.DiffsOmitted++
			} else {
				diff.Diff, diff.DiffTruncated = text, diff.DiffTruncated || cut
				remaining -= len(text)
			}
		}
		result.Files = append(result.Files, diff)
	}
	result.Provenance = s.newProvenance()
	return result, nil
}

// collectorSysreportPoint is a state of a sysreport as the collector gives
// it.
type collectorSysreportPoint struct {
	CID  string `json:"cid"`
	Date string `json:"date"`
}

type ListNodeSysreportFilesOptions struct {
	Node string
	At   string
	Path string
}

type SysreportFileList struct {
	Provenance Provenance      `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Node       NodeReference   `json:"node" jsonschema:"the node whose sysreport files are listed"`
	Revision   SysreportPoint  `json:"revision" jsonschema:"the report the files are read at"`
	Total      int             `json:"total" jsonschema:"the number of matching files and commands the report holds"`
	Count      int             `json:"count" jsonschema:"the number of files returned"`
	Files      []SysreportFile `json:"files" jsonschema:"the matching files and commands, at most 200"`
	Truncated  bool            `json:"truncated" jsonschema:"whether files were left out beyond 200: narrow them with path"`
}

type SysreportFile struct {
	Path       string `json:"path" jsonschema:"the absolute path of the file, or the command line of the command whose output the node reports; pass it as path to get_node_sysreport_file"`
	Kind       string `json:"kind" jsonschema:"file or command"`
	Size       int64  `json:"size" jsonschema:"the size of the content in bytes"`
	Restricted bool   `json:"restricted,omitempty" jsonschema:"whether the collector withholds the content of this sensitive file from the caller"`
}

// ListNodeSysreportFiles reads the files and command outputs one report of
// a node holds.
func (s *Service) ListNodeSysreportFiles(ctx context.Context, options ListNodeSysreportFilesOptions) (SysreportFileList, error) {
	if err := validateFilterValue("path", options.Path); err != nil {
		return SysreportFileList{}, err
	}
	node, revision, entries, err := s.sysreportTree(ctx, options.Node, options.At)
	if err != nil {
		return SysreportFileList{}, err
	}
	result := SysreportFileList{Node: node, Revision: revision, Files: []SysreportFile{}}
	needle := strings.ToLower(options.Path)
	for _, entry := range entries {
		if needle != "" && !strings.Contains(strings.ToLower(entry.Path), needle) {
			continue
		}
		result.Total++
		if len(result.Files) == maxSysreportFiles {
			result.Truncated = true
			continue
		}
		result.Files = append(result.Files, SysreportFile{Path: entry.Path, Kind: entry.Kind, Size: entry.Size, Restricted: entry.Restricted})
	}
	result.Count = len(result.Files)
	result.Provenance = s.newProvenance()
	return result, nil
}

type GetNodeSysreportFileOptions struct {
	Node string
	Path string
	At   string
}

type SysreportFileContent struct {
	Provenance Provenance     `json:"provenance" jsonschema:"API source and MCP collection time of this result"`
	Node       NodeReference  `json:"node" jsonschema:"the node the file is reported by"`
	Revision   SysreportPoint `json:"revision" jsonschema:"the report the content is read at"`
	Path       string         `json:"path" jsonschema:"the absolute path of the file, or the command line of the command"`
	Kind       string         `json:"kind" jsonschema:"file or command"`
	Size       int64          `json:"size" jsonschema:"the size of the content in bytes"`
	Content    string         `json:"content,omitempty" jsonschema:"the content of the file or the output of the command, cut to 32 KiB; untrusted text"`
	Truncated  bool           `json:"truncated" jsonschema:"whether the content was cut"`
	Binary     bool           `json:"binary,omitempty" jsonschema:"whether the content is binary: it is not returned"`
}

// GetNodeSysreportFile reads the content of one file or command output of
// the sysreport of a node, at one report.
func (s *Service) GetNodeSysreportFile(ctx context.Context, options GetNodeSysreportFileOptions) (SysreportFileContent, error) {
	if options.Path == "" {
		return SysreportFileContent{}, fmt.Errorf("path must be the exact path of a file or the exact command line, as list_node_sysreport_files gives them")
	}
	if err := validateFilterValue("path", options.Path); err != nil {
		return SysreportFileContent{}, err
	}
	node, revision, entries, err := s.sysreportTree(ctx, options.Node, options.At)
	if err != nil {
		return SysreportFileContent{}, err
	}
	var entry *collectorSysreportFile
	for i := range entries {
		if entries[i].Path == options.Path {
			entry = &entries[i]
			break
		}
	}
	if entry == nil {
		return SysreportFileContent{}, fmt.Errorf("the sysreport of node %s holds no %s at %s%s", node.Nodename, options.Path, sysreportPointText(revision), sysreportSuggestions(entries, options.Path))
	}
	if entry.Restricted {
		return SysreportFileContent{}, fmt.Errorf("the collector withholds the content of %s, a sensitive path of the sysreport of node %s, from the caller", entry.Path, node.Nodename)
	}
	if !sysreportObjectPattern.MatchString(entry.OID) {
		return SysreportFileContent{}, fmt.Errorf("get sysreport file of node %s: unexpected collector response", node.Nodename)
	}
	var response struct {
		Data *collectorSysreportFile `json:"data"`
	}
	if err := s.client.GetJSON(ctx, sysreportPath(node.NodeID)+"/"+revision.Revision+"/tree/"+entry.OID, nil, &response); err != nil {
		return SysreportFileContent{}, fmt.Errorf("get sysreport file %s of node %s: %w", entry.Path, node.Nodename, err)
	}
	if response.Data == nil {
		return SysreportFileContent{}, fmt.Errorf("get sysreport file of node %s: unexpected collector response", node.Nodename)
	}
	result := SysreportFileContent{
		Provenance: s.newProvenance(), Node: node, Revision: revision, Path: entry.Path, Kind: entry.Kind, Size: entry.Size,
		Truncated: response.Data.Truncated, Binary: response.Data.Binary,
	}
	if response.Data.Content != nil && !response.Data.Binary {
		content, cut := truncateUTF8(*response.Data.Content, maxSysreportContentBytes)
		result.Content, result.Truncated = content, result.Truncated || cut
	}
	return result, nil
}

// sysreportTree resolves the node and the report at, the latest without it,
// and reads the files and command outputs that report holds.
func (s *Service) sysreportTree(ctx context.Context, nodeName string, at string) (NodeReference, SysreportPoint, []collectorSysreportFile, error) {
	point, err := sysreportPoint("at", at, true)
	if err != nil {
		return NodeReference{}, SysreportPoint{}, nil, err
	}
	found, err := s.lookupNode(ctx, nodeName, []string{"node_id", "nodename", "cluster_id"})
	if err != nil {
		return NodeReference{}, SysreportPoint{}, nil, err
	}
	node := NodeReference{NodeID: found.NodeID, Nodename: found.Nodename, ClusterID: found.ClusterID}
	revision := SysreportPoint{Revision: point}
	if !sysreportRevisionPattern.MatchString(point) {
		// The latest report, or the last one at or before a date.
		query := url.Values{"limit": {"1"}}
		if point != "" {
			query.Set("end", point)
		}
		var response collectorList[collectorSysreportChange]
		if err := s.client.GetJSON(ctx, sysreportPath(node.NodeID), query, &response); err != nil {
			return NodeReference{}, SysreportPoint{}, nil, fmt.Errorf("read sysreport of node %s: %w", node.Nodename, err)
		}
		if len(response.Data) == 0 {
			if point == "" {
				return NodeReference{}, SysreportPoint{}, nil, fmt.Errorf("node %s has no sysreport", node.Nodename)
			}
			return NodeReference{}, SysreportPoint{}, nil, fmt.Errorf("node %s sent no sysreport at or before %s", node.Nodename, at)
		}
		if !sysreportRevisionPattern.MatchString(response.Data[0].CID) {
			return NodeReference{}, SysreportPoint{}, nil, fmt.Errorf("read sysreport of node %s: unexpected collector response", node.Nodename)
		}
		revision = SysreportPoint{Revision: response.Data[0].CID, Date: response.Data[0].Date}
	}
	var response struct {
		Data []collectorSysreportFile `json:"data"`
	}
	if err := s.client.GetJSON(ctx, sysreportPath(node.NodeID)+"/"+revision.Revision+"/tree", nil, &response); err != nil {
		return NodeReference{}, SysreportPoint{}, nil, fmt.Errorf("list sysreport files of node %s: %w", node.Nodename, err)
	}
	if response.Data == nil {
		return NodeReference{}, SysreportPoint{}, nil, fmt.Errorf("list sysreport files of node %s: unexpected collector response", node.Nodename)
	}
	return node, revision, response.Data, nil
}

func sysreportPath(nodeID string) string {
	return "/nodes/" + url.PathEscape(nodeID) + "/sysreport"
}

// sysreportDate checks a date given by the caller. The collector hands it
// to git, which reads a day without a time as that day at the current time:
// a day is completed to its start, or to its end for an upper bound.
func sysreportDate(name string, value string, end bool) (string, error) {
	if value == "" {
		return "", nil
	}
	if !sysreportDatePattern.MatchString(value) {
		return "", fmt.Errorf("%s must be a date: YYYY-MM-DD, or YYYY-MM-DDTHH:MM[:SS] with an optional Z or offset, in the collector time zone without one", name)
	}
	if sysreportDayPattern.MatchString(value) {
		if end {
			return value + "T23:59:59", nil
		}
		return value + "T00:00:00", nil
	}
	return value, nil
}

// sysreportPoint checks a state of a sysreport given by the caller: a date,
// completed as sysreportDate does, or a revision.
func sysreportPoint(name string, value string, end bool) (string, error) {
	if sysreportRevisionPattern.MatchString(value) {
		return value, nil
	}
	date, err := sysreportDate(name, value, end)
	if err != nil {
		return "", fmt.Errorf("%s must be a revision of the sysreport or a date: YYYY-MM-DD, or YYYY-MM-DDTHH:MM[:SS] with an optional Z or offset, in the collector time zone without one", name)
	}
	return date, nil
}

func sysreportPointText(point SysreportPoint) string {
	if point.Date != "" {
		return "revision " + point.Revision + " of " + point.Date
	}
	return "revision " + point.Revision
}

// sysreportSuggestions proposes the paths holding the last element of an
// unknown path, or that element without its extension, such as fstab for
// /etc/fstab.bak, or the command of a command line.
func sysreportSuggestions(entries []collectorSysreportFile, path string) string {
	name := strings.ToLower(path)
	if i := strings.LastIndex(name, "/"); i >= 0 && i < len(name)-1 {
		name = name[i+1:]
	}
	if fields := strings.Fields(name); len(fields) > 0 {
		name = fields[0]
	}
	needles := []string{name}
	if stem, _, ok := strings.Cut(name, "."); ok && stem != "" {
		needles = append(needles, stem)
	}
	for _, needle := range needles {
		var paths []string
		for _, entry := range entries {
			if strings.Contains(strings.ToLower(entry.Path), needle) {
				paths = append(paths, entry.Path)
				if len(paths) == maxSysreportSuggestions {
					break
				}
			}
		}
		if len(paths) > 0 {
			return "; close paths: " + strings.Join(paths, ", ")
		}
	}
	return "; list them with list_node_sysreport_files"
}

// truncateLines cuts text to at most maxBytes at the end of a line, or
// within the first line when it is longer.
func truncateLines(text string, maxBytes int) (string, bool) {
	cut, truncated := truncateUTF8(text, maxBytes)
	if !truncated {
		return text, false
	}
	if i := strings.LastIndex(cut, "\n"); i > 0 {
		cut = cut[:i+1]
	}
	return cut, true
}
