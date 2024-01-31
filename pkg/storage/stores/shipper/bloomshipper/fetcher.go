package bloomshipper

import (
	"context"
	"encoding/json"
	"io"

	"github.com/go-kit/log"
	"github.com/pkg/errors"

	"github.com/grafana/loki/pkg/storage/chunk/cache"
)

// TODO(chaudum): Add metric for cache hits/misses, and bytes stored/retrieved
type metrics struct{}

type fetcher interface {
	FetchMetas(ctx context.Context, refs []MetaRef) ([]Meta, error)
	// TODO(chaudum): Integrate block fetching
	// FetchBlocks(ctx context.Context, refs []BlockRef) ([]Block, error)
}

type Fetcher struct {
	client Client

	metasCache  cache.Cache
	blocksCache *cache.EmbeddedCache[string, io.ReadCloser]

	metrics *metrics
	logger  log.Logger
}

func NewFetcher(client Client, metasCache cache.Cache, blocksCache *cache.EmbeddedCache[string, io.ReadCloser], logger log.Logger) (*Fetcher, error) {
	return &Fetcher{
		client:      client,
		metasCache:  metasCache,
		blocksCache: blocksCache,
		logger:      logger,
	}, nil
}

func (f *Fetcher) FetchMetas(ctx context.Context, refs []MetaRef) ([]Meta, error) {
	if ctx.Err() != nil {
		return nil, errors.Wrap(ctx.Err(), "fetch Metas")
	}

	keys := make([]string, 0, len(refs))
	for _, ref := range refs {
		keys = append(keys, externalMetaKey(ref))
	}
	cacheHits, cacheBufs, _, err := f.metasCache.Fetch(ctx, keys)
	if err != nil {
		return nil, err
	}

	fromCache, missing, err := f.processCacheResponse(ctx, refs, cacheHits, cacheBufs)
	if err != nil {
		return nil, err
	}

	fromStorage, err := f.client.GetMetas(ctx, missing)
	if err != nil {
		return nil, err
	}

	// TODO(chaudum): Make async
	err = f.writeBackMetas(ctx, fromStorage)
	return append(fromCache, fromStorage...), err
}

func (f *Fetcher) processCacheResponse(_ context.Context, refs []MetaRef, keys []string, bufs [][]byte) ([]Meta, []MetaRef, error) {

	found := make(map[string][]byte, len(refs))
	for i, k := range keys {
		found[k] = bufs[i]
	}

	metas := make([]Meta, 0, len(found))
	missing := make([]MetaRef, 0, len(refs)-len(keys))

	var lastErr error
	for i, ref := range refs {
		if raw, ok := found[externalMetaKey(ref)]; ok {
			meta := Meta{
				MetaRef: ref,
			}
			lastErr = json.Unmarshal(raw, &meta)
			metas = append(metas, meta)
		} else {
			missing = append(missing, refs[i])
		}
	}

	return metas, missing, lastErr
}

func (f *Fetcher) writeBackMetas(ctx context.Context, metas []Meta) error {
	var err error
	keys := make([]string, len(metas))
	data := make([][]byte, len(metas))
	for i := range metas {
		keys[i] = externalMetaKey(metas[i].MetaRef)
		data[i], err = json.Marshal(metas[i])
	}
	if err != nil {
		return err
	}
	return f.metasCache.Store(ctx, keys, data)
}

// TODO(chaudum): Integrate block fetching

// func (f *Fetcher) FetchBlocks(ctx context.Context, refs []BlockRef) (v1.Iterator[Block], error) {
// 	if ctx.Err() != nil {
// 		return nil, errors.Wrap(ctx.Err(), "fetch Blocks")
// 	}

// 	keys := make([]string, 0, len(refs))
// 	for _, ref := range refs {
// 		keys = append(keys, externalBlockKey(ref))
// 	}
// 	found, blocksFromCache, missing, err := f.blocksCache.Fetch(ctx, keys)
// 	if err != nil {
// 		return nil, err
// 	}

// 	if len(missing) > 0 {
// 		for _, key := range missing {
// 			for i, ref := range refs {
// 				if key == externalBlockKey(ref) {
// 					refs = append(refs[:i], refs[i+1:]...)
// 					i--
// 				}
// 			}
// 		}

// 		blocksFromStorage, err := f.client.GetBlock(ctx, refs)
// 		if err != nil {
// 			return nil, err
// 		}
// 	}

// 	return nil, nil
// }

// func (f *Fetcher) writeBackBlocks(ctx context.Context, blocks []Block) error {
// 	keys := make([]string, 0, len(blocks))
// 	data := make([]io.ReadCloser, 0, len(blocks))
// 	return f.blocksCache.Store(ctx, keys, data)
// }

// type ChannelIter[T any] struct {
// 	ch  <-chan T
// 	cur T
// }

// func NewChannelIter[T any](ch <-chan T) *ChannelIter[T] {
// 	return &ChannelIter[T]{
// 		ch: ch,
// 	}
// }

// func (it *ChannelIter[T]) Next() bool {
// 	el, ok := <-it.ch
// 	if ok {
// 		it.cur = el
// 		return true
// 	}
// 	return false
// }

// func (it *ChannelIter[T]) At() T {
// 	return it.cur
// }

// func (it *ChannelIter[T]) Err() error {
// 	return nil
// }
