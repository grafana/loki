package bloomshipper

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/pkg/errors"
	"k8s.io/utils/keymutex"

	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
	"github.com/grafana/loki/pkg/storage/chunk/cache"
)

// TODO(chaudum): Add metric for cache hits/misses, and bytes stored/retrieved
type metrics struct{}

type fetcher interface {
	FetchMetas(ctx context.Context, refs []MetaRef) ([]Meta, error)
	FetchBlocks(ctx context.Context, refs []BlockRef) ([]*CloseableBlockQuerier, error)
	Close()
}

// Compiler check to ensure Fetcher implements the fetcher interface
var _ fetcher = &Fetcher{}

type Fetcher struct {
	client Client

	metasCache      cache.Cache
	blocksCache     cache.TypedCache[string, BlockDirectory]
	localFSResolver KeyResolver

	q *downloadQueue[BlockRef, BlockDirectory]

	metrics *metrics
	logger  log.Logger
}

func NewFetcher(cfg bloomStoreConfig, client Client, metasCache cache.Cache, blocksCache cache.TypedCache[string, BlockDirectory], logger log.Logger) (*Fetcher, error) {
	fetcher := &Fetcher{
		client:          client,
		metasCache:      metasCache,
		blocksCache:     blocksCache,
		localFSResolver: NewPrefixedResolver(cfg.workingDir, defaultKeyResolver{}),
		logger:          logger,
	}
	fetcher.q = newDownloadQueue[BlockRef, BlockDirectory](1000, cfg.numWorkers, fetcher.processTask, logger)
	return fetcher, nil
}

func (f *Fetcher) Close() {
	f.q.close()
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

func (f *Fetcher) FetchBlocks(ctx context.Context, refs []BlockRef) ([]*CloseableBlockQuerier, error) {
	n := len(refs)

	responses := make(chan downloadResponse[BlockDirectory], n)
	errors := make(chan error, n)
	for i := 0; i < n; i++ {
		f.q.enqueue(downloadRequest[BlockRef, BlockDirectory]{
			ctx:     ctx,
			item:    refs[i],
			key:     f.client.Block(refs[i]).Addr(),
			idx:     i,
			results: responses,
			errors:  errors,
		})
	}

	results := make([]*CloseableBlockQuerier, n)
	for i := 0; i < n; i++ {
		select {
		case err := <-errors:
			return results, err
		case res := <-responses:
			results[res.idx] = res.item.BlockQuerier()
		}
	}

	return results, nil
}

func (f *Fetcher) processTask(ctx context.Context, task downloadRequest[BlockRef, BlockDirectory]) {
	if ctx.Err() != nil {
		task.errors <- ctx.Err()
		return
	}

	result, err := f.fetchBlock(ctx, task.item)
	if err != nil {
		task.errors <- err
		return
	}

	task.results <- downloadResponse[BlockDirectory]{
		item: result,
		key:  task.key,
		idx:  task.idx,
	}
}

// fetchBlock resolves a block from three locations:
//  1. from cache
//  2. from file system
//  3. from remote storage
func (f *Fetcher) fetchBlock(ctx context.Context, ref BlockRef) (BlockDirectory, error) {
	var zero BlockDirectory

	if ctx.Err() != nil {
		return zero, errors.Wrap(ctx.Err(), "fetch block")
	}

	keys := []string{f.client.Block(ref).Addr()}

	_, fromCache, _, err := f.blocksCache.Fetch(ctx, keys)
	if err != nil {
		return zero, err
	}

	// item found in cache
	if len(fromCache) == 1 {
		return fromCache[0], nil
	}

	fromLocalFS, _, err := f.loadBlocksFromFS(ctx, []BlockRef{ref})
	if err != nil {
		return zero, err
	}

	// item found on local file system
	if len(fromLocalFS) == 1 {
		err = f.writeBackBlocks(ctx, fromLocalFS)
		return fromLocalFS[0], err
	}

	fromStorage, err := f.client.GetBlock(ctx, ref)
	if err != nil {
		return zero, err
	}

	// item found in storage
	err = f.writeBackBlocks(ctx, []BlockDirectory{fromStorage})
	return fromStorage, err
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

type processFunc[T any, R any] func(context.Context, downloadRequest[T, R])

type downloadRequest[T any, R any] struct {
	ctx     context.Context
	item    T
	key     string
	idx     int
	results chan<- downloadResponse[R]
	errors  chan<- error
}

type downloadResponse[R any] struct {
	item R
	key  string
	idx  int
}

type downloadQueue[T any, R any] struct {
	queue   chan downloadRequest[T, R]
	mu      keymutex.KeyMutex
	wg      sync.WaitGroup
	done    chan struct{}
	process processFunc[T, R]
	logger  log.Logger
}

func newDownloadQueue[T any, R any](size, workers int, process processFunc[T, R], logger log.Logger) *downloadQueue[T, R] {
	q := &downloadQueue[T, R]{
		queue:   make(chan downloadRequest[T, R], size),
		mu:      keymutex.NewHashed(workers),
		done:    make(chan struct{}),
		process: process,
		logger:  logger,
	}
	for i := 0; i < workers; i++ {
		q.wg.Add(1)
		go q.runWorker()
	}
	return q
}

func (q *downloadQueue[T, R]) enqueue(t downloadRequest[T, R]) {
	q.queue <- t
}

func (q *downloadQueue[T, R]) runWorker() {
	defer q.wg.Done()
	for {
		select {
		case <-q.done:
			return
		case task := <-q.queue:
			q.do(task.ctx, task)
		}
	}
}

func (q *downloadQueue[T, R]) do(ctx context.Context, task downloadRequest[T, R]) {
	q.mu.LockKey(task.key)
	defer func() {
		err := q.mu.UnlockKey(task.key)
		if err != nil {
			level.Error(q.logger).Log("msg", "failed to unlock key in block lock", "key", task.key, "err", err)
		}
	}()

	q.process(ctx, task)
}

func (q *downloadQueue[T, R]) close() {
	close(q.done)
	q.wg.Wait()
}
