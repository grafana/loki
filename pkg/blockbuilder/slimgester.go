package blockbuilder

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"runtime"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/services"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/push"
	"github.com/grafana/loki/v3/pkg/analytics"
	"github.com/grafana/loki/v3/pkg/chunkenc"
	"github.com/grafana/loki/v3/pkg/compression"
	"github.com/grafana/loki/v3/pkg/ingester"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores"
	"github.com/grafana/loki/v3/pkg/util"
	"github.com/grafana/loki/v3/pkg/util/constants"
)

const (
	flushReasonFull   = "full"
	flushReasonMaxAge = "max_age"
)

type SlimgesterMetrics struct {
	chunkUtilization              prometheus.Histogram
	chunkEntries                  prometheus.Histogram
	chunkSize                     prometheus.Histogram
	chunkCompressionRatio         prometheus.Histogram
	chunksPerTenant               *prometheus.CounterVec
	chunkSizePerTenant            *prometheus.CounterVec
	chunkAge                      prometheus.Histogram
	chunkEncodeTime               prometheus.Histogram
	chunksFlushFailures           prometheus.Counter
	chunksFlushedPerReason        *prometheus.CounterVec
	chunkLifespan                 prometheus.Histogram
	chunksEncoded                 *prometheus.CounterVec
	chunkDecodeFailures           *prometheus.CounterVec
	flushedChunksStats            *analytics.Counter
	flushedChunksBytesStats       *analytics.Statistics
	flushedChunksLinesStats       *analytics.Statistics
	flushedChunksAgeStats         *analytics.Statistics
	flushedChunksLifespanStats    *analytics.Statistics
	flushedChunksUtilizationStats *analytics.Statistics

	chunksCreatedTotal prometheus.Counter
	samplesPerChunk    prometheus.Histogram
	blocksPerChunk     prometheus.Histogram
	chunkCreatedStats  *analytics.Counter
}

func NewSlimgesterMetrics(r prometheus.Registerer) *SlimgesterMetrics {
	return &SlimgesterMetrics{
		chunkUtilization: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Namespace: constants.Loki,
			Name:      "slimgester_chunk_utilization",
			Help:      "Distribution of stored chunk utilization (when stored).",
			Buckets:   prometheus.LinearBuckets(0, 0.2, 6),
		}),
		chunkEntries: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Namespace: constants.Loki,
			Name:      "slimgester_chunk_entries",
			Help:      "Distribution of stored lines per chunk (when stored).",
			Buckets:   prometheus.ExponentialBuckets(200, 2, 9), // biggest bucket is 200*2^(9-1) = 51200
		}),
		chunkSize: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Namespace: constants.Loki,
			Name:      "slimgester_chunk_size_bytes",
			Help:      "Distribution of stored chunk sizes (when stored).",
			Buckets:   prometheus.ExponentialBuckets(20000, 2, 10), // biggest bucket is 20000*2^(10-1) = 10,240,000 (~10.2MB)
		}),
		chunkCompressionRatio: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Namespace: constants.Loki,
			Name:      "slimgester_chunk_compression_ratio",
			Help:      "Compression ratio of chunks (when stored).",
			Buckets:   prometheus.LinearBuckets(.75, 2, 10),
		}),
		chunksPerTenant: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Name:      "slimgester_chunks_stored_total",
			Help:      "Total stored chunks per tenant.",
		}, []string{"tenant"}),
		chunkSizePerTenant: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Name:      "slimgester_chunk_stored_bytes_total",
			Help:      "Total bytes stored in chunks per tenant.",
		}, []string{"tenant"}),
		chunkAge: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Namespace: constants.Loki,
			Name:      "slimgester_chunk_age_seconds",
			Help:      "Distribution of chunk ages (when stored).",
			// with default settings chunks should flush between 5 min and 12 hours
			// so buckets at 1min, 5min, 10min, 30min, 1hr, 2hr, 4hr, 10hr, 12hr, 16hr
			Buckets: []float64{60, 300, 600, 1800, 3600, 7200, 14400, 36000, 43200, 57600},
		}),
		chunkEncodeTime: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Namespace: constants.Loki,
			Name:      "slimgester_chunk_encode_time_seconds",
			Help:      "Distribution of chunk encode times.",
			// 10ms to 10s.
			Buckets: prometheus.ExponentialBuckets(0.01, 4, 6),
		}),
		chunksFlushFailures: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Name:      "slimgester_chunks_flush_failures_total",
			Help:      "Total number of flush failures.",
		}),
		chunksFlushedPerReason: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Name:      "slimgester_chunks_flushed_total",
			Help:      "Total flushed chunks per reason.",
		}, []string{"reason"}),
		chunkLifespan: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Namespace: constants.Loki,
			Name:      "slimgester_chunk_bounds_hours",
			Help:      "Distribution of chunk end-start durations.",
			// 1h -> 8hr
			Buckets: prometheus.LinearBuckets(1, 1, 8),
		}),
		chunksEncoded: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Name:      "slimgester_chunks_encoded_total",
			Help:      "The total number of chunks encoded in the ingester.",
		}, []string{"user"}),
		chunkDecodeFailures: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Name:      "slimgester_chunk_decode_failures_total",
			Help:      "The number of freshly encoded chunks that failed to decode.",
		}, []string{"user"}),
		flushedChunksStats:      analytics.NewCounter("slimgester_flushed_chunks"),
		flushedChunksBytesStats: analytics.NewStatistics("slimgester_flushed_chunks_bytes"),
		flushedChunksLinesStats: analytics.NewStatistics("slimgester_flushed_chunks_lines"),
		flushedChunksAgeStats: analytics.NewStatistics(
			"slimgester_flushed_chunks_age_seconds",
		),
		flushedChunksLifespanStats: analytics.NewStatistics(
			"slimgester_flushed_chunks_lifespan_seconds",
		),
		flushedChunksUtilizationStats: analytics.NewStatistics(
			"slimgester_flushed_chunks_utilization",
		),
		chunksCreatedTotal: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Name:      "slimgester_chunks_created_total",
			Help:      "The total number of chunks created in the ingester.",
		}),
		samplesPerChunk: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Namespace: constants.Loki,
			Subsystem: "slimgester",
			Name:      "samples_per_chunk",
			Help:      "The number of samples in a chunk.",

			Buckets: prometheus.LinearBuckets(4096, 2048, 6),
		}),
		blocksPerChunk: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Namespace: constants.Loki,
			Subsystem: "slimgester",
			Name:      "blocks_per_chunk",
			Help:      "The number of blocks in a chunk.",

			Buckets: prometheus.ExponentialBuckets(5, 2, 6),
		}),

		chunkCreatedStats: analytics.NewCounter("slimgester_chunk_created"),
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

	store stores.ChunkWriter

	tsdbCreator   *TsdbCreator
	jobController *PartitionJobController
}

func NewSlimgester(
	cfg Config,
	periodicConfigs []config.PeriodConfig,
	store stores.ChunkWriter,
	logger log.Logger,
	reg prometheus.Registerer,
	tsdbCreator *TsdbCreator,
	jobController *PartitionJobController,
) (*Slimgester,
	error) {
	i := &Slimgester{
		cfg:             cfg,
		periodicConfigs: periodicConfigs,
		metrics:         NewSlimgesterMetrics(reg),
		logger:          logger,
		instances:       make(map[string]*instance),
		store:           store,
		tsdbCreator:     tsdbCreator,
		jobController:   jobController,
	}

	i.Service = services.NewBasicService(nil, i.running, nil)
	return i, nil
}

func (i *Slimgester) running(ctx context.Context) error {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			_, err := i.runOne(ctx)
			if err != nil {
				return err
			}
		}
	}
}

// runOne performs a single
func (i *Slimgester) runOne(ctx context.Context) (skipped bool, err error) {

	exists, job, err := i.jobController.LoadJob(ctx)
	if err != nil {
		return false, err
	}

	if !exists {
		level.Info(i.logger).Log("msg", "no available job to process")
		return true, nil
	}

	var lastOffset int64
	p := newPipeline(ctx)

	// Pipeline stage 1: Process the job offsets and write records to inputCh
	// This stage reads from the partition and feeds records into the input channel
	// When complete, it stores the last processed offset and closes the channel
	inputCh := make(chan []AppendInput)
	p.AddStageWithCleanup(
		1,
		func(ctx context.Context) error {
			lastOffset, err = i.jobController.part.Process(ctx, job.Offsets, inputCh)
			return err
		},
		func() error {
			close(inputCh)
			return nil
		},
	)

	// Stage 2: Process input records and generate chunks
	// This stage receives AppendInput batches, appends them to appropriate instances,
	// and forwards any cut chunks to the chunks channel for flushing.
	// ConcurrentWriters workers process inputs in parallel to maximize throughput.
	flush := make(chan *chunk.Chunk)
	p.AddStageWithCleanup(
		i.cfg.ConcurrentWriters,
		func(ctx context.Context) error {

			for {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case inputs, ok := <-inputCh:
					// inputs are finished; we're done
					if !ok {
						return nil
					}

					for _, input := range inputs {
						cut, err := i.Append(ctx, input)
						if err != nil {
							level.Error(i.logger).Log("msg", "failed to append records", "err", err)
							return err
						}

						for _, chk := range cut {
							select {
							case <-ctx.Done():
								return ctx.Err()
							case flush <- chk:
							}
						}
					}
				}
			}
		},
		func() error {
			close(flush)
			return nil
		},
	)

	// Stage 3: Flush chunks to storage
	// This stage receives chunks from the chunks channel and flushes them to storage
	// using ConcurrentFlushes workers for parallel processing
	p.AddStage(
		i.cfg.ConcurrentFlushes,
		func(ctx context.Context) error {
			for {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case chk, ok := <-flush:
					if !ok {
						return nil
					}
					if _, err := withBackoff(
						ctx,
						defaultBackoffConfig, // retry forever
						func() (res struct{}, err error) {
							err = i.store.PutOne(ctx, chk.From, chk.Through, *chk)
							if err != nil {
								i.metrics.chunksFlushFailures.Inc()
								return
							}
							i.reportFlushedChunkStatistics(chk)
							return
						},
					); err != nil {
						return err
					}
				}
			}
		},
	)

	err = p.Run()
	if err != nil {
		return false, err
	}

	if err = i.jobController.part.Commit(ctx, lastOffset); err != nil {
		return false, err
	}

	return false, nil
}

// reportFlushedChunkStatistics calculate overall statistics of flushed chunks without compromising the flush process.
func (i *Slimgester) reportFlushedChunkStatistics(
	ch *chunk.Chunk,
) {
	byt, err := ch.Encoded()
	if err != nil {
		level.Error(i.logger).Log("msg", "failed to encode flushed wire chunk", "err", err)
		return
	}
	sizePerTenant := i.metrics.chunkSizePerTenant.WithLabelValues(ch.UserID)
	countPerTenant := i.metrics.chunksPerTenant.WithLabelValues(ch.UserID)

	reason := flushReasonFull
	from, through := ch.From.Time(), ch.Through.Time()
	if through.Sub(from) > i.cfg.MaxChunkAge {
		reason = flushReasonMaxAge
	}

	i.metrics.chunksFlushedPerReason.WithLabelValues(reason).Add(1)

	compressedSize := float64(len(byt))
	uncompressedSize, ok := chunkenc.UncompressedSize(ch.Data)

	if ok && compressedSize > 0 {
		i.metrics.chunkCompressionRatio.Observe(float64(uncompressedSize) / compressedSize)
	}

	utilization := ch.Data.Utilization()
	i.metrics.chunkUtilization.Observe(utilization)

	numEntries := ch.Data.Entries()
	i.metrics.chunkEntries.Observe(float64(numEntries))
	i.metrics.chunkSize.Observe(compressedSize)
	sizePerTenant.Add(compressedSize)
	countPerTenant.Inc()

	i.metrics.chunkAge.Observe(time.Since(from).Seconds())
	i.metrics.chunkLifespan.Observe(through.Sub(from).Hours())

	i.metrics.flushedChunksBytesStats.Record(compressedSize)
	i.metrics.flushedChunksLinesStats.Record(float64(numEntries))
	i.metrics.flushedChunksUtilizationStats.Record(utilization)
	i.metrics.flushedChunksAgeStats.Record(time.Since(from).Seconds())
	i.metrics.flushedChunksLifespanStats.Record(through.Sub(from).Seconds())
	i.metrics.flushedChunksStats.Inc(1)
}

type AppendInput struct {
	tenant string
	// both labels & labelsStr are populated to prevent duplicating conversion work in mulitple places
	labels    labels.Labels
	labelsStr string
	entries   []push.Entry
}

func (i *Slimgester) Append(ctx context.Context, input AppendInput) ([]*chunk.Chunk, error) {
	// use rlock so multiple appends can be called on same instance.
	// re-check after using regular lock if it didnt exist.
	i.instancesMtx.RLock()
	inst, ok := i.instances[input.tenant]
	i.instancesMtx.RUnlock()
	if !ok {
		i.instancesMtx.Lock()
		inst, ok = i.instances[input.tenant]
		if !ok {
			inst = newInstance(input.tenant, i.metrics, i.periodicConfigs, i.logger)
			i.instances[input.tenant] = inst
		}
		i.instancesMtx.Unlock()
	}

	closed, err := inst.Push(ctx, input)
	return closed, err
}

// instance is a slimmed down version from the ingester pkg
type instance struct {
	tenant  string
	buf     []byte             // buffer used to compute fps.
	mapper  *ingester.FpMapper // using of mapper no longer needs mutex because reading from streams is lock-free
	metrics *SlimgesterMetrics
	streams *streamsMap
	logger  log.Logger

	periods []config.PeriodConfig
}

func newInstance(
	tenant string,
	metrics *SlimgesterMetrics,
	periods []config.PeriodConfig,
	logger log.Logger,
) *instance {
	streams := newStreamsMap()
	return &instance{
		tenant:  tenant,
		buf:     make([]byte, 0, 1024),
		mapper:  ingester.NewFPMapper(streams.getLabelsFromFingerprint),
		metrics: metrics,
		streams: streams,
		logger:  logger,
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
	input AppendInput,
) (closed []*chunk.Chunk, err error) {
	err = i.streams.For(
		input.labelsStr,
		func() (*stream, error) {
			fp := i.getHashForLabels(input.labels)
			return newStream(fp, input.labels, i.metrics), nil
		},
		func(stream *stream) error {
			xs, err := stream.Push(input.entries)
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
					// encodeChunk mutates the chunk so we must pass by reference
					if err := i.encodeChunk(ctx, &chk, x); err != nil {
						return err
					}

					closed = append(closed, &chk)
				}
			}
			return err
		},
	)

	return closed, err
}

// encodeChunk encodes a chunk.Chunk.
func (i *instance) encodeChunk(ctx context.Context, ch *chunk.Chunk, mc *chunkenc.MemChunk) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	start := time.Now()
	chunkBytesSize := mc.BytesSize() + 4*1024 // size + 4kB should be enough room for cortex header
	if err := ch.EncodeTo(bytes.NewBuffer(make([]byte, 0, chunkBytesSize)), i.logger); err != nil {
		if !errors.Is(err, chunk.ErrChunkDecode) {
			return fmt.Errorf("chunk encoding: %w", err)
		}

		i.metrics.chunkDecodeFailures.WithLabelValues(ch.UserID).Inc()
	}
	i.metrics.chunkEncodeTime.Observe(time.Since(start).Seconds())
	i.metrics.chunksEncoded.WithLabelValues(ch.UserID).Inc()
	return nil
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
			s.metrics.chunkCreatedStats.Inc(1)

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
