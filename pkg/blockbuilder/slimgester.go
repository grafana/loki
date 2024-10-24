package blockbuilder

import (
	"context"
	"sync"

	"github.com/go-kit/log"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
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

type Config struct{}

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
	cfg             Config
	periodicConfigs []config.PeriodConfig

	metrics *SlimgesterMetrics
	logger  log.Logger

	instances    map[string]*instance
	instancesMtx sync.RWMutex

	store stores.ChunkWriter

	queueMtx   sync.Mutex
	flushQueue []*chunk.Chunk
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
	if len(closed) > 0 {
		i.queueMtx.Lock()
		defer i.queueMtx.Unlock()
		i.flushQueue = append(i.flushQueue, closed...)
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
	return &instance{
		tenant:  tenant,
		buf:     make([]byte, 0, 1024),
		mapper:  ingester.NewFPMapper(nil), // TODO: impl
		metrics: metrics,
		streams: &streamsMap{m: make(map[string]*stream)},
		periods: periods,
	}
}

type streamsMap struct {
	// labels -> stream
	m   map[string]*stream
	mtx sync.RWMutex
}

// For performs an operation on an existing stream, creating it if it wasn't previously present.
func (m *streamsMap) For(
	ls string,
	createFn func() (*stream, error),
	fn func(*stream) error,
) error {
	// first use read lock in case the stream exists
	m.mtx.RLock()
	if s, ok := m.m[ls]; ok {
		err := fn(s)
		m.mtx.RUnlock()
		return err
	}
	m.mtx.RUnlock()

	// Stream wasn't found, acquire write lock to create it
	m.mtx.Lock()
	defer m.mtx.Unlock()

	// Double check it wasn't created while we were upgrading the lock
	if s, ok := m.m[ls]; ok {
		return fn(s)
	}

	// Create new stream
	s, err := createFn()
	if err != nil {
		return err
	}

	m.m[ls] = s
	return fn(s)
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
