package builder

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/chunkenc"
	"github.com/grafana/loki/v3/pkg/compression"
	"github.com/grafana/loki/v3/pkg/ingester"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores"
	"github.com/grafana/loki/v3/pkg/util"

	"github.com/grafana/loki/pkg/push"
)

const (
	flushReasonFull   = "full"
	flushReasonMaxAge = "max_age"
	onePointFiveMB    = 3 << 19
)

type Appender struct {
	id              string
	cfg             Config
	periodicConfigs []config.PeriodConfig

	metrics *builderMetrics
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
	metrics *builderMetrics,
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
	metrics *builderMetrics
	streams *streamsMap
	logger  log.Logger

	periods []config.PeriodConfig
}

func newInstance(
	cfg Config,
	tenant string,
	metrics *builderMetrics,
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
	metrics  *builderMetrics
}

func newStream(fp model.Fingerprint, ls labels.Labels, cfg Config, metrics *builderMetrics) *stream {
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
			return closed, fmt.Errorf("appending entry: %w", err)
		}
	}

	return closed, nil
}

func (s *stream) closeChunk() (*chunkenc.MemChunk, error) {
	if err := s.chunk.Close(); err != nil {
		return nil, fmt.Errorf("closing chunk: %w", err)
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
