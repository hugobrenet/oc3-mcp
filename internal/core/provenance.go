package core

import "time"

const provenanceSourceOpenSVCCollector = "opensvc_collector"

// Provenance identifies the API source and the time the MCP finished collecting
// a successful result. ObservedAt does not assert that the underlying data is fresh.
type Provenance struct {
	Source     string `json:"source" jsonschema:"the API source of this result; opensvc_collector identifies the OpenSVC collector API"`
	ObservedAt string `json:"observed_at" jsonschema:"UTC time when the MCP finished collecting this result; this is not the time the collector received the data"`
}

func (s *Service) newProvenance() Provenance {
	return Provenance{
		Source:     provenanceSourceOpenSVCCollector,
		ObservedAt: s.now().UTC().Format(time.RFC3339Nano),
	}
}
