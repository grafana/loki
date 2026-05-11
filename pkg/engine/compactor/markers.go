// Package compactor contains the dataobj compactor coordinator and
// supporting libraries. This file provides workflow-marker primitives used
// by the coordinator to track in-flight compaction workflows in object
// storage for cross-restart recovery and cross-coordinator deduplication.
//
// See the dataobj-compactor design spec § "Workflow markers and recovery"
// for the full lifecycle and race semantics.
package compactor

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/thanos-io/objstore"
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

// WriteMarker creates a workflow marker at path using bucket.GetAndReplace.
//
// Race semantics:
//
//   - On GCS / filesystem (atomic conditional-PUT backends), exactly one
//     concurrent caller wins and observes created=true; losers either observe
//     created=false (when their GetAndReplace's initial Get sees the
//     winner's content) or surface a precondition error from GetAndReplace
//     (the rarer case where two callers both saw nil at Get time and one's
//     conditional PUT on a non-existent object fails). Both outcomes are
//     acceptable; the caller treats both as "another coordinator is handling
//     this workflow".
//   - On S3 (current thanos-io/objstore provider), the conditional
//     precondition is not enforced for new objects (see spec § "No
//     coordinator leader election"). Two concurrent callers may both observe
//     created=true. The overall design (deterministic output paths,
//     idempotent ToC swap) tolerates this; this function does not attempt to
//     compensate for it.
//
// Soft idempotency: when the existing object's content equals the proposed
// content, WriteMarker returns (false, nil) — we did not create it, but no
// error: an exact byte-for-byte duplicate is the same workflow being
// (re)written. This is informational; callers may ignore the bool.
//
// The callback returns the existing bytes unchanged on race-loss so that
// backends performing an unconditional PUT inside GetAndReplace (in-mem,
// filesystem) write a no-op identical body, and so backends with optimistic
// concurrency (GCS) match against the existing generation rather than
// against "must not exist".
func WriteMarker(ctx context.Context, bucket objstore.Bucket, path string, content []byte) (created bool, err error) {
	err = bucket.GetAndReplace(ctx, path, func(existing io.ReadCloser) (io.ReadCloser, error) {
		if existing == nil {
			created = true
			return io.NopCloser(bytes.NewReader(content)), nil
		}
		defer existing.Close()
		existingBytes, readErr := io.ReadAll(existing)
		if readErr != nil {
			return nil, fmt.Errorf("read existing marker: %w", readErr)
		}
		// Race-loss (or soft idempotency): another writer beat us, or wrote
		// the same content. Either way, created stays false. Return the
		// existing bytes unchanged so the surrounding GetAndReplace performs
		// no logical mutation.
		return io.NopCloser(bytes.NewReader(existingBytes)), nil
	})
	if err != nil {
		return false, fmt.Errorf("write marker %q: %w", path, err)
	}
	return created, nil
}

// ListMarkers enumerates all parseable Marker objects under prefix using
// bucket.Iter, fetching and JSON-decoding each entry. Use
// DefaultInFlightPrefix for the standard coordinator path.
//
// Entries that fail to parse (corrupt JSON, manually-placed garbage) are
// silently skipped — the coordinator's recovery loop must remain robust to
// stray files, and a single bad object should not block recovery for all
// other in-flight workflows. The caller observing a missing workflow is
// expected to fall back to state-poll; an unparseable marker is
// operationally equivalent to a missing marker.
//
// Entries deleted between LIST and GET (a normal race against
// DeleteMarker) are likewise skipped.
//
// Order is unspecified.
func ListMarkers(ctx context.Context, bucket objstore.Bucket, prefix string) ([]Marker, error) {
	var markers []Marker
	err := bucket.Iter(ctx, prefix, func(name string) error {
		rc, getErr := bucket.Get(ctx, name)
		if getErr != nil {
			if bucket.IsObjNotFoundErr(getErr) {
				return nil
			}
			return fmt.Errorf("get %q: %w", name, getErr)
		}
		defer rc.Close()
		body, readErr := io.ReadAll(rc)
		if readErr != nil {
			return fmt.Errorf("read %q: %w", name, readErr)
		}
		m, parseErr := parseMarker(body)
		if parseErr != nil {
			// Skip unparseable entries: see function doc.
			return nil
		}
		markers = append(markers, m)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list markers under %q: %w", prefix, err)
	}
	return markers, nil
}

// DeleteMarker removes the marker at path. NotFound is treated as success
// (idempotent): it is normal for two coordinator replicas to both attempt
// deletion at the end of a workflow, and idempotent delete is also part of
// the force-cleanup path for stale markers.
func DeleteMarker(ctx context.Context, bucket objstore.Bucket, path string) error {
	err := bucket.Delete(ctx, path)
	if err == nil || bucket.IsObjNotFoundErr(err) {
		return nil
	}
	return fmt.Errorf("delete marker %q: %w", path, err)
}

// parseMarker decodes a marker file's bytes. Missing fields decode to zero
// values; callers that care about completeness (e.g., empty WorkflowID)
// should validate after parsing.
func parseMarker(body []byte) (Marker, error) {
	var m Marker
	if err := json.Unmarshal(body, &m); err != nil {
		return Marker{}, err
	}
	return m, nil
}
