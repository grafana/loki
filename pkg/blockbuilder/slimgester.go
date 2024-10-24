package blockbuilder

import (
	"context"
	"flag"
	"fmt"
	"runtime"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/services"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/push"
	"github.com/grafana/loki/v3/pkg/chunkenc"
	"github.com/grafana/loki/v3/pkg/compression"
	"github.com/grafana/loki/v3/pkg/ingester"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores"
	"github.com/grafana/loki/v3/pkg/util"
)

type SlimgesterMetrics struct {
	chunksCreatedTotal prometheus.Counter
	samplesPerChunk    prometheus.Histogram
	blocksPerChunk     prometheus.Histogram
}

func NewSlimgesterMetrics(r prometheus.Registerer) *SlimgesterMetrics {
	return &SlimgesterMetrics{
		chunksCreatedTotal: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "loki_slimgester_chunks_created_total",
			Help: "The total number of chunks created in the slimgester.",
		}),
		samplesPerChunk: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Name:    "loki_slimgester_chunk_samples",
			Help:    "Number of samples in chunks at flush.",
			Buckets: prometheus.ExponentialBuckets(10, 2, 8), // 10, 20, 40, 80, 160, 320, 640, 1280
		}),
		blocksPerChunk: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Name:    "loki_slimgester_chunk_blocks",
			Help:    "Number of blocks in chunks at flush.",
			Buckets: prometheus.ExponentialBuckets(2, 2, 6), // 2, 4, 8, 16, 32, 64
		}),
	}
}

type Config struct {
	ConcurrentFlushes int               `yaml:"concurrent_flushes"`
	ConcurrentWriters int               `yaml:"concurrent_writers"`
	BlockSize         int               `yaml:"chunk_block_size"`
	TargetChunkSize   int               `yaml:"chunk_target_size"`
	ChunkEncoding     string            `yaml:"chunk_encoding"`
	parsedEncoding    compression.Codec `yaml:"-"` // placeholder for validated encoding
	MaxChunkAge       time.Duration     `yaml:"max_chunk_age"`
}

func (cfg *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.IntVar(&cfg.ConcurrentFlushes, "ingester.concurrent-flushes", 16, "How many flushes can happen concurrently")
	f.IntVar(&cfg.ConcurrentWriters, "ingester.concurrent-writers", runtime.NumCPU(), "How many workers to process writes, defaults to number of available cpus")
	f.IntVar(&cfg.BlockSize, "ingester.chunks-block-size", 256*1024, "The targeted _uncompressed_ size in bytes of a chunk block When this threshold is exceeded the head block will be cut and compressed inside the chunk.")
	f.IntVar(&cfg.TargetChunkSize, "ingester.chunk-target-size", 1572864, "A target _compressed_ size in bytes for chunks. This is a desired size not an exact size, chunks may be slightly bigger or significantly smaller if they get flushed for other reasons (e.g. chunk_idle_period). A value of 0 creates chunks with a fixed 10 blocks, a non zero value will create chunks with a variable number of blocks to meet the target size.") // 1.5 MB
	f.StringVar(&cfg.ChunkEncoding, "ingester.chunk-encoding", compression.GZIP.String(), fmt.Sprintf("The algorithm to use for compressing chunk. (%s)", compression.SupportedCodecs()))
	f.DurationVar(&cfg.MaxChunkAge, "ingester.max-chunk-age", 2*time.Hour, "The maximum duration of a timeseries chunk in memory. If a timeseries runs for longer than this, the current chunk will be flushed to the store and a new chunk created.")
}

// RegisterFlags registers flags.
func (c *Config) RegisterFlags(flags *flag.FlagSet) {
	c.RegisterFlagsWithPrefix("slimgester", flags)
}

func (cfg *Config) Validate() error {
	enc, err := compression.ParseCodec(cfg.ChunkEncoding)
	if err != nil {
		return err
	}
	cfg.parsedEncoding = enc
	return nil
}

// Slimgester is a slimmed-down version of the ingester, intended to
// ingest logs without WALs. Broadly, it accumulates logs into per-tenant chunks in the same way the existing ingester does,
// without a WAL. Index (TSDB) creation is also not an out-of-band procedure and must be called directly. In essence, this
// allows us to buffer data, flushing chunks to storage as necessary, and then when ready to commit this, relevant TSDBs (one per period) are created and flushed to storage. This allows an external caller to prepare a batch of data, build relevant chunks+indices, ensure they're flushed, and then return. As long as chunk+index creation is deterministic, this operation is also
// idempotent, making retries simple and impossible to introduce duplicate data.
// It contains the following methods:
//   - `Append(context.Context, logproto.PushRequest) error`
//     Adds a push request to ingested data. May flush existing chunks when they're full/etc.
//   - `Commit(context.Context) error`
//     Serializes (cuts) any buffered data into chunks, flushes them to storage, then creates + flushes TSDB indices
//     containing all chunk references. Finally, clears internal state.
type Slimgester struct {
	services.Service

	cfg             Config
	periodicConfigs []config.PeriodConfig

	metrics *SlimgesterMetrics
	logger  log.Logger

	instances    map[string]*instance
	instancesMtx sync.RWMutex

	store      stores.ChunkWriter
	flushQueue chan *chunk.Chunk

	wg     sync.WaitGroup // for waiting on flusher
	quit   chan struct{}  // for signaling flusher
	closer sync.Once      // for coordinating channel closure
}

func NewSlimgester(
	cfg Config,
	periodicConfigs []config.PeriodConfig,
	store stores.ChunkWriter,
	logger log.Logger,
	reg prometheus.Registerer,
) (*Slimgester,
	error) {
	i := &Slimgester{
		cfg:             cfg,
		periodicConfigs: periodicConfigs,
		metrics:         NewSlimgesterMetrics(reg),
		logger:          logger,
		instances:       make(map[string]*instance),
		store:           store,

		flushQueue: make(chan *chunk.Chunk),
		quit:       make(chan struct{}),
	}

	i.Service = services.NewBasicService(i.starting, i.running, i.stopping)
	return i, nil
}

func (i *Slimgester) starting(_ context.Context) error {
	go i.flushLoop()
	return nil
}

func (i *Slimgester) running(ctx context.Context) error {
	select {
	case <-ctx.Done():
		i.close()
	}

	return nil
}

func (i *Slimgester) stopping(err error) error {
	i.wg.Wait()
	return err
}

func (i *Slimgester) flushLoop() {
	i.wg.Add(1)
	i.wg.Done()
}

// behind sync.Once b/c it can be called from etiher `running` or `stopping`.
func (i *Slimgester) close() {
	i.closer.Do(func() {
		close(i.quit)
	})
}

func (i *Slimgester) Append(ctx context.Context, tenant string, req *logproto.PushRequest) error {
	// use rlock so multiple appends can be called on same instance.
	// re-check after using regular lock if it didnt exist.
	i.instancesMtx.RLock()
	inst, ok := i.instances[tenant]
	i.instancesMtx.RUnlock()
	if !ok {
		i.instancesMtx.Lock()
		inst, ok = i.instances[tenant]
		if !ok {
			inst = newInstance(tenant, i.metrics, i.periodicConfigs)
			i.instances[tenant] = inst
		}
		i.instancesMtx.Unlock()
	}

	closed, err := inst.Push(ctx, req)
	for _, chk := range closed {
		i.flushQueue <- chk
	}

	return err
}

// instance is a slimmed down version from the ingester pkg
type instance struct {
	tenant  string
	buf     []byte             // buffer used to compute fps.
	mapper  *ingester.FpMapper // using of mapper no longer needs mutex because reading from streams is lock-free
	metrics *SlimgesterMetrics
	streams *streamsMap

	periods []config.PeriodConfig
}

func newInstance(
	tenant string,
	metrics *SlimgesterMetrics,
	periods []config.PeriodConfig,
) *instance {
	streams := newStreamsMap()
	return &instance{
		tenant:  tenant,
		buf:     make([]byte, 0, 1024),
		mapper:  ingester.NewFPMapper(streams.getLabelsFromFingerprint),
		metrics: metrics,
		streams: streams,
		periods: periods,
	}
}

func newStreamsMap() *streamsMap {
	return &streamsMap{
		byLabels: make(map[string]*stream),
		byFp:     make(map[model.Fingerprint]*stream),
	}
}

type streamsMap struct {
	// labels -> stream
	byLabels map[string]*stream
	byFp     map[model.Fingerprint]*stream
	mtx      sync.RWMutex
}

// For performs an operation on an existing stream, creating it if it wasn't previously present.
func (m *streamsMap) For(
	ls string,
	createFn func() (*stream, error),
	fn func(*stream) error,
) error {
	// first use read lock in case the stream exists
	m.mtx.RLock()
	if s, ok := m.byLabels[ls]; ok {
		err := fn(s)
		m.mtx.RUnlock()
		return err
	}
	m.mtx.RUnlock()

	// Stream wasn't found, acquire write lock to create it
	m.mtx.Lock()
	defer m.mtx.Unlock()

	// Double check it wasn't created while we were upgrading the lock
	if s, ok := m.byLabels[ls]; ok {
		return fn(s)
	}

	// Create new stream
	s, err := createFn()
	if err != nil {
		return err
	}

	m.byLabels[ls] = s
	m.byFp[s.fp] = s
	return fn(s)
}

// Return labels associated with given fingerprint. Used by fingerprint mapper.
func (m *streamsMap) getLabelsFromFingerprint(fp model.Fingerprint) labels.Labels {
	m.mtx.RLock()
	defer m.mtx.RUnlock()

	if s, ok := m.byFp[fp]; ok {
		return s.ls
	}
	return nil
}

func (i *instance) getHashForLabels(ls labels.Labels) model.Fingerprint {
	var fp uint64
	fp, i.buf = ls.HashWithoutLabels(i.buf, []string(nil)...)
	return i.mapper.MapFP(model.Fingerprint(fp), ls)
}

// Push will iterate over the given streams present in the PushRequest and attempt to store them.
func (i *instance) Push(
	ctx context.Context,
	req *logproto.PushRequest,
) (closed []*chunk.Chunk, err error) {
	for _, s := range req.Streams {
		err = i.streams.For(
			s.Labels,
			func() (*stream, error) {
				ls, err := syntax.ParseLabels(s.Labels)
				if err != nil {
					return nil, err
				}
				fp := i.getHashForLabels(ls)
				return newStream(fp, ls, i.metrics), nil
			},
			func(stream *stream) error {
				xs, err := stream.Push(s.Entries)
				if err != nil {
					return err
				}

				if len(xs) > 0 {
					for _, x := range xs {
						firstTime, lastTime := util.RoundToMilliseconds(x.Bounds())
						chk := chunk.NewChunk(
							i.tenant, stream.fp, stream.ls,
							chunkenc.NewFacade(x, stream.blockSize, stream.targetChunkSize),
							firstTime,
							lastTime,
						)
						closed = append(closed, &chk)
					}
				}
				return err
			},
		)
	}

	return closed, err
}

type stream struct {
	fp model.Fingerprint
	ls labels.Labels

	chunkFormat     byte
	headFmt         chunkenc.HeadBlockFmt
	codec           compression.Codec
	blockSize       int
	targetChunkSize int

	chunkMtx sync.RWMutex
	chunk    *chunkenc.MemChunk
	metrics  *SlimgesterMetrics
}

func newStream(fp model.Fingerprint, ls labels.Labels, metrics *SlimgesterMetrics) *stream {
	return &stream{
		fp: fp,
		ls: ls,

		chunkFormat: chunkenc.ChunkFormatV3,
		metrics:     metrics,
	}
}

func (s *stream) Push(entries []push.Entry) (closed []*chunkenc.MemChunk, err error) {
	s.chunkMtx.Lock()
	defer s.chunkMtx.Unlock()

	if s.chunk == nil {
		s.chunk = s.NewChunk()
	}

	// bytesAdded, err := s.storeEntries(ctx, toStore, usageTracker)
	for i := 0; i < len(entries); i++ {

		// cut the chunk if the new addition overflows target size
		if !s.chunk.SpaceFor(&entries[i]) {
			if err = s.chunk.Close(); err != nil {
				return closed, errors.Wrap(err, "closing chunk")
			}

			s.metrics.samplesPerChunk.Observe(float64(s.chunk.Size()))
			s.metrics.blocksPerChunk.Observe(float64(s.chunk.BlockCount()))
			s.metrics.chunksCreatedTotal.Inc()

			// add a chunk
			closed = append(closed, s.chunk)
			s.chunk = s.NewChunk()
		}

		if _, err = s.chunk.Append(&entries[i]); err != nil {
			return closed, errors.Wrap(err, "appending entry")
		}
	}

	return closed, nil
}

func (s *stream) NewChunk() *chunkenc.MemChunk {
	return chunkenc.NewMemChunk(s.chunkFormat, s.codec, s.headFmt, s.blockSize, s.targetChunkSize)
}
