package builder

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/v3/pkg/logline"
	"github.com/grafana/loki/v3/pkg/logline/store"
)

// uploadPartialIndexes uploads the cycle's .lidx files to the index store.
// StorageIDs are generated during prepare so retries reuse the same object paths.
// The producing Builder owns the read handles and files; each upload attempt
// seeks to the start, and only Clear or the next Prepare cleans up the files.
func uploadPartialIndexes(ctx context.Context, indexStore *store.Store, cfg logline.IndexConfig, files []fileInfo, logger log.Logger, metrics *Metrics) error {
	g, ctx := errgroup.WithContext(ctx)

	for _, f := range files {

		g.Go(func() error {
			uploadStart := time.Now()

			if _, err := f.file.Seek(0, io.SeekStart); err != nil {
				return fmt.Errorf("failed to seek index file: %w", err)
			}

			meta := store.Meta{
				StorageID:         f.storageID,
				Date:              f.date,
				Version:           cfg.Version,
				MinLogTs:          f.minLogTs,
				MaxLogTs:          f.maxLogTs,
				MinRecordTs:       f.minEnqueuedTime,
				MaxRecordTs:       f.maxEnqueuedTime,
				ShardCount:        cfg.ShardCount,
				ShardAlgorithm:    cfg.ShardAlgorithm,
				ShardValue:        f.shardValue,
				DocumentInterval:  cfg.DocumentInterval,
				DocumentShardBits: cfg.DocumentShardBits,
			}
			if err := meta.SetFileInfo(f.file); err != nil {
				return fmt.Errorf("failed to populate file info: %w", err)
			}
			header, err := logline.ReadHeaderAt(f.file, meta.SizeBytes)
			if err != nil {
				return fmt.Errorf("failed to read index header: %w", err)
			}
			meta.IndexHeader = &header

			exists, err := indexStore.IndexExists(ctx, meta)
			if err != nil {
				return fmt.Errorf("failed to check index existence: %w", err)
			}
			if exists {
				level.Info(logger).Log("msg", "index already exists, skipping upload", "path", meta.IndexPath())
				metrics.uploadDuration.Observe(time.Since(uploadStart).Seconds())
				return nil
			}

			if err := indexStore.PutIndex(ctx, f.file, meta); err != nil {
				return fmt.Errorf("failed to upload index: %w", err)
			}

			level.Info(logger).Log(
				"msg", "uploaded index",
				"path", meta.IndexPath(),
				"date", f.date,
				"min_log_ts", f.minLogTs.Format(time.RFC3339),
				"max_log_ts", f.maxLogTs.Format(time.RFC3339),
				"min_rec_ts", f.minEnqueuedTime.Format(time.RFC3339),
				"max_rec_ts", f.maxEnqueuedTime.Format(time.RFC3339),
				"duration", time.Since(uploadStart),
			)
			metrics.uploadDuration.Observe(time.Since(uploadStart).Seconds())
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return fmt.Errorf("partial upload failure: %w", err)
	}
	return nil
}
