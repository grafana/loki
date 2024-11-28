package blockbuilder

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/backoff"
	"github.com/grafana/dskit/services"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/chunkenc"
	"github.com/grafana/loki/v3/pkg/compression"
	"github.com/grafana/loki/v3/pkg/ingester"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
	"github.com/grafana/loki/v3/pkg/storage/types"
	"github.com/grafana/loki/v3/pkg/util"
	"github.com/grafana/loki/v3/pkg/util/flagext"
	util_log "github.com/grafana/loki/v3/pkg/util/log"

	"github.com/grafana/loki/pkg/push"
)

const (
	flushReasonFull   = "full"
	flushReasonMaxAge = "max_age"
	onePointFiveMB    = 3 << 19
)

type Config struct {
	ConcurrentFlushes int               `yaml:"concurrent_flushes"`
	ConcurrentWriters int               `yaml:"concurrent_writers"`
	BlockSize         flagext.ByteSize  `yaml:"chunk_block_size"`
	TargetChunkSize   flagext.ByteSize  `yaml:"chunk_target_size"`
	ChunkEncoding     string            `yaml:"chunk_encoding"`
	parsedEncoding    compression.Codec `yaml:"-"` // placeholder for validated encoding
	MaxChunkAge       time.Duration     `yaml:"max_chunk_age"`
	Interval          time.Duration     `yaml:"interval"`
	Backoff           backoff.Config    `yaml:"backoff_config"`
}

func (cfg *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.IntVar(&cfg.ConcurrentFlushes, prefix+"concurrent-flushes", 1, "How many flushes can happen concurrently")
	f.IntVar(&cfg.ConcurrentWriters, prefix+"concurrent-writers", 1, "How many workers to process writes, defaults to number of available cpus")
	_ = cfg.BlockSize.Set("256KB")
	f.Var(&cfg.BlockSize, prefix+"chunks-block-size", "The targeted _uncompressed_ size in bytes of a chunk block When this threshold is exceeded the head block will be cut and compressed inside the chunk.")
	_ = cfg.TargetChunkSize.Set(fmt.Sprint(onePointFiveMB))
	f.Var(&cfg.TargetChunkSize, prefix+"chunk-target-size", "A target _compressed_ size in bytes for chunks. This is a desired size not an exact size, chunks may be slightly bigger or significantly smaller if they get flushed for other reasons (e.g. chunk_idle_period). A value of 0 creates chunks with a fixed 10 blocks, a non zero value will create chunks with a variable number of blocks to meet the target size.")
	f.StringVar(&cfg.ChunkEncoding, prefix+"chunk-encoding", compression.Snappy.String(), fmt.Sprintf("The algorithm to use for compressing chunk. (%s)", compression.SupportedCodecs()))
	f.DurationVar(&cfg.MaxChunkAge, prefix+"max-chunk-age", 2*time.Hour, "The maximum duration of a timeseries chunk in memory. If a timeseries runs for longer than this, the current chunk will be flushed to the store and a new chunk created.")
	f.DurationVar(&cfg.Interval, prefix+"interval", 10*time.Minute, "The interval at which to run.")
	cfg.Backoff.RegisterFlagsWithPrefix(prefix+"backoff.", f)
}

// RegisterFlags registers flags.
func (cfg *Config) RegisterFlags(flags *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix("blockbuilder.", flags)
}

func (cfg *Config) Validate() error {
	enc, err := compression.ParseCodec(cfg.ChunkEncoding)
	if err != nil {
		return err
	}
	cfg.parsedEncoding = enc
	return nil
}

// BlockBuilder is a slimmed-down version of the ingester, intended to
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
type BlockBuilder struct {
	services.Service

	id              string
	cfg             Config
	periodicConfigs []config.PeriodConfig

	metrics *SlimgesterMetrics
	logger  log.Logger

	store         stores.ChunkWriter
	objStore      *MultiStore
	jobController *PartitionJobController
}

func NewBlockBuilder(
	id string,
	cfg Config,
	periodicConfigs []config.PeriodConfig,
	store stores.ChunkWriter,
	objStore *MultiStore,
	logger log.Logger,
	reg prometheus.Registerer,
	jobController *PartitionJobController,
) (*BlockBuilder,
	error) {
	i := &BlockBuilder{
		id:              id,
		cfg:             cfg,
		periodicConfigs: periodicConfigs,
		metrics:         NewSlimgesterMetrics(reg),
		logger:          logger,
		store:           store,
		objStore:        objStore,
		jobController:   jobController,
	}

	i.Service = services.NewBasicService(nil, i.running, nil)
	return i, nil
}

func (i *BlockBuilder) running(ctx context.Context) error {
	ticker := time.NewTicker(i.cfg.Interval)
	defer ticker.Stop()

	// run once in beginning
	select {
	case <-ctx.Done():
		return nil
	default:
		_, err := i.runOne(ctx)
		if err != nil {
			level.Error(i.logger).Log("msg", "block builder run failed", "err", err)
		}
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			skipped, err := i.runOne(ctx)
			level.Info(i.logger).Log(
				"msg", "completed block builder run", "skipped",
				"skipped", skipped,
				"err", err,
			)
			if err != nil {
				level.Error(i.logger).Log("msg", "block builder run failed", "err", err)
			}
		}
	}
}

// runOne performs a single
func (i *BlockBuilder) runOne(ctx context.Context) (skipped bool, err error) {

	exists, job, err := i.jobController.LoadJob(ctx)
	if err != nil {
		return false, err
	}

	if !exists {
		level.Info(i.logger).Log("msg", "no available job to process")
		return true, nil
	}

	logger := log.With(
		i.logger,
		"partition", job.Partition,
		"job_min_offset", job.Offsets.Min,
		"job_max_offset", job.Offsets.Max,
	)

	level.Debug(logger).Log("msg", "beginning job")

	indexer := newTsdbCreator()
	appender := newAppender(i.id,
		i.cfg,
		i.periodicConfigs,
		i.store,
		i.objStore,
		logger,
		i.metrics,
	)

	var lastOffset int64
	p := newPipeline(ctx)

	// Pipeline stage 1: Process the job offsets and write records to inputCh
	// This stage reads from the partition and feeds records into the input channel
	// When complete, it stores the last processed offset and closes the channel
	inputCh := make(chan []AppendInput)
	p.AddStageWithCleanup(
		"load records",
		1,
		func(ctx context.Context) error {
			lastOffset, err = i.jobController.Process(ctx, job.Offsets, inputCh)
			return err
		},
		func(ctx context.Context) error {
			level.Debug(logger).Log(
				"msg", "finished loading records",
				"ctx_error", ctx.Err(),
				"last_offset", lastOffset,
				"total_records", lastOffset-job.Offsets.Min,
			)
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
		"appender",
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
						cut, err := appender.Append(ctx, input)
						if err != nil {
							level.Error(logger).Log("msg", "failed to append records", "err", err)
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
		func(ctx context.Context) (err error) {
			defer func() {
				level.Debug(logger).Log(
					"msg", "finished appender",
					"err", err,
					"ctx_error", ctx.Err(),
				)
			}()
			defer close(flush)

			// once we're done appending, cut all remaining chunks.
			chks, err := appender.CutRemainingChunks(ctx)
			if err != nil {
				return err
			}

			for _, chk := range chks {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case flush <- chk:
				}
			}
			return nil
		},
	)

	// Stage 3: Flush chunks to storage
	// This stage receives chunks from the chunks channel and flushes them to storage
	// using ConcurrentFlushes workers for parallel processing
	p.AddStage(
		"flusher",
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
						i.cfg.Backoff, // retry forever
						func() (res struct{}, err error) {
							err = i.store.PutOne(ctx, chk.From, chk.Through, *chk)
							if err != nil {
								level.Error(logger).Log("msg", "failed to flush chunk", "err", err)
								i.metrics.chunksFlushFailures.Inc()
								return
							}
							appender.reportFlushedChunkStatistics(chk)

							// write flushed chunk to index
							approxKB := math.Round(float64(chk.Data.UncompressedSize()) / float64(1<<10))
							meta := index.ChunkMeta{
								Checksum: chk.ChunkRef.Checksum,
								MinTime:  int64(chk.ChunkRef.From),
								MaxTime:  int64(chk.ChunkRef.Through),
								KB:       uint32(approxKB),
								Entries:  uint32(chk.Data.Entries()),
							}
							err = indexer.Append(chk.UserID, chk.Metric, chk.ChunkRef.Fingerprint, index.ChunkMetas{meta})
							if err != nil {
								level.Error(logger).Log("msg", "failed to append chunk to index", "err", err)
							}

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
	level.Debug(logger).Log(
		"msg", "finished chunk creation",
		"err", err,
	)
	if err != nil {
		return false, err
	}

	var (
		nodeName    = i.id
		tableRanges = config.GetIndexStoreTableRanges(types.TSDBType, i.periodicConfigs)
	)

	built, err := indexer.create(ctx, nodeName, tableRanges)
	if err != nil {
		level.Error(logger).Log("msg", "failed to build index", "err", err)
		return false, err
	}

	u := newUploader(i.objStore)
	for _, db := range built {
		if _, err := withBackoff(ctx, i.cfg.Backoff, func() (res struct{}, err error) {
			err = u.Put(ctx, db)
			if err != nil {
				level.Error(util_log.Logger).Log(
					"msg", "failed to upload tsdb",
					"path", db.id.Path(),
				)
				return
			}

			level.Debug(logger).Log(
				"msg", "uploaded tsdb",
				"name", db.id.Name(),
			)
			return
		}); err != nil {
			return false, err
		}
	}

	if lastOffset <= job.Offsets.Min {
		return false, nil
	}

	if err = i.jobController.part.Commit(ctx, lastOffset); err != nil {
		level.Error(logger).Log(
			"msg", "failed to commit offset",
			"last_offset", lastOffset,
			"err", err,
		)
		return false, err
	}

	// log success
	level.Info(logger).Log(
		"msg", "successfully processed and committed batch",
		"last_offset", lastOffset,
	)

	return false, nil
}

type Appender struct {
	id              string
	cfg             Config
	periodicConfigs []config.PeriodConfig

	metrics *SlimgesterMetrics
	logger  log.Logger

	instances    map[string]*instance
	instancesMtx sync.RWMutex

	store    stores.ChunkWriter
	objStore *MultiStore
}

// Writer is a single use construct for building chunks
// for from a set of records. It's an independent struct to ensure its
// state is not reused across jobs.
func newAppender(
	id string,
	cfg Config,
	periodicConfigs []config.PeriodConfig,
	store stores.ChunkWriter,
	objStore *MultiStore,
	logger log.Logger,
	metrics *SlimgesterMetrics,
) *Appender {
	return &Appender{
		id:              id,
		cfg:             cfg,
		periodicConfigs: periodicConfigs,
		metrics:         metrics,
		logger:          logger,
		instances:       make(map[string]*instance),
		store:           store,
		objStore:        objStore,
	}
}

// reportFlushedChunkStatistics calculate overall statistics of flushed chunks without compromising the flush process.
func (w *Appender) reportFlushedChunkStatistics(
	ch *chunk.Chunk,
) {
	byt, err := ch.Encoded()
	if err != nil {
		level.Error(w.logger).Log("msg", "failed to encode flushed wire chunk", "err", err)
		return
	}
	sizePerTenant := w.metrics.chunkSizePerTenant.WithLabelValues(ch.UserID)
	countPerTenant := w.metrics.chunksPerTenant.WithLabelValues(ch.UserID)

	reason := flushReasonFull
	from, through := ch.From.Time(), ch.Through.Time()
	if through.Sub(from) > w.cfg.MaxChunkAge {
		reason = flushReasonMaxAge
	}

	w.metrics.chunksFlushedPerReason.WithLabelValues(reason).Add(1)

	compressedSize := float64(len(byt))
	uncompressedSize, ok := chunkenc.UncompressedSize(ch.Data)

	if ok && compressedSize > 0 {
		w.metrics.chunkCompressionRatio.Observe(float64(uncompressedSize) / compressedSize)
	}

	utilization := ch.Data.Utilization()
	w.metrics.chunkUtilization.Observe(utilization)

	numEntries := ch.Data.Entries()
	w.metrics.chunkEntries.Observe(float64(numEntries))
	w.metrics.chunkSize.Observe(compressedSize)
	sizePerTenant.Add(compressedSize)
	countPerTenant.Inc()

	w.metrics.chunkAge.Observe(time.Since(from).Seconds())
	w.metrics.chunkLifespan.Observe(through.Sub(from).Hours())

	w.metrics.flushedChunksBytesStats.Record(compressedSize)
	w.metrics.flushedChunksLinesStats.Record(float64(numEntries))
	w.metrics.flushedChunksUtilizationStats.Record(utilization)
	w.metrics.flushedChunksAgeStats.Record(time.Since(from).Seconds())
	w.metrics.flushedChunksLifespanStats.Record(through.Sub(from).Seconds())
	w.metrics.flushedChunksStats.Inc(1)
}

func (w *Appender) CutRemainingChunks(ctx context.Context) ([]*chunk.Chunk, error) {
	var chunks []*chunk.Chunk
	w.instancesMtx.Lock()
	defer w.instancesMtx.Unlock()

	for _, inst := range w.instances {

		// wrap in anonymous fn to make lock release more straightforward
		if err := func() error {
			inst.streams.mtx.Lock()
			defer inst.streams.mtx.Unlock()

			for _, stream := range inst.streams.byLabels {

				// wrap in anonymous fn to make lock release more straightforward
				if err := func() error {
					stream.chunkMtx.Lock()
					defer stream.chunkMtx.Unlock()
					if stream.chunk != nil {
						cut, err := stream.closeChunk()
						if err != nil {
							return err
						}
						encoded, err := inst.encodeChunk(ctx, stream, cut)
						if err != nil {
							return err
						}
						chunks = append(chunks, encoded)
					}
					return nil

				}(); err != nil {
					return err
				}

			}
			return nil

		}(); err != nil {
			return nil, err
		}

	}

	return chunks, nil
}

type AppendInput struct {
	tenant string
	// both labels & labelsStr are populated to prevent duplicating conversion work in multiple places
	labels    labels.Labels
	labelsStr string
	entries   []push.Entry
}

func (w *Appender) Append(ctx context.Context, input AppendInput) ([]*chunk.Chunk, error) {
	// use rlock so multiple appends can be called on same instance.
	// re-check after using regular lock if it didnt exist.
	w.instancesMtx.RLock()
	inst, ok := w.instances[input.tenant]
	w.instancesMtx.RUnlock()
	if !ok {
		w.instancesMtx.Lock()
		inst, ok = w.instances[input.tenant]
		if !ok {
			inst = newInstance(w.cfg, input.tenant, w.metrics, w.periodicConfigs, w.logger)
			w.instances[input.tenant] = inst
		}
		w.instancesMtx.Unlock()
	}

	closed, err := inst.Push(ctx, input)
	return closed, err
}

// instance is a slimmed down version from the ingester pkg
type instance struct {
	cfg     Config
	tenant  string
	buf     []byte             // buffer used to compute fps.
	mapper  *ingester.FpMapper // using of mapper no longer needs mutex because reading from streams is lock-free
	metrics *SlimgesterMetrics
	streams *streamsMap
	logger  log.Logger

	periods []config.PeriodConfig
}

func newInstance(
	cfg Config,
	tenant string,
	metrics *SlimgesterMetrics,
	periods []config.PeriodConfig,
	logger log.Logger,
) *instance {
	streams := newStreamsMap()
	return &instance{
		cfg:     cfg,
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
			return newStream(fp, input.labels, i.cfg, i.metrics), nil
		},
		func(stream *stream) error {
			xs, err := stream.Push(input.entries)
			if err != nil {
				return err
			}

			if len(xs) > 0 {
				for _, x := range xs {
					// encodeChunk mutates the chunk so we must pass by reference
					chk, err := i.encodeChunk(ctx, stream, x)
					if err != nil {
						return err
					}
					closed = append(closed, chk)
				}
			}
			return err
		},
	)

	return closed, err
}

// encodeChunk encodes a chunk.Chunk.
func (i *instance) encodeChunk(ctx context.Context, stream *stream, mc *chunkenc.MemChunk) (*chunk.Chunk, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	start := time.Now()

	firstTime, lastTime := util.RoundToMilliseconds(mc.Bounds())
	chk := chunk.NewChunk(
		i.tenant, stream.fp, stream.ls,
		chunkenc.NewFacade(mc, stream.blockSize, stream.targetChunkSize),
		firstTime,
		lastTime,
	)

	chunkBytesSize := mc.BytesSize() + 4*1024 // size + 4kB should be enough room for cortex header
	if err := chk.EncodeTo(bytes.NewBuffer(make([]byte, 0, chunkBytesSize)), i.logger); err != nil {
		if !errors.Is(err, chunk.ErrChunkDecode) {
			return nil, fmt.Errorf("chunk encoding: %w", err)
		}

		i.metrics.chunkDecodeFailures.WithLabelValues(chk.UserID).Inc()
	}
	i.metrics.chunkEncodeTime.Observe(time.Since(start).Seconds())
	i.metrics.chunksEncoded.WithLabelValues(chk.UserID).Inc()
	return &chk, nil
}

type stream struct {
	fp model.Fingerprint
	ls labels.Labels

	chunkFormat     byte
	codec           compression.Codec
	blockSize       int
	targetChunkSize int

	chunkMtx sync.RWMutex
	chunk    *chunkenc.MemChunk
	metrics  *SlimgesterMetrics
}

func newStream(fp model.Fingerprint, ls labels.Labels, cfg Config, metrics *SlimgesterMetrics) *stream {
	return &stream{
		fp: fp,
		ls: ls,

		chunkFormat:     chunkenc.ChunkFormatV4,
		codec:           cfg.parsedEncoding,
		blockSize:       cfg.BlockSize.Val(),
		targetChunkSize: cfg.TargetChunkSize.Val(),

		metrics: metrics,
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
			cut, err := s.closeChunk()
			if err != nil {
				return nil, err
			}
			closed = append(closed, cut)
		}

		if _, err = s.chunk.Append(&entries[i]); err != nil {
			return closed, errors.Wrap(err, "appending entry")
		}
	}

	return closed, nil
}

func (s *stream) closeChunk() (*chunkenc.MemChunk, error) {
	if err := s.chunk.Close(); err != nil {
		return nil, errors.Wrap(err, "closing chunk")
	}

	s.metrics.samplesPerChunk.Observe(float64(s.chunk.Size()))
	s.metrics.blocksPerChunk.Observe(float64(s.chunk.BlockCount()))
	s.metrics.chunksCreatedTotal.Inc()
	s.metrics.chunkCreatedStats.Inc(1)

	// add a chunk
	res := s.chunk
	s.chunk = s.NewChunk()
	return res, nil
}

func (s *stream) NewChunk() *chunkenc.MemChunk {
	return chunkenc.NewMemChunk(
		s.chunkFormat,
		s.codec,
		chunkenc.ChunkHeadFormatFor(s.chunkFormat),
		s.blockSize,
		s.targetChunkSize,
	)
}

func withBackoff[T any](
	ctx context.Context,
	config backoff.Config,
	fn func() (T, error),
) (T, error) {
	var zero T

	var boff = backoff.New(ctx, config)
	for boff.Ongoing() {
		res, err := fn()
		if err != nil {
			boff.Wait()
			continue
		}
		return res, nil
	}

	return zero, boff.ErrCause()
}
