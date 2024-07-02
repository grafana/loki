package ingesterrf1

import (
	"fmt"
	"net/http"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/backoff"
	"github.com/grafana/dskit/ring"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"golang.org/x/net/context"

	"github.com/grafana/loki/v3/pkg/chunkenc"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/storage/wal"
	"github.com/grafana/loki/v3/pkg/util"
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
		op := o.(*flushCtx)

		err := i.flushOp(l, op)
		if err != nil {
			level.Error(l).Log("msg", "failed to flush", "err", err)
			// Immediately re-queue another attempt at flushing this segment.
			// TODO: Add some backoff or something?
			i.flushQueues[j].Enqueue(op)
		} else {
			// Close the channel and trigger all waiting listeners to return
			// TODO: Figure out how to return an error if we want to?
			close(op.flushDone)
		}
	}
}

func (i *Ingester) flushOp(l log.Logger, flushCtx *flushCtx) error {
	ctx, cancelFunc := context.WithCancel(context.Background())
	defer cancelFunc()

	b := backoff.New(ctx, i.cfg.FlushOpBackoff)
	for b.Ongoing() {
		err := i.flushSegment(ctx, flushCtx.segmentWriter)
		if err == nil {
			break
		}
		level.Error(l).Log("msg", "failed to flush", "retries", b.NumRetries(), "err", err)
		b.Wait()
	}
	return b.Err()
}

// flushChunk flushes the given chunk to the store.
//
// If the flush is successful, metrics for this flush are to be reported.
// If the flush isn't successful, the operation for this userID is requeued allowing this and all other unflushed
// segments to have another opportunity to be flushed.
func (i *Ingester) flushSegment(ctx context.Context, ch *wal.SegmentWriter) error {
	if err := i.store.PutWal(ctx, ch); err != nil {
		i.metrics.chunksFlushFailures.Inc()
		return fmt.Errorf("store put chunk: %w", err)
	}
	i.metrics.flushedChunksStats.Inc(1)
	// TODO: report some flush metrics
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
