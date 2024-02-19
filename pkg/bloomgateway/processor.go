package bloomgateway

import (
	"context"
	"math"
	"time"

	"github.com/go-kit/log"

	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
	"github.com/grafana/loki/pkg/storage/config"
	"github.com/grafana/loki/pkg/storage/stores/shipper/bloomshipper"
)

func newProcessor(id string, store bloomshipper.Store, logger log.Logger, metrics *workerMetrics) *processor {
	return &processor{
		id:      id,
		store:   store,
		logger:  logger,
		metrics: metrics,
	}
}

type processor struct {
	id      string
	store   bloomshipper.Store
	logger  log.Logger
	metrics *workerMetrics
}

func (p *processor) run(ctx context.Context, tasks []Task) error {
	for ts, tasks := range group(tasks, func(t Task) config.DayTime { return t.table }) {
		tenant := tasks[0].Tenant
		err := p.processTasks(ctx, tenant, ts, []v1.FingerprintBounds{{Min: 0, Max: math.MaxUint64}}, tasks)
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

func (p *processor) processTasks(ctx context.Context, tenant string, day config.DayTime, keyspaces []v1.FingerprintBounds, tasks []Task) error {
	minFpRange, maxFpRange := getFirstLast(keyspaces)
	interval := bloomshipper.NewInterval(day.Bounds())
	metaSearch := bloomshipper.MetaSearchParams{
		TenantID: tenant,
		Interval: interval,
		Keyspace: v1.FingerprintBounds{Min: minFpRange.Min, Max: maxFpRange.Max},
	}
	metas, err := p.store.FetchMetas(ctx, metaSearch)
	if err != nil {
		return err
	}
	p.metrics.metasFetched.WithLabelValues(p.id).Observe(float64(len(metas)))

	blocksRefs := bloomshipper.BlocksForMetas(metas, interval, keyspaces)
	return p.processBlocks(ctx, partitionTasks(tasks, blocksRefs))
}

func (p *processor) processBlocks(ctx context.Context, data []blockWithTasks) error {
	refs := make([]bloomshipper.BlockRef, len(data))
	for _, block := range data {
		refs = append(refs, block.ref)
	}

	bqs, err := p.store.FetchBlocks(ctx, refs)
	if err != nil {
		return err
	}
	p.metrics.blocksFetched.WithLabelValues(p.id).Observe(float64(len(bqs)))

	blockIter := v1.NewSliceIter(bqs)

outer:
	for blockIter.Next() {
		bq := blockIter.At()
		for i, block := range data {
			if block.ref.Bounds.Equal(bq.Bounds) {
				err := p.processBlock(ctx, bq.BlockQuerier, block.tasks)
				bq.Close()
				if err != nil {
					return err
				}
				data = append(data[:i], data[i+1:]...)
				continue outer
			}
		}
		// should not happen, but close anyway
		bq.Close()
	}
	return nil
}

func (p *processor) processBlock(_ context.Context, blockQuerier *v1.BlockQuerier, tasks []Task) error {
	schema, err := blockQuerier.Schema()
	if err != nil {
		return err
	}

	tokenizer := v1.NewNGramTokenizer(schema.NGramLen(), 0)
	iters := make([]v1.PeekingIterator[v1.Request], 0, len(tasks))
	for _, task := range tasks {
		it := v1.NewPeekingIter(task.RequestIter(tokenizer))
		iters = append(iters, it)
	}

	fq := blockQuerier.Fuse(iters)

	start := time.Now()
	err = fq.Run()
	if err != nil {
		p.metrics.blockQueryLatency.WithLabelValues(p.id, labelFailure).Observe(time.Since(start).Seconds())
	} else {
		p.metrics.blockQueryLatency.WithLabelValues(p.id, labelSuccess).Observe(time.Since(start).Seconds())
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
