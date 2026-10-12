package core

import (
	"testing"
	"time"
)

func TestNewProvenanceUsesUTCCollectionTime(t *testing.T) {
	service := New(nil)
	service.now = func() time.Time { return time.Date(2026, 10, 12, 13, 4, 5, 6, time.FixedZone("CEST", 2*3600)) }
	provenance := service.newProvenance()
	if provenance.Source != "opensvc_collector" || provenance.ObservedAt != "2026-10-12T11:04:05.000000006Z" {
		t.Fatalf("provenance = %+v", provenance)
	}
}
