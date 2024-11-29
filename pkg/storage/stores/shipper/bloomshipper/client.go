package bloomshipper

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/concurrency"
	"github.com/opentracing/opentracing-go"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/v3/pkg/compression"
	v1 "github.com/grafana/loki/v3/pkg/storage/bloom/v1"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/util"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb"
	"github.com/grafana/loki/v3/pkg/util/encoding"
)

const (
	rootFolder            = "bloom"
	metasFolder           = "metas"
	bloomsFolder          = "blooms"
	delimiter             = "/"
	fileNamePartDelimiter = "-"
)

type Ref struct {
	TenantID                     string
	TableName                    string
	Bounds                       v1.FingerprintBounds
	StartTimestamp, EndTimestamp model.Time
	Checksum                     uint32
}

// Hash hashes the ref
// NB(owen-d): we don't include the tenant in the hash
// as it's not included in the data and leaving it out gives
// flexibility for migrating data between tenants
func (r Ref) Hash(h hash.Hash32) error {
	if err := r.Bounds.Hash(h); err != nil {
		return err
	}

	var enc encoding.Encbuf

	enc.PutString(r.TableName)
	enc.PutBE64(uint64(r.StartTimestamp))
	enc.PutBE64(uint64(r.EndTimestamp))
	enc.PutBE32(r.Checksum)

	_, err := h.Write(enc.Get())
	return errors.Wrap(err, "writing BlockRef")
}

// Cmp returns the fingerprint's position relative to the bounds
func (r Ref) Cmp(fp uint64) v1.BoundsCheck {
	return r.Bounds.Cmp(model.Fingerprint(fp))
}

func (r Ref) Interval() Interval {
	return NewInterval(r.StartTimestamp, r.EndTimestamp)
}

type BlockRef struct {
	Ref
	compression.Codec
}

func (r BlockRef) String() string {
	return defaultKeyResolver{}.Block(r).Addr()
}

func BlockRefFromKey(k string) (BlockRef, error) {
	return defaultKeyResolver{}.ParseBlockKey(key(k))
}

type MetaRef struct {
	Ref
}

func (r MetaRef) String() string {
	return defaultKeyResolver{}.Meta(r).Addr()
}

func MetaRefFromKey(k string) (MetaRef, error) {
	return defaultKeyResolver{}.ParseMetaKey(key(k))
}

// todo rename it
type Meta struct {
	MetaRef `json:"-"`

	// The specific TSDB files used to generate the block.
	Sources []tsdb.SingleTenantTSDBIdentifier

	// A list of blocks that were generated
	Blocks []BlockRef
}

func (m Meta) MostRecentSource() (tsdb.SingleTenantTSDBIdentifier, bool) {
	if len(m.Sources) == 0 {
		return tsdb.SingleTenantTSDBIdentifier{}, false
	}

	mostRecent := m.Sources[0]
	for _, source := range m.Sources[1:] {
		if source.TS.After(mostRecent.TS) {
			mostRecent = source
		}
	}

	return mostRecent, true
}

func MetaRefFrom(
	tenant,
	table string,
	bounds v1.FingerprintBounds,
	sources []tsdb.SingleTenantTSDBIdentifier,
	blocks []BlockRef,
) (MetaRef, error) {

	h := v1.Crc32HashPool.Get()
	defer v1.Crc32HashPool.Put(h)

	err := bounds.Hash(h)
	if err != nil {
		return MetaRef{}, errors.Wrap(err, "writing OwnershipRange")
	}

	for _, source := range sources {
		err = source.Hash(h)
		if err != nil {
			return MetaRef{}, errors.Wrap(err, "writing Sources")
		}
	}

	var (
		start, end model.Time
	)

	for i, block := range blocks {
		if i == 0 || block.StartTimestamp.Before(start) {
			start = block.StartTimestamp
		}

		if block.EndTimestamp.After(end) {
			end = block.EndTimestamp
		}

		err = block.Hash(h)
		if err != nil {
			return MetaRef{}, errors.Wrap(err, "writing Blocks")
		}
	}

	return MetaRef{
		Ref: Ref{
			TenantID:       tenant,
			TableName:      table,
			Bounds:         bounds,
			StartTimestamp: start,
			EndTimestamp:   end,
			Checksum:       h.Sum32(),
		},
	}, nil

}

type MetaSearchParams struct {
	TenantID string
	Interval Interval
	Keyspace v1.FingerprintBounds
}

type MetaClient interface {
	KeyResolver
	GetMeta(ctx context.Context, ref MetaRef) (Meta, error)
	GetMetas(ctx context.Context, refs []MetaRef) ([]Meta, error)
	PutMeta(ctx context.Context, meta Meta) error
	DeleteMetas(ctx context.Context, refs []MetaRef) error
}

type Block struct {
	BlockRef
	Data io.ReadSeekCloser
}

// CloseableReadSeekerAdapter is a wrapper around io.ReadSeeker to make it io.Closer
// if it doesn't already implement it.
type ClosableReadSeekerAdapter struct {
	io.ReadSeeker
}

func (c ClosableReadSeekerAdapter) Close() error {
	if closer, ok := c.ReadSeeker.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

func newRefFrom(tenant, table string, md v1.BlockMetadata) Ref {
	return Ref{
		TenantID:       tenant,
		TableName:      table,
		Bounds:         md.Series.Bounds,
		StartTimestamp: md.Series.FromTs,
		EndTimestamp:   md.Series.ThroughTs,
		Checksum:       md.Checksum,
	}
}

func newBlockRefWithEncoding(ref Ref, enc compression.Codec) BlockRef {
	return BlockRef{Ref: ref, Codec: enc}
}

func BlockFrom(enc compression.Codec, tenant, table string, blk *v1.Block) (Block, error) {
	md, err := blk.Metadata()
	if err != nil {
		return Block{}, errors.Wrap(err, "decoding index")
	}

	ref := newBlockRefWithEncoding(newRefFrom(tenant, table, md), enc)

	// TODO(owen-d): pool
	buf := bytes.NewBuffer(nil)
	err = v1.TarCompress(ref.Codec, buf, blk.Reader())
	if err != nil {
		return Block{}, err
	}

	reader := bytes.NewReader(buf.Bytes())

	return Block{
		BlockRef: ref,
		Data:     ClosableReadSeekerAdapter{reader},
	}, nil
}

type BlockClient interface {
	KeyResolver
	GetBlock(ctx context.Context, ref BlockRef) (BlockDirectory, error)
	GetBlocks(ctx context.Context, refs []BlockRef) ([]BlockDirectory, error)
	PutBlock(ctx context.Context, block Block) error
	DeleteBlocks(ctx context.Context, refs []BlockRef) error
}

type Client interface {
	MetaClient
	BlockClient
	IsObjectNotFoundErr(err error) bool
	ObjectClient() client.ObjectClient
	Stop()
}

// Compiler check to ensure BloomClient implements the Client interface
var _ Client = &BloomClient{}

type BloomClient struct {
	KeyResolver
	concurrency int
	client      client.ObjectClient
	logger      log.Logger
	fsResolver  KeyResolver
}

func NewBloomClient(cfg bloomStoreConfig, client client.ObjectClient, logger log.Logger) (*BloomClient, error) {
	fsResolver, err := NewShardedPrefixedResolver(cfg.workingDirs, defaultKeyResolver{})
	if err != nil {
		return nil, errors.Wrap(err, "creating fs resolver")
	}

	return &BloomClient{
		KeyResolver: defaultKeyResolver{}, // TODO(owen-d): hook into schema, similar to `{,Parse}ExternalKey`
		fsResolver:  fsResolver,
		concurrency: cfg.numWorkers,
		client:      client,
		logger:      logger,
	}, nil
}

func (b *BloomClient) ObjectClient() client.ObjectClient {
	return b.client
}

func (b *BloomClient) IsObjectNotFoundErr(err error) bool {
	return b.client.IsObjectNotFoundErr(err)
}

func (b *BloomClient) PutMeta(ctx context.Context, meta Meta) error {
	data, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("failed to encode meta file %s: %w", meta.String(), err)
	}
	key := b.Meta(meta.MetaRef).Addr()
	return b.client.PutObject(ctx, key, bytes.NewReader(data))
}

func (b *BloomClient) DeleteMetas(ctx context.Context, refs []MetaRef) error {
	err := concurrency.ForEachJob(ctx, len(refs), b.concurrency, func(ctx context.Context, idx int) error {
		key := b.Meta(refs[idx]).Addr()
		return b.client.DeleteObject(ctx, key)
	})

	return err
}

// GetBlock downloads the blocks from objectStorage and returns the directory
// in which the block data resides
func (b *BloomClient) GetBlock(ctx context.Context, ref BlockRef) (BlockDirectory, error) {
	key := b.Block(ref).Addr()

	rc, _, err := b.client.GetObject(ctx, key)
	if err != nil {
		return BlockDirectory{}, errors.Wrap(err, fmt.Sprintf("failed to get block file %s", key))
	}
	defer rc.Close()

	// the block directory must not contain the .tar(.compression) extension
	path := localFilePathWithoutExtension(ref, b.fsResolver)
	err = util.EnsureDirectory(path)
	if err != nil {
		return BlockDirectory{}, fmt.Errorf("failed to create block directory %s: %w", path, err)
	}

	err = v1.UnTarCompress(ref.Codec, path, rc)
	if err != nil {
		return BlockDirectory{}, fmt.Errorf("failed to extract block file %s: %w", key, err)
	}

	return NewBlockDirectory(ref, path), nil
}

func (b *BloomClient) GetBlocks(ctx context.Context, refs []BlockRef) ([]BlockDirectory, error) {
	// TODO(chaudum): Integrate download queue
	// The current implementation does brute-force download of all blocks with maximum concurrency.
	// However, we want that a single block is downloaded only exactly once, even if it is requested
	// multiple times concurrently.
	results := make([]BlockDirectory, len(refs))
	err := concurrency.ForEachJob(ctx, len(refs), b.concurrency, func(ctx context.Context, idx int) error {
		block, err := b.GetBlock(ctx, refs[idx])
		if err != nil {
			return err
		}
		results[idx] = block
		return nil
	})

	return results, err
}

func (b *BloomClient) PutBlock(ctx context.Context, block Block) error {
	defer func(Data io.ReadCloser) {
		_ = Data.Close()
	}(block.Data)

	key := b.Block(block.BlockRef).Addr()
	_, err := block.Data.Seek(0, 0)
	if err != nil {
		return fmt.Errorf("failed to seek block file %s: %w", key, err)
	}

	err = b.client.PutObject(ctx, key, block.Data)
	if err != nil {
		return fmt.Errorf("failed to put block file %s: %w", key, err)
	}
	return nil
}

func (b *BloomClient) DeleteBlocks(ctx context.Context, references []BlockRef) error {
	return concurrency.ForEachJob(ctx, len(references), b.concurrency, func(ctx context.Context, idx int) error {
		ref := references[idx]
		key := b.Block(ref).Addr()
		err := b.client.DeleteObject(ctx, key)

		if err != nil {
			return fmt.Errorf("failed to delete block file %s: %w", key, err)
		}
		return nil
	})
}

func (b *BloomClient) Stop() {
	b.client.Stop()
}

func (b *BloomClient) GetMetas(ctx context.Context, refs []MetaRef) ([]Meta, error) {
	results := make([]*Meta, len(refs))
	err := concurrency.ForEachJob(ctx, len(refs), b.concurrency, func(ctx context.Context, idx int) error {
		meta, err := b.GetMeta(ctx, refs[idx])
		if err != nil {
			key := b.KeyResolver.Meta(refs[idx]).Addr()
			if !b.IsObjectNotFoundErr(err) {
				return fmt.Errorf("failed to get meta file %s: %w", key, err)
			}
			level.Error(b.logger).Log("msg", "failed to get meta file", "ref", key, "err", err)
			return nil
		}
		results[idx] = &meta
		return nil
	})

	filtered := make([]Meta, 0, len(results))
	for _, r := range results {
		if r != nil {
			filtered = append(filtered, *r)
		}
	}
	return filtered, err
}

// GetMeta fetches the meta file for given MetaRef from object storage and
// decodes the JSON data into a Meta.
// If the meta file is not found in storage or decoding fails, the empty Meta
// is returned along with the error.
func (b *BloomClient) GetMeta(ctx context.Context, ref MetaRef) (Meta, error) {
	meta := Meta{MetaRef: ref}
	key := b.KeyResolver.Meta(ref).Addr()
	reader, _, err := b.client.GetObject(ctx, key)
	if err != nil {
		return meta, err
	}
	defer reader.Close()

	err = json.NewDecoder(reader).Decode(&meta)
	if err != nil {
		return meta, errors.Wrap(err, "failed to decode JSON")
	}
	return meta, nil
}

func findPeriod(configs []config.PeriodConfig, ts model.Time) (config.DayTime, error) {
	for i := len(configs) - 1; i >= 0; i-- {
		periodConfig := configs[i]
		if !periodConfig.From.Time.After(ts) {
			return periodConfig.From, nil
		}
	}
	return config.DayTime{}, fmt.Errorf("can not find period for timestamp %d", ts)
}

type listOpResult struct {
	ts       time.Time
	objects  []client.StorageObject
	prefixes []client.StorageCommonPrefix
}

type listOpCache map[string]listOpResult

type cachedListOpObjectClient struct {
	client.ObjectClient
	cache         listOpCache
	mtx           sync.RWMutex
	ttl, interval time.Duration
	done          chan struct{}
}

func newCachedListOpObjectClient(oc client.ObjectClient, ttl, interval time.Duration) *cachedListOpObjectClient {
	client := &cachedListOpObjectClient{
		ObjectClient: oc,
		cache:        make(listOpCache),
		done:         make(chan struct{}),
		ttl:          ttl,
		interval:     interval,
	}

	go func(c *cachedListOpObjectClient) {
		ticker := time.NewTicker(c.interval)
		defer ticker.Stop()

		for {
			select {
			case <-c.done:
				return
			case <-ticker.C:
				c.mtx.Lock()
				for k := range c.cache {
					if time.Since(c.cache[k].ts) > c.ttl {
						delete(c.cache, k)
					}
				}
				c.mtx.Unlock()
			}
		}
	}(client)

	return client
}

func (c *cachedListOpObjectClient) List(ctx context.Context, prefix string, delimiter string) ([]client.StorageObject, []client.StorageCommonPrefix, error) {
	var (
		start    = time.Now()
		cacheDur time.Duration
	)
	defer func() {
		if sp := opentracing.SpanFromContext(ctx); sp != nil {
			sp.LogKV(
				"cache_duration", cacheDur,
				"total_duration", time.Since(start),
			)
		}
	}()

	if delimiter != "" {
		return nil, nil, fmt.Errorf("does not support LIST calls with delimiter: %s", delimiter)
	}
	c.mtx.RLock()
	cached, found := c.cache[prefix]
	c.mtx.RUnlock()
	cacheDur = time.Since(start)
	if found {
		return cached.objects, cached.prefixes, nil
	}

	c.mtx.Lock()
	defer c.mtx.Unlock()

	objects, prefixes, err := c.ObjectClient.List(ctx, prefix, delimiter)
	if err != nil {
		return nil, nil, err
	}

	c.cache[prefix] = listOpResult{
		ts:       time.Now(),
		objects:  objects,
		prefixes: prefixes,
	}

	return objects, prefixes, err
}

func (c *cachedListOpObjectClient) Stop() {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	close(c.done)
	c.cache = nil
	c.ObjectClient.Stop()
}
