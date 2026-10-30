package scheduler

import (
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"
	"time"

	schedulerapi "github.com/grafana/loki/v4/pkg/logline/scheduler/api"
)

// Job represents a compaction job assigned to a worker.
type Job struct {
	ID        string    `json:"id"`
	SourceIDs []string  `json:"source_ids"`
	WorkerID  string    `json:"worker_id"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

// Response converts a Job to the shared API wire type, omitting internal fields.
func (j *Job) Response() schedulerapi.JobResponse {
	return schedulerapi.JobResponse{
		ID:        j.ID,
		SourceIDs: j.SourceIDs,
		ExpiresAt: j.ExpiresAt,
	}
}

// jobID returns a deterministic ID derived from the sorted source IDs.
// Identical source sets always produce the same ID.
func jobID(sourceIDs []string) string {
	sorted := make([]string, len(sourceIDs))
	copy(sorted, sourceIDs)
	sort.Strings(sorted)
	h := sha256.Sum256([]byte(strings.Join(sorted, ",")))
	return fmt.Sprintf("%x", h[:8])
}
