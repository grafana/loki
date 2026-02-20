package indexcompactor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/thanos-io/objstore"
)

const checkpointKey = ".checkpoint.json"

// Checkpoint tracks compaction state across runs.
//
// CompactedPaths is the cumulative set of all source paths successfully
// compacted in prior runs, used to avoid re-processing the same sources.
//
// OutputPaths is the set of object paths produced by prior compaction runs.
// When one of these appears as input to a new run (re-compaction), the old
// output is deleted and its TOC entries are removed.
//
// The remaining fields track progress within the current run for crash
// recovery.
type Checkpoint struct {
	CompactedPaths map[string]struct{} `json:"compacted_paths"`
	OutputPaths    map[string]struct{} `json:"output_paths"`

	ProcessedPaths  map[string]struct{}  `json:"processed_paths"`
	Intermediates   []IntermediateRecord `json:"intermediates"`
	ScatterComplete bool                 `json:"scatter_complete"`
}

type IntermediateRecord struct {
	Key    string      `json:"key"`
	Window time.Time   `json:"window"`
	TOC    []TOCRecord `json:"toc"`
}

type TOCRecord struct {
	Path    string    `json:"path"`
	Tenant  string    `json:"tenant"`
	MinTime time.Time `json:"min_time"`
	MaxTime time.Time `json:"max_time"`
}

func newCheckpoint() *Checkpoint {
	return &Checkpoint{
		CompactedPaths: make(map[string]struct{}),
		OutputPaths:    make(map[string]struct{}),
		ProcessedPaths: make(map[string]struct{}),
	}
}

// finalizeRun promotes the current run's processed paths into the permanent
// compacted set, updates the output paths, and resets run-specific state.
func (cp *Checkpoint) finalizeRun(newOutputPaths, recompactedOutputs map[string]struct{}) {
	for p := range cp.ProcessedPaths {
		cp.CompactedPaths[p] = struct{}{}
	}
	for p := range recompactedOutputs {
		delete(cp.OutputPaths, p)
	}
	for p := range newOutputPaths {
		cp.OutputPaths[p] = struct{}{}
	}
	cp.ProcessedPaths = make(map[string]struct{})
	cp.Intermediates = nil
	cp.ScatterComplete = false
}

// loadCheckpoint reads the checkpoint from the bucket.
// Returns nil if the object does not exist.
func loadCheckpoint(ctx context.Context, bucket objstore.Bucket) (*Checkpoint, error) {
	r, err := bucket.Get(ctx, checkpointKey)
	if err != nil {
		if bucket.IsObjNotFoundErr(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading checkpoint: %w", err)
	}
	defer r.Close()

	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("reading checkpoint data: %w", err)
	}

	var cp Checkpoint
	if err := json.Unmarshal(data, &cp); err != nil {
		return nil, fmt.Errorf("parsing checkpoint: %w", err)
	}
	if cp.CompactedPaths == nil {
		cp.CompactedPaths = make(map[string]struct{})
	}
	if cp.OutputPaths == nil {
		cp.OutputPaths = make(map[string]struct{})
	}
	if cp.ProcessedPaths == nil {
		cp.ProcessedPaths = make(map[string]struct{})
	}
	return &cp, nil
}

// saveCheckpoint writes the checkpoint to the bucket.
func saveCheckpoint(ctx context.Context, bucket objstore.Bucket, cp *Checkpoint) error {
	data, err := json.MarshalIndent(cp, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling checkpoint: %w", err)
	}
	if err := bucket.Upload(ctx, checkpointKey, bytes.NewReader(data)); err != nil {
		return fmt.Errorf("uploading checkpoint: %w", err)
	}
	return nil
}
