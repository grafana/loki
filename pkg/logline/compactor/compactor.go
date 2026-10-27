package compactor

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/go-kit/log/level"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/v3/pkg/logline"
	"github.com/grafana/loki/v3/pkg/logline/format"
	schedulerapi "github.com/grafana/loki/v3/pkg/logline/scheduler/api"
	"github.com/grafana/loki/v3/pkg/logline/store"
)

// compact executes a compaction job: opens source indexes via range reads,
// merges them directly from object storage, and streams the merged output
// into the destination object. No local file is materialised.
// The context carries the job's hard deadline; any phase that exceeds it
// returns a context error so executeJob can record it as expiry.
func (s *Service) compact(ctx context.Context, job *schedulerapi.JobResponse) error {
	if len(job.SourceIDs) < 2 {
		return fmt.Errorf("job %s requires at least 2 source_ids, got %d", job.ID, len(job.SourceIDs))
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	compactStart := time.Now()
	level.Info(s.logger).Log(
		"msg", "compaction started",
		"job_id", job.ID,
		"sources", len(job.SourceIDs),
		"expires_at", job.ExpiresAt,
	)

	// Phase 1: fetch source metas and open range readers.
	readers, sizes, sourceMetas, err := s.prepareSourceReaders(ctx, job)
	if err != nil {
		return fmt.Errorf("prepare source readers: %w", err)
	}

	if err := verifyCompactionJob(sourceMetas); err != nil {
		return fmt.Errorf("verify compaction job: %w", err)
	}

	// Guard: deadline may have fired during metadata fetch.
	if err := ctx.Err(); err != nil {
		return err
	}

	// Phase 2: build the merged meta skeleton. Hash, SizeBytes, and
	// IndexHeader are populated during the streaming merge below.
	storageID, err := store.NewStorageID()
	if err != nil {
		return fmt.Errorf("generate storage id: %w", err)
	}

	mergedMeta, err := s.buildMergedMetaSkeleton(storageID, job.SourceIDs, sourceMetas)
	if err != nil {
		return fmt.Errorf("build merged meta skeleton: %w", err)
	}

	// Phase 3: streaming merge + upload. The merge writes directly into the
	// object-storage upload pipe; hash and size are computed from the stream.
	// A v5 merge checks DayStart against its inputs' footers.
	dayStart, err := time.Parse(time.DateOnly, sourceMetas[0].Date)
	if err != nil {
		return fmt.Errorf("parse source date %q: %w", sourceMetas[0].Date, err)
	}
	writerCfg := &format.WriterConfig{
		Encoding:          s.postingsEncoding,
		DensityThreshold:  float32(s.cfg.DensityThreshold),
		DocumentInterval:  sourceMetas[0].DocumentInterval,
		DocumentShardBits: sourceMetas[0].DocumentShardBits,
		DayStart:          dayStart,
	}
	merger, err := logline.NewMerger(sourceMetas[0].Version, writerCfg)
	if err != nil {
		return fmt.Errorf("create merger: %w", err)
	}

	mergeStart := time.Now()
	var mergeDuration time.Duration
	err = s.store.PutIndexStreaming(ctx, &mergedMeta, func(w io.Writer) (format.HeaderInfo, error) {
		info, mergeErr := merger.Merge(ctx, readers, sizes, w)
		mergeDuration = time.Since(mergeStart)
		return info, mergeErr
	})
	if err != nil {
		return fmt.Errorf("streaming compaction upload: %w", err)
	}
	s.metrics.mergeDuration.Observe(mergeDuration.Seconds())
	uploadDuration := time.Since(mergeStart) - mergeDuration
	if uploadDuration < 0 {
		uploadDuration = 0
	}
	s.metrics.uploadDuration.Observe(uploadDuration.Seconds())
	s.metrics.bytesUploaded.Add(float64(mergedMeta.SizeBytes))

	// Sum source sizes for compression ratio logging.
	var inputBytes int64
	for _, sz := range sizes {
		inputBytes += sz
	}

	totalDuration := time.Since(compactStart)
	bytesPerSec := float64(inputBytes) / totalDuration.Seconds()
	level.Info(s.logger).Log(
		"msg", "compaction complete",
		"job_id", job.ID,
		"merged_id", mergedMeta.ID(),
		"sources", len(job.SourceIDs),
		"input_bytes", inputBytes,
		"output_bytes", mergedMeta.SizeBytes,
		"duration", totalDuration,
		"bytes_per_sec", int64(bytesPerSec),
	)
	return nil
}

// verifyCompactionJob checks that all source metas share the same date, shard
// configuration, and resolved format version. The scheduler guarantees this
// via the compaction key, but we verify defensively — merging indexes with
// different parameters would produce a corrupt index.
func verifyCompactionJob(metas []store.Meta) error {
	if len(metas) < 2 {
		return nil
	}
	if err := logline.ValidateVersion(metas[0].Version); err != nil {
		return fmt.Errorf("source 0 version: %w", err)
	}
	first := metas[0]
	for i, m := range metas[1:] {
		if err := logline.ValidateVersion(m.Version); err != nil {
			return fmt.Errorf("source %d version: %w", i+1, err)
		}
		if m.Version != first.Version {
			return fmt.Errorf("source %d version=%q differs from source 0 version=%q", i+1, m.Version, first.Version)
		}
		if m.Date != first.Date {
			return fmt.Errorf("source %d date=%q differs from source 0 date=%q", i+1, m.Date, first.Date)
		}
		if m.ShardCount != first.ShardCount {
			return fmt.Errorf("source %d shard_count=%d differs from source 0 shard_count=%d", i+1, m.ShardCount, first.ShardCount)
		}
		if m.ShardAlgorithm != first.ShardAlgorithm {
			return fmt.Errorf("source %d shard_algorithm=%q differs from source 0 shard_algorithm=%q", i+1, m.ShardAlgorithm, first.ShardAlgorithm)
		}
		if m.ShardValue != first.ShardValue {
			return fmt.Errorf("source %d shard_value=%d differs from source 0 shard_value=%d", i+1, m.ShardValue, first.ShardValue)
		}
		if m.DocumentInterval != first.DocumentInterval {
			return fmt.Errorf("source %d document_interval=%v differs from source 0 document_interval=%v", i+1, m.DocumentInterval, first.DocumentInterval)
		}
		if m.DocumentShardBits != first.DocumentShardBits {
			return fmt.Errorf("source %d document_shard_bits=%d differs from source 0 document_shard_bits=%d", i+1, m.DocumentShardBits, first.DocumentShardBits)
		}
	}
	return nil
}

// prepareSourceReaders fetches source metas and sizes concurrently, then opens
// an io.ReaderAt backed by range reads for each source index. Returns readers,
// their sizes, and the parsed metas (for time-bound union). Stops on any error.
func (s *Service) prepareSourceReaders(ctx context.Context, job *schedulerapi.JobResponse) ([]io.ReaderAt, []int64, []store.Meta, error) {
	n := len(job.SourceIDs)
	readers := make([]io.ReaderAt, n)
	sizes := make([]int64, n)
	sourceMetas := make([]store.Meta, n)

	fetchStart := time.Now()
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(s.cfg.Compaction.DownloadConcurrency)

	for i, id := range job.SourceIDs {
		g.Go(func() error {
			m, err := s.store.GetMeta(gctx, id)
			if err != nil {
				return fmt.Errorf("get meta %s: %w", id, err)
			}
			sourceMetas[i] = m

			sizes[i] = m.SizeBytes
			readers[i] = s.store.GetIndexReadAheadReaderAt(ctx, m, 0)

			level.Debug(s.logger).Log("msg", "prepared source reader", "id", id, "bytes", m.SizeBytes)
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, nil, nil, err
	}

	var totalBytes int64
	for _, sz := range sizes {
		totalBytes += sz
	}
	level.Info(s.logger).Log("msg", "prepared all source readers", "job_id", job.ID, "count", n, "total_bytes", totalBytes, "duration", time.Since(fetchStart))
	return readers, sizes, sourceMetas, nil
}

// buildMergedMetaSkeleton constructs the Meta for the merged index with every
// field except Hash, SizeBytes, and IndexHeader populated. Those are filled
// in by PutIndexStreaming (Hash/SizeBytes) and the merge callback (IndexHeader).
// Time bounds are the union of all source metas.
func (s *Service) buildMergedMetaSkeleton(storageID string, sourceIDs []string, sourceMetas []store.Meta) (store.Meta, error) {
	// Extract date from the first source ID — the scheduler guarantees all
	// sources share the same partition date.
	date := strings.SplitN(sourceIDs[0], "/", 2)[0]

	// Union time bounds across all sources.
	minLogTs := sourceMetas[0].MinLogTs
	maxLogTs := sourceMetas[0].MaxLogTs
	minRecordTs := sourceMetas[0].MinRecordTs
	maxRecordTs := sourceMetas[0].MaxRecordTs

	for _, m := range sourceMetas[1:] {
		if m.MinLogTs.Before(minLogTs) {
			minLogTs = m.MinLogTs
		}
		if m.MaxLogTs.After(maxLogTs) {
			maxLogTs = m.MaxLogTs
		}
		if m.MinRecordTs.Before(minRecordTs) {
			minRecordTs = m.MinRecordTs
		}
		if m.MaxRecordTs.After(maxRecordTs) {
			maxRecordTs = m.MaxRecordTs
		}
	}

	// Shard and document layout fields are inherited from the first source —
	// the planner guarantees all sources in a job share these values.
	return store.Meta{
		StorageID:         storageID,
		Date:              date,
		Version:           sourceMetas[0].Version,
		MinLogTs:          minLogTs,
		MaxLogTs:          maxLogTs,
		MinRecordTs:       minRecordTs,
		MaxRecordTs:       maxRecordTs,
		CompactedFrom:     sourceIDs,
		ShardCount:        sourceMetas[0].ShardCount,
		ShardAlgorithm:    sourceMetas[0].ShardAlgorithm,
		ShardValue:        sourceMetas[0].ShardValue,
		DocumentInterval:  sourceMetas[0].DocumentInterval,
		DocumentShardBits: sourceMetas[0].DocumentShardBits,
	}, nil
}
