package ingester_rf1

import (
	"bytes"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/backoff"
	"github.com/grafana/dskit/ring"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"golang.org/x/net/context"

	"github.com/grafana/dskit/tenant"

	"github.com/grafana/loki/v3/pkg/chunkenc"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/util"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
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
		i.flushQueues[j] = util.NewPriorityQueue(i.metrics.flushQueueLength)
		go i.flushLoop(j)
	}
}

// Flush implements ring.FlushTransferer
// Flush triggers a flush of all the chunks and closes the flush queues.
// Called from the Lifecycler as part of the ingester shutdown.
func (i *Ingester) Flush() {
	i.flush()
}

// TransferOut implements ring.FlushTransferer
// Noop implemenetation because ingesters have a WAL now that does not require transferring chunks any more.
// We return ErrTransferDisabled to indicate that we don't support transfers, and therefore we may flush on shutdown if configured to do so.
func (i *Ingester) TransferOut(_ context.Context) error {
	return ring.ErrTransferDisabled
}

func (i *Ingester) flush() {
	// TODO: Flush the last chunks
	// Close the flush queues, to unblock waiting workers.
	for _, flushQueue := range i.flushQueues {
		flushQueue.Close()
	}

	i.flushQueuesDone.Wait()
	level.Debug(i.logger).Log("msg", "flush queues have drained")
}

// FlushHandler triggers a flush of all in memory chunks.  Mainly used for
// local testing.
func (i *Ingester) FlushHandler(w http.ResponseWriter, _ *http.Request) {
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

func (i *Ingester) flushLoop(j int) {
	l := log.With(i.logger, "loop", j)
	defer func() {
		level.Debug(l).Log("msg", "Ingester.flushLoop() exited")
		i.flushQueuesDone.Done()
	}()

	for {
		o := i.flushQueues[j].Dequeue()
		if o == nil {
			return
		}
		op := o.(*flushOp)

		m := util_log.WithUserID(op.userID, l)
		err := i.flushOp(m, op)
		if err != nil {
			level.Error(m).Log("msg", "failed to flush", "err", err)
		}

		// If we're exiting & we failed to flush, put the failed operation
		// back in the queue at a later point.
		if op.immediate && err != nil {
			op.from = op.from.Add(flushBackoff)
			i.flushQueues[j].Enqueue(op)
		}
	}
}

func (i *Ingester) flushOp(l log.Logger, op *flushOp) error {
	ctx, cancelFunc := context.WithCancel(context.Background())
	defer cancelFunc()

	b := backoff.New(ctx, i.cfg.FlushOpBackoff)
	for b.Ongoing() {
		/*		err := i.flushUserSeries(ctx, op.userID, op.fp, op.immediate)
				if err == nil {
					break
				}*/
		//level.Error(l).Log("msg", "failed to flush", "retries", b.NumRetries(), "err", err)
		b.Wait()
	}
	return b.Err()
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
		i.metrics.chunksFlushFailures.Inc()
		return fmt.Errorf("store put chunk: %w", err)
	}
	i.metrics.flushedChunksStats.Inc(1)
	return nil
}

// reportFlushedChunkStatistics calculate overall statistics of flushed chunks without compromising the flush process.
func (i *Ingester) reportFlushedChunkStatistics(ch *chunk.Chunk, desc *chunkDesc, sizePerTenant prometheus.Counter, countPerTenant prometheus.Counter, reason string) {
	byt, err := ch.Encoded()
	if err != nil {
		level.Error(i.logger).Log("msg", "failed to encode flushed wire chunk", "err", err)
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
