package core

import (
	"fmt"
	"net/url"
	"strings"
	"testing"
)

const (
	sysreportNode = `{"data":[{"node_id":"n-1","nodename":"dev2n1","cluster_id":"c1"}]}`
	sysreportHead = "1196cc2469d8a1b2c3d4e5f60718293a4b5c6d7e"
	sysreportOld  = "45550cc8749222d67851ebd25d7c21785726fe36"
	fstabOID      = "0123456789abcdef0123456789abcdef01234567"
)

func TestListNodeSysreportChangesPagesAndBoundsTheFiles(t *testing.T) {
	var files []string
	for i := range maxSysreportChangeFiles + 3 {
		files = append(files, fmt.Sprintf(`{"path":"/etc/f%d","kind":"file","added":%d,"deleted":0,"binary":false}`, i, i+1))
	}
	collector := &fakeCollector{bodies: map[string]string{
		"/nodes/dev2n1": sysreportNode,
		"/nodes/n-1/sysreport": `{"data":[
			{"cid":"` + sysreportHead + `","date":"2026-10-14T16:09:38+02:00","initial":false,"files":[{"path":"ip -br addr","kind":"command","added":1,"deleted":1,"binary":false}]},
			{"cid":"` + sysreportOld + `","date":"2026-09-29T23:03:24+02:00","initial":true,"files":[` + strings.Join(files, ",") + `]}],
			"meta":{"total":5,"limit":2,"offset":0,"count":2}}`,
	}}
	changes, err := newTestService(collector).ListNodeSysreportChanges(t.Context(), ListNodeSysreportChangesOptions{
		Node: "dev2n1", Path: "etc", Since: "2026-09-01", Until: "2026-10-14", Limit: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	query := collector.requests[1].query
	// A day is completed: git reads a day without a time as that day at the
	// current time.
	if query.Get("path") != "etc" || query.Get("begin") != "2026-09-01T00:00:00" || query.Get("end") != "2026-10-14T23:59:59" ||
		query.Get("limit") != "2" || query.Get("offset") != "0" {
		t.Fatalf("query = %v", query)
	}
	initial := changes.Changes[1]
	if changes.Node.Nodename != "dev2n1" || changes.Total != 5 || changes.Count != 2 || !changes.Truncated || changes.NextCursor == "" ||
		changes.Changes[0].Revision != sysreportHead || changes.Changes[0].Files[0].Kind != "command" ||
		!initial.Initial || initial.FilesCount != maxSysreportChangeFiles+3 || len(initial.Files) != maxSysreportChangeFiles || !initial.FilesTruncated {
		t.Fatalf("changes = %+v", changes)
	}
	// The cursor is bound to the filters.
	if _, err := newTestService(collector).ListNodeSysreportChanges(t.Context(), ListNodeSysreportChangesOptions{Node: "dev2n1", Cursor: changes.NextCursor}); err == nil {
		t.Fatal("cursor accepted with other filters")
	}
}

func TestListNodeSysreportChangesTellsANodeWithoutSysreport(t *testing.T) {
	empty := `{"data":[],"meta":{"total":0,"limit":20,"offset":0,"count":0}}`
	collector := &fakeCollector{bodies: map[string]string{"/nodes/dev2n1": sysreportNode, "/nodes/n-1/sysreport": empty}}
	changes, err := newTestService(collector).ListNodeSysreportChanges(t.Context(), ListNodeSysreportChangesOptions{Node: "dev2n1"})
	if err != nil || !changes.NoSysreport || len(collector.requests) != 2 {
		t.Fatalf("without filters: %+v, %v, %d requests", changes, err, len(collector.requests))
	}
	// With filters, nothing matching does not tell: the latest report is read.
	collector = &fakeCollector{answer: func(path string, query url.Values) string {
		if path == "/nodes/dev2n1" {
			return sysreportNode
		}
		if query.Get("path") != "" {
			return empty
		}
		return `{"data":[{"cid":"` + sysreportHead + `","date":"2026-10-14T16:09:38+02:00","files":[{"path":"/etc/hosts","kind":"file"}]}],"meta":{"total":1}}`
	}}
	changes, err = newTestService(collector).ListNodeSysreportChanges(t.Context(), ListNodeSysreportChangesOptions{Node: "dev2n1", Path: "nosuch"})
	if err != nil || changes.NoSysreport || changes.Total != 0 || len(collector.requests) != 3 || collector.requests[2].query.Get("limit") != "1" {
		t.Fatalf("with filters: %+v, %v, %d requests", changes, err, len(collector.requests))
	}
}

func TestGetNodeSysreportDiffOfARevisionBoundsTheDiffs(t *testing.T) {
	// Each diff is cut to 16 KiB at a line end: 910 lines of 18 bytes, so
	// that 4 of them fit in the 64 KiB of a call, with 16 bytes left.
	big := strings.ReplaceAll(strings.Repeat("+line of lvm.conf\n", maxSysreportDiffBytes/10), "\n", `\n`)
	var bigFiles []string
	for i := range 5 {
		bigFiles = append(bigFiles, fmt.Sprintf(`{"path":"/etc/big%d","kind":"file","added":1600,"deleted":0,"binary":false,"secure":true,"restricted":false,"truncated":false,"diff":"%s"}`, i, big))
	}
	collector := &fakeCollector{bodies: map[string]string{
		"/nodes/dev2n1": sysreportNode,
		"/nodes/n-1/sysreport/" + sysreportHead[:8]: `{"data":{"cid":"` + sysreportHead[:8] + `","date":"2026-10-14T16:09:38+02:00","files":[` + strings.Join(bigFiles, ",") + `,
			{"path":"/etc/last","kind":"file","added":1,"deleted":0,"binary":false,"secure":true,"restricted":false,"truncated":false,"diff":"+late\n"},
			{"path":"/etc/shadow","kind":"file","added":1,"deleted":1,"binary":false,"secure":true,"restricted":true,"truncated":false},
			{"path":"/usr/bin/tool","kind":"file","added":0,"deleted":0,"binary":true,"secure":false,"restricted":false,"truncated":false}]}}`,
	}}
	diff, err := newTestService(collector).GetNodeSysreportDiff(t.Context(), GetNodeSysreportDiffOptions{Node: "dev2n1", Revision: sysreportHead[:8]})
	if err != nil {
		t.Fatal(err)
	}
	if diff.From != nil || diff.To.Revision != sysreportHead[:8] || diff.FilesCount != 8 || len(diff.Files) != 8 {
		t.Fatalf("diff = from %v, to %+v, %d files", diff.From, diff.To, diff.FilesCount)
	}
	for _, file := range diff.Files[:4] {
		if len(file.Diff) != 910*18 || !file.DiffTruncated || !strings.HasSuffix(file.Diff, "\n") || file.DiffOmitted {
			t.Fatalf("%s diff cut to %d bytes, truncated %v", file.Path, len(file.Diff), file.DiffTruncated)
		}
	}
	// The fifth big diff does not fit: it is left out, its file listed, and
	// the next diff, small, still fits.
	fifth, last, shadow, binary := diff.Files[4], diff.Files[5], diff.Files[6], diff.Files[7]
	if fifth.Diff != "" || !fifth.DiffOmitted || fifth.Added != 1600 || last.Diff != "+late\n" || diff.DiffsOmitted != 1 {
		t.Fatalf("budget: fifth omitted %v, last %q, omitted %d", fifth.DiffOmitted, last.Diff, diff.DiffsOmitted)
	}
	if !shadow.Restricted || shadow.Diff != "" || !binary.Binary || binary.Diff != "" {
		t.Fatalf("restricted %+v, binary %+v", shadow, binary)
	}
}

func TestGetNodeSysreportDiffSinceADateFiltersThePath(t *testing.T) {
	collector := &fakeCollector{bodies: map[string]string{
		"/nodes/dev2n1": sysreportNode,
		"/nodes/n-1/sysreport/timediff": `{"data":{"begin":{"cid":"","date":""},"end":{"cid":"` + sysreportHead + `","date":"2026-10-14T16:09:38+02:00"},"files":[
			{"path":"/etc/fstab","kind":"file","added":2,"deleted":0,"binary":false,"secure":true,"restricted":false,"truncated":false,"diff":"+/dev/sda1 / ext4\n"},
			{"path":"ip -br addr","kind":"command","added":1,"deleted":1,"binary":false,"secure":true,"restricted":false,"truncated":false,"diff":"-a\n+b\n"}]}}`,
	}}
	diff, err := newTestService(collector).GetNodeSysreportDiff(t.Context(), GetNodeSysreportDiffOptions{Node: "dev2n1", Since: "2026-09-01", Until: sysreportHead[:7], Path: "FSTAB"})
	if err != nil {
		t.Fatal(err)
	}
	query := collector.requests[1].query
	if query.Get("begin") != "2026-09-01T00:00:00" || query.Get("end") != sysreportHead[:7] {
		t.Fatalf("query = %v", query)
	}
	if diff.From == nil || diff.From.Revision != "" || diff.To.Revision != sysreportHead || diff.FilesCount != 1 || diff.Files[0].Path != "/etc/fstab" {
		t.Fatalf("diff = %+v", diff)
	}
}

func TestGetNodeSysreportDiffRejectsInvalidInputs(t *testing.T) {
	for _, options := range []GetNodeSysreportDiffOptions{
		{Node: "dev2n1"},
		{Node: "dev2n1", Revision: sysreportHead, Since: "2026-10-01"},
		{Node: "dev2n1", Revision: sysreportHead, Until: "2026-10-01"},
		{Node: "dev2n1", Revision: "HEAD~1"},
		{Node: "dev2n1", Since: "yesterday"},
		{Node: "dev2n1", Since: "-7d"},
		{Node: "dev2n1", Since: "2026-10-01", Until: "2026/10/02"},
		{Node: "dev2n1", Since: "2026-10-01", Path: " fstab"},
	} {
		collector := &fakeCollector{body: sysreportNode}
		if _, err := newTestService(collector).GetNodeSysreportDiff(t.Context(), options); err == nil || len(collector.requests) != 0 {
			t.Fatalf("options %+v accepted", options)
		}
	}
}

// sysreportTreeCollector answers a node, its timeline and the tree of its
// latest report.
func sysreportTreeCollector() *fakeCollector {
	return &fakeCollector{answer: func(path string, query url.Values) string {
		switch path {
		case "/nodes/dev2n1":
			return sysreportNode
		case "/nodes/n-1/sysreport":
			return `{"data":[{"cid":"` + sysreportHead + `","date":"2026-10-14T16:09:38+02:00","initial":false,"files":[{"path":"/etc/hosts","kind":"file","added":1,"deleted":0}]}],"meta":{"total":326}}`
		case "/nodes/n-1/sysreport/" + sysreportHead + "/tree", "/nodes/n-1/sysreport/" + sysreportOld + "/tree":
			return `{"data":[
				{"oid":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","path":"ip -br addr","kind":"command","size":512,"secure":true,"restricted":false},
				{"oid":"` + fstabOID + `","path":"/etc/fstab","kind":"file","size":40000,"secure":true,"restricted":false},
				{"oid":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","path":"/etc/fstab.d/extra","kind":"file","size":10,"secure":true,"restricted":false},
				{"oid":"cccccccccccccccccccccccccccccccccccccccc","path":"/etc/shadow","kind":"file","size":900,"secure":true,"restricted":true}]}`
		case "/nodes/n-1/sysreport/" + sysreportHead + "/tree/" + fstabOID:
			return `{"data":{"oid":"` + fstabOID + `","path":"/etc/fstab","kind":"file","size":40000,"secure":true,"truncated":false,"binary":false,"content":"` + strings.Repeat("é", 20000) + `"}}`
		}
		return `{"data":[],"meta":{"total":0}}`
	}}
}

func TestListNodeSysreportFilesReadsTheLatestReport(t *testing.T) {
	collector := sysreportTreeCollector()
	files, err := newTestService(collector).ListNodeSysreportFiles(t.Context(), ListNodeSysreportFilesOptions{Node: "dev2n1", Path: "FSTAB"})
	if err != nil {
		t.Fatal(err)
	}
	if collector.requests[1].query.Get("limit") != "1" || collector.requests[1].query.Get("end") != "" {
		t.Fatalf("timeline query = %v", collector.requests[1].query)
	}
	if files.Revision.Revision != sysreportHead || files.Revision.Date == "" || files.Total != 2 || files.Count != 2 ||
		files.Files[0].Path != "/etc/fstab" || files.Files[0].Size != 40000 || files.Truncated {
		t.Fatalf("files = %+v", files)
	}
	// At a date, the last report at or before its end is read.
	if _, err := newTestService(collector).ListNodeSysreportFiles(t.Context(), ListNodeSysreportFilesOptions{Node: "dev2n1", At: "2026-10-01"}); err != nil {
		t.Fatal(err)
	}
	if collector.requests[4].query.Get("end") != "2026-10-01T23:59:59" {
		t.Fatalf("timeline query at a date = %v", collector.requests[4].query)
	}
	// At a revision, the tree is read without the timeline.
	collector = sysreportTreeCollector()
	files, err = newTestService(collector).ListNodeSysreportFiles(t.Context(), ListNodeSysreportFilesOptions{Node: "dev2n1", At: sysreportOld})
	if err != nil || len(collector.requests) != 2 || files.Revision.Revision != sysreportOld || files.Revision.Date != "" || files.Total != 4 {
		t.Fatalf("files at a revision = %+v, %v, %d requests", files, err, len(collector.requests))
	}
}

func TestGetNodeSysreportFileReadsOneContent(t *testing.T) {
	collector := sysreportTreeCollector()
	file, err := newTestService(collector).GetNodeSysreportFile(t.Context(), GetNodeSysreportFileOptions{Node: "dev2n1", Path: "/etc/fstab"})
	if err != nil {
		t.Fatal(err)
	}
	if file.Path != "/etc/fstab" || file.Kind != "file" || file.Size != 40000 || len(file.Content) > maxSysreportContentBytes || !file.Truncated ||
		!strings.HasPrefix(file.Content, "é") || file.Revision.Revision != sysreportHead {
		t.Fatalf("file = path %s, %d bytes, truncated %v", file.Path, len(file.Content), file.Truncated)
	}
	_, err = newTestService(sysreportTreeCollector()).GetNodeSysreportFile(t.Context(), GetNodeSysreportFileOptions{Node: "dev2n1", Path: "/etc/fstab.bak"})
	if err == nil || !strings.Contains(err.Error(), "close paths: /etc/fstab, /etc/fstab.d/extra") {
		t.Fatalf("unknown path: %v", err)
	}
	_, err = newTestService(sysreportTreeCollector()).GetNodeSysreportFile(t.Context(), GetNodeSysreportFileOptions{Node: "dev2n1", Path: "/etc/shadow"})
	if err == nil || !strings.Contains(err.Error(), "withholds") {
		t.Fatalf("restricted path: %v", err)
	}
	collector = &fakeCollector{bodies: map[string]string{"/nodes/dev2n1": sysreportNode, "/nodes/n-1/sysreport": `{"data":[],"meta":{"total":0}}`}}
	_, err = newTestService(collector).GetNodeSysreportFile(t.Context(), GetNodeSysreportFileOptions{Node: "dev2n1", Path: "/etc/fstab"})
	if err == nil || !strings.Contains(err.Error(), "has no sysreport") {
		t.Fatalf("node without sysreport: %v", err)
	}
	for _, options := range []GetNodeSysreportFileOptions{{Node: "dev2n1"}, {Node: "dev2n1", Path: "/etc/fstab", At: "last week"}} {
		collector := &fakeCollector{body: sysreportNode}
		if _, err := newTestService(collector).GetNodeSysreportFile(t.Context(), options); err == nil || len(collector.requests) != 0 {
			t.Fatalf("options %+v accepted", options)
		}
	}
}
