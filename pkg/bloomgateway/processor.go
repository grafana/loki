package bloomgateway

import (
	"context"
	"math"
	"sort"

	"github.com/go-kit/log"
	"github.com/prometheus/common/model"

	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
	"github.com/grafana/loki/pkg/storage/stores/shipper/bloomshipper"
)

type tasksForBlock struct {
	blockRef bloomshipper.BlockRef
	tasks    []Task
}

type blockLoader interface {
	LoadBlocks(context.Context, []bloomshipper.BlockRef) (v1.Iterator[bloomshipper.BlockQuerierWithFingerprintRange], error)
}

type store interface {
	blockLoader
	bloomshipper.Store
}

type processor struct {
	store  store
	logger log.Logger
}

func (p *processor) run(ctx context.Context, tasks []Task) error {
	for ts, tasks := range group(tasks, func(t Task) model.Time { return t.day }) {
		interval := bloomshipper.NewInterval(ts, ts.Add(Day))
		tenant := tasks[0].Tenant
		err := p.processTasks(ctx, tenant, interval, []v1.FingerprintBounds{{Min: 0, Max: math.MaxUint64}}, tasks)
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

func (p *processor) processTasks(ctx context.Context, tenant string, interval bloomshipper.Interval, keyspaces []v1.FingerprintBounds, tasks []Task) error {
	minFpRange, maxFpRange := getFirstLast(keyspaces)
	metaSearch := bloomshipper.MetaSearchParams{
		TenantID: tenant,
		Interval: interval,
		Keyspace: v1.FingerprintBounds{Min: minFpRange.Min, Max: maxFpRange.Max},
	}
	metas, err := p.store.FetchMetas(ctx, metaSearch)
	if err != nil {
		return err
	}
	blocksRefs := bloomshipper.BlocksForMetas(metas, interval, keyspaces)
	return p.processBlocks(ctx, partition(tasks, blocksRefs))
}

func (p *processor) processBlocks(ctx context.Context, data []tasksForBlock) error {
	refs := make([]bloomshipper.BlockRef, len(data))
	for _, block := range data {
		refs = append(refs, block.blockRef)
	}

	blockIter, err := p.store.LoadBlocks(ctx, refs)
	if err != nil {
		return err
	}

outer:
	for blockIter.Next() {
		bq := blockIter.At()
		for i, block := range data {
			if block.blockRef.Bounds().Equal(bq.FingerprintBounds) {
				err := p.processBlock(ctx, bq.BlockQuerier, block.tasks)
				if err != nil {
					return err
				}
				data = append(data[:i], data[i+1:]...)
				continue outer
			}
		}
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
	return fq.Run()
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

func partition(tasks []Task, blocks []bloomshipper.BlockRef) []tasksForBlock {
	result := make([]tasksForBlock, 0, len(blocks))

	for _, block := range blocks {
		bounded := tasksForBlock{
			blockRef: block,
		}

		for _, task := range tasks {
			refs := task.series
			min := sort.Search(len(refs), func(i int) bool {
				return block.Cmp(refs[i].Fingerprint) > v1.Before
			})

			max := sort.Search(len(refs), func(i int) bool {
				return block.Cmp(refs[i].Fingerprint) == v1.After
			})

			// All fingerprints fall outside of the consumer's range
			if min == len(refs) || max == 0 {
				continue
			}

			bounded.tasks = append(bounded.tasks, task.Copy(refs[min:max]))
		}

		if len(bounded.tasks) > 0 {
			result = append(result, bounded)
		}

	}
	return result
}
