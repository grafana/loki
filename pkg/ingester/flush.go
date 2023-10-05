package ingester

import (
	"bytes"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"golang.org/x/net/context"

	"github.com/grafana/dskit/tenant"

	"github.com/grafana/loki/pkg/chunkenc"
	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/util"
	util_log "github.com/grafana/loki/pkg/util/log"
)

const (
	// Backoff for retrying 'immediate' flushes. Only counts for queue
	// position, not wallclock time.
	flushBackoff = 1 * time.Second

	nameLabel = "__name__"
	logsValue = "logs"

	flushReasonIdle   = "idle"
	flushReasonMaxAge = "max_age"
	flushReasonForced = "forced"
	flushReasonFull   = "full"
	flushReasonSynced = "synced"
)

// Note: this is called both during the WAL replay (zero or more times)
// and then after replay as well.
func (i *Ingester) InitFlushQueues() {
	i.flushQueuesDone.Add(i.cfg.ConcurrentFlushes)
	for j := 0; j < i.cfg.ConcurrentFlushes; j++ {
		i.flushQueues[j] = util.NewPriorityQueue(flushQueueLength)
		go i.flushLoop(j)
	}
}

// Flush implements ring.FlushTransferer
// Flush triggers a flush of all the chunks and closes the flush queues.
// Called from the Lifecycler as part of the ingester shutdown.
func (i *Ingester) Flush() {
	i.flush(true)
}

// TransferOut implements ring.FlushTransferer
// Noop implemenetation because ingesters have a WAL now that does not require transferring chunks any more.
// We return ErrTransferDisabled to indicate that we don't support transfers, and therefore we may flush on shutdown if configured to do so.
func (i *Ingester) TransferOut(_ context.Context) error {
	return ring.ErrTransferDisabled
}

func (i *Ingester) flush(mayRemoveStreams bool) {
	i.sweepUsers(true, mayRemoveStreams)

	// Close the flush queues, to unblock waiting workers.
	for _, flushQueue := range i.flushQueues {
		flushQueue.Close()
	}

	i.flushQueuesDone.Wait()
	level.Debug(util_log.Logger).Log("msg", "flush queues have drained")
}

// FlushHandler triggers a flush of all in memory chunks.  Mainly used for
// local testing.
func (i *Ingester) FlushHandler(w http.ResponseWriter, _ *http.Request) {
	i.sweepUsers(true, true)
	w.WriteHeader(http.StatusNoContent)
}

type flushOp struct {
	from      model.Time
	userID    string
	fp        model.Fingerprint
	immediate bool
}

func (o *flushOp) Key() string {
	return fmt.Sprintf("%s-%s-%v", o.userID, o.fp, o.immediate)
}

func (o *flushOp) Priority() int64 {
	return -int64(o.from)
}

// sweepUsers periodically schedules series for flushing and garbage collects users with no series
func (i *Ingester) sweepUsers(immediate, mayRemoveStreams bool) {
	instances := i.getInstances()

	for _, instance := range instances {
		i.sweepInstance(instance, immediate, mayRemoveStreams)
	}
}

func (i *Ingester) sweepInstance(instance *instance, immediate, mayRemoveStreams bool) {
	_ = instance.streams.ForEach(func(s *stream) (bool, error) {
		i.sweepStream(instance, s, immediate)
		i.removeFlushedChunks(instance, s, mayRemoveStreams)
		return true, nil
	})
}

func (i *Ingester) sweepStream(instance *instance, stream *stream, immediate bool) {
	stream.chunkMtx.RLock()
	defer stream.chunkMtx.RUnlock()
	if len(stream.chunks) == 0 {
		return
	}

	lastChunk := stream.chunks[len(stream.chunks)-1]
	shouldFlush, _ := i.shouldFlushChunk(&lastChunk)
	if len(stream.chunks) == 1 && !immediate && !shouldFlush {
		return
	}

	flushQueueIndex := int(uint64(stream.fp) % uint64(i.cfg.ConcurrentFlushes))
	firstTime, _ := stream.chunks[0].chunk.Bounds()
	i.flushQueues[flushQueueIndex].Enqueue(&flushOp{
		model.TimeFromUnixNano(firstTime.UnixNano()), instance.instanceID,
		stream.fp, immediate,
	})
}

func (i *Ingester) flushLoop(j int) {
	defer func() {
		level.Debug(util_log.Logger).Log("msg", "Ingester.flushLoop() exited")
		i.flushQueuesDone.Done()
	}()

	for {
		o := i.flushQueues[j].Dequeue()
		if o == nil {
			return
		}
		op := o.(*flushOp)

		err := i.flushUserSeries(op.userID, op.fp, op.immediate)
		if err != nil {
			level.Error(util_log.WithUserID(op.userID, util_log.Logger)).Log("msg", "failed to flush", "err", err)
		}

		// If we're exiting & we failed to flush, put the failed operation
		// back in the queue at a later point.
		if op.immediate && err != nil {
			op.from = op.from.Add(flushBackoff)
			i.flushQueues[j].Enqueue(op)
		}
	}
}

func (i *Ingester) flushUserSeries(userID string, fp model.Fingerprint, immediate bool) error {
	instance, ok := i.getInstanceByID(userID)
	if !ok {
		return nil
	}

	chunks, labels, chunkMtx := i.collectChunksToFlush(instance, fp, immediate)
	if len(chunks) < 1 {
		return nil
	}

	lbs := labels.String()
	level.Info(util_log.Logger).Log("msg", "flushing stream", "user", userID, "fp", fp, "immediate", immediate, "num_chunks", len(chunks), "labels", lbs)

	ctx := user.InjectOrgID(context.Background(), userID)
	ctx, cancel := context.WithTimeout(ctx, i.cfg.FlushOpTimeout)
	defer cancel()
	err := i.flushChunks(ctx, fp, labels, chunks, chunkMtx)
	if err != nil {
		return fmt.Errorf("failed to flush chunks: %w, num_chunks: %d, labels: %s", err, len(chunks), lbs)
	}

	return nil
}

func (i *Ingester) collectChunksToFlush(instance *instance, fp model.Fingerprint, immediate bool) ([]*chunkDesc, labels.Labels, *sync.RWMutex) {
	var stream *stream
	var ok bool
	stream, ok = instance.streams.LoadByFP(fp)

	if !ok {
		return nil, nil, nil
	}

	stream.chunkMtx.Lock()
	defer stream.chunkMtx.Unlock()

	var result []*chunkDesc
	for j := range stream.chunks {
		shouldFlush, reason := i.shouldFlushChunk(&stream.chunks[j])
		if immediate || shouldFlush {
			// Ensure no more writes happen to this chunk.
			if !stream.chunks[j].closed {
				stream.chunks[j].closed = true
			}
			// Flush this chunk if it hasn't already been successfully flushed.
			if stream.chunks[j].flushed.IsZero() {
				if immediate {
					reason = flushReasonForced
				}
				stream.chunks[j].reason = reason

				result = append(result, &stream.chunks[j])
			}
		}
	}
	return result, stream.labels, &stream.chunkMtx
}

func (i *Ingester) shouldFlushChunk(chunk *chunkDesc) (bool, string) {
	// Append should close the chunk when the a new one is added.
	if chunk.closed {
		if chunk.synced {
			return true, flushReasonSynced
		}
		return true, flushReasonFull
	}

	if time.Since(chunk.lastUpdated) > i.cfg.MaxChunkIdle {
		return true, flushReasonIdle
	}

	if from, to := chunk.chunk.Bounds(); to.Sub(from) > i.cfg.MaxChunkAge {
		return true, flushReasonMaxAge
	}

	return false, ""
}

func (i *Ingester) removeFlushedChunks(instance *instance, stream *stream, mayRemoveStream bool) {
	now := time.Now()

	stream.chunkMtx.Lock()
	defer stream.chunkMtx.Unlock()
	prevNumChunks := len(stream.chunks)
	var subtracted int
	for len(stream.chunks) > 0 {
		if stream.chunks[0].flushed.IsZero() || now.Sub(stream.chunks[0].flushed) < i.cfg.RetainPeriod {
			break
		}

		subtracted += stream.chunks[0].chunk.UncompressedSize()
		stream.chunks[0].chunk = nil // erase reference so the chunk can be garbage-collected
		stream.chunks = stream.chunks[1:]
	}
	i.metrics.memoryChunks.Sub(float64(prevNumChunks - len(stream.chunks)))

	// Signal how much data has been flushed to lessen any WAL replay pressure.
	i.replayController.Sub(int64(subtracted))

	if mayRemoveStream && len(stream.chunks) == 0 {
		// Unlock first, then lock inside streams' lock to prevent deadlock
		stream.chunkMtx.Unlock()
		// Only lock streamsMap when it's needed to remove a stream
		instance.streams.WithLock(func() {
			stream.chunkMtx.Lock()
			// Double check length
			if len(stream.chunks) == 0 {
				instance.removeStream(stream)
			}
		})
	}
}

// flushChunks iterates over given chunkDescs, derives chunk.Chunk from them and flush them to the store, one at a time.
//
// If a chunk fails to be flushed, this operation is reinserted in the queue. Since previously flushed chunks
// are marked as flushed, they shouldn't be flushed again.
// It has to close given chunks to have have the head block included.
func (i *Ingester) flushChunks(ctx context.Context, fp model.Fingerprint, labelPairs labels.Labels, cs []*chunkDesc, chunkMtx sync.Locker) error {
	userID, err := tenant.TenantID(ctx)
	if err != nil {
		return err
	}

	// NB(owen-d): No longer needed in TSDB (and is removed in that code path)
	// It's required by historical index stores so we keep it for now.
	labelsBuilder := labels.NewBuilder(labelPairs)
	labelsBuilder.Set(nameLabel, logsValue)
	metric := labelsBuilder.Labels()

	sizePerTenant := i.metrics.chunkSizePerTenant.WithLabelValues(userID)
	countPerTenant := i.metrics.chunksPerTenant.WithLabelValues(userID)

	for j, c := range cs {
		if err := i.closeChunk(c, chunkMtx); err != nil {
			return fmt.Errorf("chunk close for flushing: %w", err)
		}

		firstTime, lastTime := util.RoundToMilliseconds(c.chunk.Bounds())
		ch := chunk.NewChunk(
			userID, fp, metric,
			chunkenc.NewFacade(c.chunk, i.cfg.BlockSize, i.cfg.TargetChunkSize),
			firstTime,
			lastTime,
		)

		// encodeChunk mutates the chunk so we must pass by reference
		if err := i.encodeChunk(ctx, &ch, c); err != nil {
			return err
		}

		if err := i.flushChunk(ctx, &ch); err != nil {
			return err
		}

		reason := func() string {
			chunkMtx.Lock()
			defer chunkMtx.Unlock()

			return c.reason
		}()

		i.reportFlushedChunkStatistics(&ch, c, sizePerTenant, countPerTenant, reason)
		i.markChunkAsFlushed(cs[j], chunkMtx)
	}

	return nil
}

// markChunkAsFlushed mark a chunk to make sure it won't be flushed if this operation fails.
func (i *Ingester) markChunkAsFlushed(desc *chunkDesc, chunkMtx sync.Locker) {
	chunkMtx.Lock()
	defer chunkMtx.Unlock()
	desc.flushed = time.Now()
}

// closeChunk closes the given chunk while locking it to ensure that new blocks are cut before flushing.
//
// If the chunk isn't closed, data in the head block isn't included.
func (i *Ingester) closeChunk(desc *chunkDesc, chunkMtx sync.Locker) error {
	chunkMtx.Lock()
	defer chunkMtx.Unlock()

	return desc.chunk.Close()
}

// encodeChunk encodes a chunk.Chunk based on the given chunkDesc.
//
// If the encoding is unsuccessful the flush operation is reinserted in the queue which will cause
// the encoding for a given chunk to be evaluated again.
func (i *Ingester) encodeChunk(ctx context.Context, ch *chunk.Chunk, desc *chunkDesc) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	start := time.Now()
	chunkBytesSize := desc.chunk.BytesSize() + 4*1024 // size + 4kB should be enough room for cortex header
	if err := ch.EncodeTo(bytes.NewBuffer(make([]byte, 0, chunkBytesSize))); err != nil {
		return fmt.Errorf("chunk encoding: %w", err)
	}
	i.metrics.chunkEncodeTime.Observe(time.Since(start).Seconds())
	return nil
}

// flushChunk flushes the given chunk to the store.
//
// If the flush is successful, metrics for this flush are to be reported.
// If the flush isn't successful, the operation for this userID is requeued allowing this and all other unflushed
// chunk to have another opportunity to be flushed.
func (i *Ingester) flushChunk(ctx context.Context, ch *chunk.Chunk) error {
	if err := i.store.Put(ctx, []chunk.Chunk{*ch}); err != nil {
		return fmt.Errorf("store put chunk: %w", err)
	}
	i.metrics.flushedChunksStats.Inc(1)
	return nil
}

// reportFlushedChunkStatistics calculate overall statistics of flushed chunks without compromising the flush process.
func (i *Ingester) reportFlushedChunkStatistics(ch *chunk.Chunk, desc *chunkDesc, sizePerTenant prometheus.Counter, countPerTenant prometheus.Counter, reason string) {
	byt, err := ch.Encoded()
	if err != nil {
		level.Error(util_log.Logger).Log("msg", "failed to encode flushed wire chunk", "err", err)
		return
	}

	i.metrics.chunksFlushedPerReason.WithLabelValues(reason).Add(1)

	compressedSize := float64(len(byt))
	uncompressedSize, ok := chunkenc.UncompressedSize(ch.Data)

	if ok && compressedSize > 0 {
		i.metrics.chunkCompressionRatio.Observe(float64(uncompressedSize) / compressedSize)
	}

	utilization := ch.Data.Utilization()
	i.metrics.chunkUtilization.Observe(utilization)
	numEntries := desc.chunk.Size()
	i.metrics.chunkEntries.Observe(float64(numEntries))
	i.metrics.chunkSize.Observe(compressedSize)
	sizePerTenant.Add(compressedSize)
	countPerTenant.Inc()

	boundsFrom, boundsTo := desc.chunk.Bounds()
	i.metrics.chunkAge.Observe(time.Since(boundsFrom).Seconds())
	i.metrics.chunkLifespan.Observe(boundsTo.Sub(boundsFrom).Hours())

	i.metrics.flushedChunksBytesStats.Record(compressedSize)
	i.metrics.flushedChunksLinesStats.Record(float64(numEntries))
	i.metrics.flushedChunksUtilizationStats.Record(utilization)
	i.metrics.flushedChunksAgeStats.Record(time.Since(boundsFrom).Seconds())
	i.metrics.flushedChunksLifespanStats.Record(boundsTo.Sub(boundsFrom).Seconds())
}
