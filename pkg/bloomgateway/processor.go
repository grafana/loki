package bloomgateway

import (
	"context"
	"math"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/opentracing/opentracing-go"
	"github.com/pkg/errors"

	"github.com/grafana/dskit/concurrency"

	v1 "github.com/grafana/loki/v3/pkg/storage/bloom/v1"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/bloomshipper"
)

func newProcessor(id string, concurrency int, store bloomshipper.Store, logger log.Logger, metrics *workerMetrics) *processor {
	return &processor{
		id:          id,
		concurrency: concurrency,
		store:       store,
		logger:      logger,
		metrics:     metrics,
	}
}

type processor struct {
	id          string
	concurrency int // concurrency at which bloom blocks are processed
	store       bloomshipper.Store
	logger      log.Logger
	metrics     *workerMetrics
}

func (p *processor) run(ctx context.Context, tasks []Task) error {
	return p.runWithBounds(ctx, tasks, v1.MultiFingerprintBounds{{Min: 0, Max: math.MaxUint64}})
}

func (p *processor) runWithBounds(ctx context.Context, tasks []Task, bounds v1.MultiFingerprintBounds) error {
	tenant := tasks[0].Tenant
	level.Info(p.logger).Log(
		"msg", "process tasks with bounds",
		"tenant", tenant,
		"tasks", len(tasks),
		"bounds", len(bounds),
	)

	for ts, tasks := range group(tasks, func(t Task) config.DayTime { return t.table }) {
		err := p.processTasks(ctx, tenant, ts, bounds, tasks)
		if err != nil {
			for _, task := range tasks {
				task.CloseWithError(err)
			}
			return err
		}
		for _, task := range tasks {
			task.Close()
		}
	}
	return nil
}

func (p *processor) processTasks(ctx context.Context, tenant string, day config.DayTime, _ v1.MultiFingerprintBounds, tasks []Task) error {
	level.Info(p.logger).Log("msg", "process tasks for day", "tenant", tenant, "tasks", len(tasks), "day", day.String())
	var duration time.Duration

	blocksRefs := make([]bloomshipper.BlockRef, 0, len(tasks[0].blocks)*len(tasks))
	for _, task := range tasks {
		blocksRefs = append(blocksRefs, task.blocks...)
	}

	data := partitionTasks(tasks, blocksRefs)

	refs := make([]bloomshipper.BlockRef, 0, len(data))
	for _, block := range data {
		refs = append(refs, block.ref)
	}

	startBlocks := time.Now()
	bqs, err := p.store.FetchBlocks(
		ctx,
		refs,
		bloomshipper.WithFetchAsync(true),
		bloomshipper.WithIgnoreNotFound(true),
		// NB(owen-d): we relinquish bloom pages to a pool
		// after iteration for performance (alloc reduction).
		// This is safe to do here because we do not capture
		// the underlying bloom []byte outside of iteration
		bloomshipper.WithPool(true),
	)
	duration = time.Since(startBlocks)
	level.Debug(p.logger).Log("msg", "fetched blocks", "count", len(refs), "duration", duration, "err", err)

	for _, t := range tasks {
		FromContext(t.ctx).AddBlocksFetchTime(duration)
	}

	if err != nil {
		return err
	}

	startProcess := time.Now()
	res := p.processBlocks(ctx, bqs, data)
	duration = time.Since(startProcess)

	for _, t := range tasks {
		FromContext(t.ctx).AddProcessingTime(duration)
	}

	return res
}

func (p *processor) processBlocks(ctx context.Context, bqs []*bloomshipper.CloseableBlockQuerier, data []blockWithTasks) error {

	defer func() {
		for i := range bqs {
			if bqs[i] == nil {
				continue
			}
			bqs[i].Close()
		}
	}()

	return concurrency.ForEachJob(ctx, len(bqs), p.concurrency, func(ctx context.Context, i int) error {
		bq := bqs[i]
		if bq == nil {
			// TODO(chaudum): Add metric for skipped blocks
			return nil
		}

		block := data[i]

		if !block.ref.Bounds.Equal(bq.Bounds) {
			return errors.Errorf("block and querier bounds differ: %s vs %s", block.ref.Bounds, bq.Bounds)
		}

		err := p.processBlock(ctx, bq.BlockQuerier, block.tasks)
		if err != nil {
			return errors.Wrap(err, "processing block")
		}
		return nil
	})
}

func (p *processor) processBlock(_ context.Context, blockQuerier *v1.BlockQuerier, tasks []Task) error {
	schema, err := blockQuerier.Schema()
	if err != nil {
		return err
	}

	tokenizer := v1.NewNGramTokenizer(schema.NGramLen(), schema.NGramSkip())
	iters := make([]v1.PeekingIterator[v1.Request], 0, len(tasks))

	for _, task := range tasks {
		if sp := opentracing.SpanFromContext(task.ctx); sp != nil {
			md, _ := blockQuerier.Metadata()
			blk := bloomshipper.BlockRefFrom(task.Tenant, task.table.String(), md)
			sp.LogKV("process block", blk.String(), "series", len(task.series))
		}

		it := v1.NewPeekingIter(task.RequestIter(tokenizer))
		iters = append(iters, it)
	}

	fq := blockQuerier.Fuse(iters, p.logger)

	start := time.Now()
	err = fq.Run()
	duration := time.Since(start)

	if err != nil {
		p.metrics.blockQueryLatency.WithLabelValues(p.id, labelFailure).Observe(duration.Seconds())
	} else {
		p.metrics.blockQueryLatency.WithLabelValues(p.id, labelSuccess).Observe(duration.Seconds())
	}

	for _, task := range tasks {
		stats := FromContext(task.ctx)
		stats.AddTotalProcessingTime(duration)
		stats.IncProcessedBlocks()
	}

	return err
}

// getFirstLast returns the first and last item of a fingerprint slice
// It assumes an ascending sorted list of fingerprints.
func getFirstLast[T any](s []T) (T, T) {
	var zero T
	if len(s) == 0 {
		return zero, zero
	}
	return s[0], s[len(s)-1]
}

func group[K comparable, V any, S ~[]V](s S, f func(v V) K) map[K]S {
	m := make(map[K]S)
	for _, elem := range s {
		m[f(elem)] = append(m[f(elem)], elem)
	}
	return m
}
