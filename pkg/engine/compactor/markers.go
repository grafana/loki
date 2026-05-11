// Package compactor contains the dataobj compactor coordinator and
// supporting libraries. This file provides workflow-marker primitives used
// by the coordinator to track in-flight compaction workflows in object
// storage for cross-restart recovery and cross-coordinator deduplication.
//
// See the dataobj-compactor design spec § "Workflow markers and recovery"
// for the full lifecycle and race semantics.
package compactor

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"time"
)

// DefaultInFlightPrefix is the bucket prefix under which workflow markers
// are written. The coordinator LISTs this prefix during recovery to
// enumerate in-flight workflows.
const DefaultInFlightPrefix = "dataobj/compaction/in-flight/"

// Marker is the persisted content of a workflow marker file. It records the
// identity of an in-flight compaction workflow so that coordinator restarts
// can recover (via state-poll) and so that other coordinator replicas can
// detect duplicate work.
//
// The encoded form is approximately 200 bytes.
type Marker struct {
	WorkflowID          string    `json:"workflow_id"`
	Tenant              string    `json:"tenant"`
	Window              time.Time `json:"window"`
	PlanVersion         int       `json:"plan_version"`
	StartedAt           time.Time `json:"started_at"`
	ExpectedLogObjects  int       `json:"expected_log_objects"`
	ExpectedIndexObject string    `json:"expected_index_object"`
}

// MarkerPath returns the deterministic object-storage path for the marker
// identifying the workflow that compacts (tenant, window, planVersion). The
// path is prefix + sha256(tenant + window.UTC().RFC3339 + planVersion) +
// ".json". Two coordinators that build the same plan will compute the same
// path; this is the basis of cross-coordinator deduplication.
func MarkerPath(prefix, tenant string, window time.Time, planVersion int) string {
	h := sha256.Sum256([]byte(tenant + window.UTC().Format(time.RFC3339) + strconv.Itoa(planVersion)))
	return prefix + hex.EncodeToString(h[:]) + ".json"
}
