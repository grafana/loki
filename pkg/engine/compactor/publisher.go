package compactor

import (
	"context"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/thanos-io/objstore"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
)

// fileSizeStatConcurrency bounds concurrent bucket.Attributes calls when
// filling in index FileSize before a ToC replace.
const fileSizeStatConcurrency = 16

// tocReplacer is the subset of *metastore.TableOfContentsWriter the
// publisher needs.
type tocReplacer interface {
	ReplaceIndexPointers(
		ctx context.Context,
		window time.Time,
		tenant string,
		oldPaths []string,
		newEntries []metastore.TableOfContentsEntry,
	) (bool, error)
}

// tocPublisher commits the outputs of completed compaction tasks to a window's
// ToC. It is the single place that owns dry-run handling, FileSize backfill,
// the replace timeout, and race-loss semantics, so every phase publishes with
// identical behaviour.
type tocPublisher struct {
	writer  tocReplacer
	bucket  objstore.Bucket
	logger  log.Logger
	metrics *coordinatorMetrics
	timeout time.Duration
	dryRun  bool
}

func newTocPublisher(cfg Config, writer tocReplacer, bucket objstore.Bucket, logger log.Logger, metrics *coordinatorMetrics) *tocPublisher {
	return &tocPublisher{
		writer:  writer,
		bucket:  bucket,
		logger:  logger,
		metrics: metrics,
		timeout: cfg.ToCConsolidateTimeout,
		dryRun:  cfg.DryRun,
	}
}

// replace atomically replaces oldPaths with newEntries in the tenant's ToC for
// window. dispatched is the number of tasks that produced newEntries and is
// reported in the returned stats.
//
// Outcomes:
//   - dry run: nothing is written; stats report only dispatched.
//   - race loss (the ToC no longer references oldPaths): zero stats, nil error.
//   - success: stats report removed, added, and dispatched.
func (p *tocPublisher) replace(
	ctx context.Context,
	tenant string,
	window time.Time,
	oldPaths []string,
	newEntries []metastore.TableOfContentsEntry,
	dispatched int,
) (compactionStats, error) {
	if p.dryRun {
		return compactionStats{dispatched: dispatched}, nil
	}

	p.fillFileSizes(ctx, newEntries)

	replaceCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	swapped, err := p.writer.ReplaceIndexPointers(replaceCtx, window, tenant, oldPaths, newEntries)
	if err != nil {
		return compactionStats{}, err
	}
	if !swapped {
		level.Debug(p.logger).Log("msg", "ToC replace race-loss", "tenant", tenant, "window", window)
		return compactionStats{}, nil
	}
	return compactionStats{
		removed:    len(oldPaths),
		added:      len(newEntries),
		dispatched: dispatched,
	}, nil
}

// fillFileSizes stats each entry's object and sets FileSize. Best-effort: when
// the stat fails (missing or not-yet-visible object) the entry keeps its zero
// FileSize and is persisted as-is.
//
// Stats run concurrently (bounded by fileSizeStatConcurrency) because each
// bucket.Attributes call can take tens of milliseconds; serializing hundreds
// of entries would dominate the cycle. Each goroutine writes a distinct slice
// element, so the concurrent writes do not race.
func (p *tocPublisher) fillFileSizes(ctx context.Context, entries []metastore.TableOfContentsEntry) {
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(fileSizeStatConcurrency)
	for i := range entries {
		g.Go(func() error {
			start := time.Now()
			attrs, err := p.bucket.Attributes(gctx, entries[i].Path)
			p.metrics.observeFileSizeStat(time.Since(start))
			if err != nil {
				level.Warn(p.logger).Log("msg", "attributes for output failed", "path", entries[i].Path, "err", err)
				return nil
			}
			if attrs.Size > 0 {
				entries[i].FileSize = uint64(attrs.Size)
			}
			return nil
		})
	}
	_ = g.Wait()
}
