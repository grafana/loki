package bloomshipper

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/pkg/errors"

	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
	"github.com/grafana/loki/pkg/storage/chunk/cache"
)

// TODO(chaudum): Add metric for cache hits/misses, and bytes stored/retrieved
type metrics struct{}

type fetcher interface {
	FetchMetas(ctx context.Context, refs []MetaRef) ([]Meta, error)
	FetchBlocks(ctx context.Context, refs []BlockRef) ([]BlockDirectory, error)
}

// Compiler check to ensure Fetcher implements the fetcher interface
var _ fetcher = &Fetcher{}

type Fetcher struct {
	client Client

	metasCache      cache.Cache
	blocksCache     *cache.EmbeddedCache[string, BlockDirectory]
	localFSResolver KeyResolver

	metrics *metrics
	logger  log.Logger
}

func NewFetcher(cfg bloomStoreConfig, client Client, metasCache cache.Cache, blocksCache *cache.EmbeddedCache[string, BlockDirectory], logger log.Logger) (*Fetcher, error) {
	fetcher := &Fetcher{
		client:          client,
		metasCache:      metasCache,
		blocksCache:     blocksCache,
		localFSResolver: NewPrefixedResolver(cfg.workingDir, defaultKeyResolver{}),
		logger:          logger,
	}
	return fetcher, nil
}

func (f *Fetcher) FetchMetas(ctx context.Context, refs []MetaRef) ([]Meta, error) {
	if ctx.Err() != nil {
		return nil, errors.Wrap(ctx.Err(), "fetch Metas")
	}

	keys := make([]string, 0, len(refs))
	for _, ref := range refs {
		keys = append(keys, f.client.Meta(ref).Addr())
	}
	cacheHits, cacheBufs, _, err := f.metasCache.Fetch(ctx, keys)
	if err != nil {
		return nil, err
	}

	fromCache, missing, err := f.processMetasCacheResponse(ctx, refs, cacheHits, cacheBufs)
	if err != nil {
		return nil, err
	}

	fromStorage, err := f.client.GetMetas(ctx, missing)
	if err != nil {
		return nil, err
	}

	err = f.writeBackMetas(ctx, fromStorage)
	return append(fromCache, fromStorage...), err
}

func (f *Fetcher) processMetasCacheResponse(_ context.Context, refs []MetaRef, keys []string, bufs [][]byte) ([]Meta, []MetaRef, error) {
	found := make(map[string][]byte, len(refs))
	for i, k := range keys {
		found[k] = bufs[i]
	}

	metas := make([]Meta, 0, len(found))
	missing := make([]MetaRef, 0, len(refs)-len(keys))

	var lastErr error
	for i, ref := range refs {
		if raw, ok := found[f.client.Meta(ref).Addr()]; ok {
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
		keys[i] = f.client.Meta(metas[i].MetaRef).Addr()
		data[i], err = json.Marshal(metas[i])
	}
	if err != nil {
		return err
	}
	return f.metasCache.Store(ctx, keys, data)
}

func (f *Fetcher) FetchBlocks(ctx context.Context, refs []BlockRef) ([]BlockDirectory, error) {
	if ctx.Err() != nil {
		return nil, errors.Wrap(ctx.Err(), "fetch Blocks")
	}

	keys := make([]string, 0, len(refs))
	for _, ref := range refs {
		keys = append(keys, f.client.Block(ref).Addr())
	}
	cacheHits, cacheBufs, _, err := f.blocksCache.Fetch(ctx, keys)
	if err != nil {
		return nil, err
	}

	var results []BlockDirectory

	fromCache, missing, err := f.processBlocksCacheResponse(ctx, refs, cacheHits, cacheBufs)
	if err != nil {
		return nil, err
	}
	results = append(results, fromCache...)

	fromLocalFS, missing, err := f.loadBlocksFromFS(ctx, missing)
	if err != nil {
		return nil, err
	}
	results = append(results, fromLocalFS...)

	fromStorage, err := f.client.GetBlocks(ctx, missing)
	if err != nil {
		return nil, err
	}
	results = append(results, fromStorage...)

	err = f.writeBackBlocks(ctx, fromStorage)
	return results, err
}

func (f *Fetcher) processBlocksCacheResponse(_ context.Context, refs []BlockRef, keys []string, entries []BlockDirectory) ([]BlockDirectory, []BlockRef, error) {
	found := make(map[string]BlockDirectory, len(refs))
	for i, k := range keys {
		found[k] = entries[i]
	}

	blockDirs := make([]BlockDirectory, 0, len(found))
	missing := make([]BlockRef, 0, len(refs)-len(keys))

	var lastErr error
	for i, ref := range refs {
		if raw, ok := found[f.client.Block(ref).Addr()]; ok {
			blockDirs = append(blockDirs, raw)
		} else {
			missing = append(missing, refs[i])
		}
	}

	return blockDirs, missing, lastErr
}

func (f *Fetcher) loadBlocksFromFS(_ context.Context, refs []BlockRef) ([]BlockDirectory, []BlockRef, error) {
	blockDirs := make([]BlockDirectory, 0, len(refs))
	missing := make([]BlockRef, 0, len(refs))

	for _, ref := range refs {
		path := f.localFSResolver.Block(ref).LocalPath()
		if ok, clean := f.isBlockDir(path); ok {
			blockDirs = append(blockDirs, NewBlockDirectory(ref, path, f.logger))
		} else {
			_ = clean(path)
			missing = append(missing, ref)
		}
	}

	return blockDirs, missing, nil
}

var noopClean = func(string) error { return nil }

func (f *Fetcher) isBlockDir(path string) (bool, func(string) error) {
	info, err := os.Stat(path)
	if err != nil && os.IsNotExist(err) {
		level.Warn(f.logger).Log("msg", "path does not exist", "path", path)
		return false, noopClean
	}
	if !info.IsDir() {
		return false, os.Remove
	}
	for _, file := range []string{
		filepath.Join(path, v1.BloomFileName),
		filepath.Join(path, v1.SeriesFileName),
	} {
		if _, err := os.Stat(file); err != nil && os.IsNotExist(err) {
			level.Warn(f.logger).Log("msg", "path does not contain required file", "path", path, "file", file)
			return false, os.RemoveAll
		}
	}
	return true, nil
}

func (f *Fetcher) writeBackBlocks(ctx context.Context, blocks []BlockDirectory) error {
	keys := make([]string, len(blocks))
	for i := range blocks {
		keys[i] = f.client.Block(blocks[i].BlockRef).Addr()
	}
	return f.blocksCache.Store(ctx, keys, blocks)
}
