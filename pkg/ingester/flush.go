package ingester

import (
	"fmt"
	"net/http"
	"time"

	"golang.org/x/net/context"

	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/common/model"
	"github.com/weaveworks/common/user"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/ingester/client"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/grafana/loki/pkg/chunkenc"
)

const (
	// Backoff for retrying 'immediate' flushes. Only counts for queue
	// position, not wallclock time.
	flushBackoff = 1 * time.Second

	nameLabel = model.LabelName("__name__")
	logsValue = model.LabelValue("logs")
)

// Flush triggers a flush of all the chunks and closes the flush queues.
// Called from the Lifecycler as part of the ingester shutdown.
func (i *Ingester) Flush() {
	i.sweepInstances(true)

	// Close the flush queues, to unblock waiting workers.
	for _, flushQueue := range i.flushQueues {
		flushQueue.Close()
	}

	i.flushQueuesDone.Wait()
}

// FlushHandler triggers a flush of all in memory chunks.  Mainly used for
// local testing.
func (i *Ingester) FlushHandler(w http.ResponseWriter, _ *http.Request) {
	i.sweepInstances(true)
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
func (i *Ingester) sweepInstances(immediate bool) {
	instances := i.getInstances()
	for _, instance := range instances {
		i.sweepInstance(instance, immediate)
	}

	// Must take write lock on instance before write lock on streams, to
	// prevent deadlock with write path.
	i.instancesMtx.Lock()
	defer i.instancesMtx.Unlock()
	for _, instance := range i.instances {
		instance.streamsMtx.Lock()
		if len(instance.streams) == 0 {
			delete(i.instances, instance.instanceID)
			memoryTenants.Dec()
		}
		instance.streamsMtx.Unlock()
	}
}

func (i *Ingester) sweepInstance(instance *instance, immediate bool) {
	instance.streamsMtx.Lock()
	defer instance.streamsMtx.Unlock()

	for _, stream := range instance.streams {
		i.sweepStream(instance, stream, immediate)
		i.removeFlushedChunks(instance, stream)
	}
}

func (i *Ingester) sweepStream(instance *instance, stream *stream, immediate bool) {
	if len(stream.chunks) == 0 {
		return
	}

	lastChunk := stream.chunks[len(stream.chunks)-1]
	if len(stream.chunks) == 1 && time.Since(lastChunk.lastUpdated) < i.cfg.MaxChunkIdle && !immediate {
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
		level.Debug(util.Logger).Log("msg", "Ingester.flushLoop() exited")
		i.flushQueuesDone.Done()
	}()

	for {
		o := i.flushQueues[j].Dequeue()
		if o == nil {
			return
		}
		op := o.(*flushOp)

		level.Debug(util.Logger).Log("msg", "flushing stream", "userid", op.userID, "fp", op.fp, "immediate", op.immediate)

		err := i.flushUserSeries(op.userID, op.fp, op.immediate)
		if err != nil {
			level.Error(util.WithUserID(op.userID, util.Logger)).Log("msg", "failed to flush user", "err", err)
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

	chunks, labels := i.collectChunksToFlush(instance, fp, immediate)
	if len(chunks) < 1 {
		return nil
	}

	ctx := user.InjectOrgID(context.Background(), userID)
	ctx, cancel := context.WithTimeout(ctx, i.cfg.FlushOpTimeout)
	defer cancel()
	err := i.flushChunks(ctx, fp, labels, chunks)
	if err != nil {
		return err
	}

	instance.streamsMtx.Lock()
	for _, chunk := range chunks {
		chunk.flushed = time.Now()
	}
	instance.streamsMtx.Unlock()
	return nil
}

func (i *Ingester) collectChunksToFlush(instance *instance, fp model.Fingerprint, immediate bool) ([]*chunkDesc, []client.LabelPair) {
	instance.streamsMtx.Lock()
	defer instance.streamsMtx.Unlock()

	stream, ok := instance.streams[fp]
	if !ok {
		return nil, nil
	}

	var result []*chunkDesc
	for j := range stream.chunks {
		if immediate || i.shouldFlushChunk(&stream.chunks[j]) {
			result = append(result, &stream.chunks[j])
		}
	}
	return result, stream.labels
}

func (i *Ingester) shouldFlushChunk(chunk *chunkDesc) bool {
	if !chunk.flushed.IsZero() {
		return false
	}

	// Append should close the chunk when the a new one is added.
	if chunk.closed {
		return true
	}

	if time.Since(chunk.lastUpdated) > i.cfg.MaxChunkIdle {
		chunk.closed = true
		return true
	}

	return false
}

func (i *Ingester) removeFlushedChunks(instance *instance, stream *stream) {
	now := time.Now()

	for len(stream.chunks) > 0 {
		if stream.chunks[0].flushed.IsZero() || now.Sub(stream.chunks[0].flushed) < i.cfg.RetainPeriod {
			break
		}

		stream.chunks[0].chunk = nil // erase reference so the chunk can be garbage-collected
		stream.chunks = stream.chunks[1:]
	}

	if len(stream.chunks) == 0 {
		delete(instance.streams, stream.fp)
		instance.streamsRemovedTotal.Inc()
		memoryStreams.Dec()
	}
}

func (i *Ingester) flushChunks(ctx context.Context, fp model.Fingerprint, labelPairs []client.LabelPair, cs []*chunkDesc) error {
	userID, err := user.ExtractOrgID(ctx)
	if err != nil {
		return err
	}

	metric := fromLabelPairs(labelPairs)
	metric[nameLabel] = logsValue

	wireChunks := make([]chunk.Chunk, 0, len(cs))
	for _, c := range cs {
		firstTime, lastTime := c.chunk.Bounds()
		wireChunks = append(wireChunks, chunk.NewChunk(
			userID, fp, metric,
			chunkenc.NewFacade(c.chunk),
			model.TimeFromUnixNano(firstTime.UnixNano()),
			model.TimeFromUnixNano(lastTime.UnixNano()),
		))
	}

	chunksFlushedTotal.Add(float64(len(wireChunks)))
	return i.store.Put(ctx, wireChunks)
}

func fromLabelPairs(ls []client.LabelPair) model.Metric {
	m := make(model.Metric, len(ls))
	for _, l := range ls {
		m[model.LabelName(l.Name)] = model.LabelValue(l.Value)
	}
	return m
}
