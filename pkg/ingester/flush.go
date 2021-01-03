package ingester

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	"golang.org/x/net/context"

	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/weaveworks/common/user"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/util"

	"github.com/grafana/loki/pkg/chunkenc"
	loki_util "github.com/grafana/loki/pkg/util"
)

var (
	chunkUtilization = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: "loki",
		Name:      "ingester_chunk_utilization",
		Help:      "Distribution of stored chunk utilization (when stored).",
		Buckets:   prometheus.LinearBuckets(0, 0.2, 6),
	})
	memoryChunks = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "loki",
		Name:      "ingester_memory_chunks",
		Help:      "The total number of chunks in memory.",
	})
	chunkEntries = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: "loki",
		Name:      "ingester_chunk_entries",
		Help:      "Distribution of stored lines per chunk (when stored).",
		Buckets:   prometheus.ExponentialBuckets(200, 2, 9), // biggest bucket is 200*2^(9-1) = 51200
	})
	chunkSize = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: "loki",
		Name:      "ingester_chunk_size_bytes",
		Help:      "Distribution of stored chunk sizes (when stored).",
		Buckets:   prometheus.ExponentialBuckets(20000, 2, 10), // biggest bucket is 20000*2^(10-1) = 10,240,000 (~10.2MB)
	})
	chunkCompressionRatio = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: "loki",
		Name:      "ingester_chunk_compression_ratio",
		Help:      "Compression ratio of chunks (when stored).",
		Buckets:   prometheus.LinearBuckets(.75, 2, 10),
	})
	chunksPerTenant = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "loki",
		Name:      "ingester_chunks_stored_total",
		Help:      "Total stored chunks per tenant.",
	}, []string{"tenant"})
	chunkSizePerTenant = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "loki",
		Name:      "ingester_chunk_stored_bytes_total",
		Help:      "Total bytes stored in chunks per tenant.",
	}, []string{"tenant"})
	chunkAge = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: "loki",
		Name:      "ingester_chunk_age_seconds",
		Help:      "Distribution of chunk ages (when stored).",
		// with default settings chunks should flush between 5 min and 12 hours
		// so buckets at 1min, 5min, 10min, 30min, 1hr, 2hr, 4hr, 10hr, 12hr, 16hr
		Buckets: []float64{60, 300, 600, 1800, 3600, 7200, 14400, 36000, 43200, 57600},
	})
	chunkEncodeTime = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: "loki",
		Name:      "ingester_chunk_encode_time_seconds",
		Help:      "Distribution of chunk encode times.",
		// 10ms to 10s.
		Buckets: prometheus.ExponentialBuckets(0.01, 4, 6),
	})
	chunksFlushedPerReason = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "loki",
		Name:      "ingester_chunks_flushed_total",
		Help:      "Total flushed chunks per reason.",
	}, []string{"reason"})
	chunkLifespan = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: "loki",
		Name:      "ingester_chunk_bounds_hours",
		Help:      "Distribution of chunk end-start durations.",
		// 1h -> 8hr
		Buckets: prometheus.LinearBuckets(1, 1, 8),
	})
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

// Flush triggers a flush of all the chunks and closes the flush queues.
// Called from the Lifecycler as part of the ingester shutdown.
func (i *Ingester) Flush() {
	i.sweepUsers(true)

	// Close the flush queues, to unblock waiting workers.
	for _, flushQueue := range i.flushQueues {
		flushQueue.Close()
	}

	i.flushQueuesDone.Wait()
}

// FlushHandler triggers a flush of all in memory chunks.  Mainly used for
// local testing.
func (i *Ingester) FlushHandler(w http.ResponseWriter, _ *http.Request) {
	i.sweepUsers(true)
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
func (i *Ingester) sweepUsers(immediate bool) {
	instances := i.getInstances()

	for _, instance := range instances {
		i.sweepInstance(instance, immediate)
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

// must hold streamsMtx
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

	chunks, labels, chunkMtx := i.collectChunksToFlush(instance, fp, immediate)
	if len(chunks) < 1 {
		return nil
	}

	ctx := user.InjectOrgID(context.Background(), userID)
	ctx, cancel := context.WithTimeout(ctx, i.cfg.FlushOpTimeout)
	defer cancel()
	err := i.flushChunks(ctx, fp, labels, chunks, chunkMtx)
	if err != nil {
		return err
	}

	return nil
}

func (i *Ingester) collectChunksToFlush(instance *instance, fp model.Fingerprint, immediate bool) ([]*chunkDesc, labels.Labels, *sync.RWMutex) {
	instance.streamsMtx.Lock()
	stream, ok := instance.streamsByFP[fp]
	instance.streamsMtx.Unlock()

	if !ok {
		return nil, nil, nil
	}

	var result []*chunkDesc
	stream.chunkMtx.Lock()
	defer stream.chunkMtx.Unlock()
	for j := range stream.chunks {
		shouldFlush, reason := i.shouldFlushChunk(&stream.chunks[j])
		if immediate || shouldFlush {
			// Ensure no more writes happen to this chunk.
			if !stream.chunks[j].closed {
				stream.chunks[j].closed = true
			}
			// Flush this chunk if it hasn't already been successfully flushed.
			if stream.chunks[j].flushed.IsZero() {
				result = append(result, &stream.chunks[j])
				if immediate {
					reason = flushReasonForced
				}
				chunksFlushedPerReason.WithLabelValues(reason).Add(1)
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

// must hold streamsMtx
func (i *Ingester) removeFlushedChunks(instance *instance, stream *stream) {
	now := time.Now()

	stream.chunkMtx.Lock()
	defer stream.chunkMtx.Unlock()
	prevNumChunks := len(stream.chunks)
	for len(stream.chunks) > 0 {
		if stream.chunks[0].flushed.IsZero() || now.Sub(stream.chunks[0].flushed) < i.cfg.RetainPeriod {
			break
		}

		stream.chunks[0].chunk = nil // erase reference so the chunk can be garbage-collected
		stream.chunks = stream.chunks[1:]
	}
	memoryChunks.Sub(float64(prevNumChunks - len(stream.chunks)))

	if len(stream.chunks) == 0 {
		delete(instance.streamsByFP, stream.fp)
		delete(instance.streams, stream.labelsString)
		instance.index.Delete(stream.labels, stream.fp)
		instance.streamsRemovedTotal.Inc()
		memoryStreams.WithLabelValues(instance.instanceID).Dec()
	}
}

func (i *Ingester) flushChunks(ctx context.Context, fp model.Fingerprint, labelPairs labels.Labels, cs []*chunkDesc, chunkMtx sync.Locker) error {
	userID, err := user.ExtractOrgID(ctx)
	if err != nil {
		return err
	}

	labelsBuilder := labels.NewBuilder(labelPairs)
	labelsBuilder.Set(nameLabel, logsValue)
	metric := labelsBuilder.Labels()

	wireChunks := make([]chunk.Chunk, 0, len(cs))

	// use anonymous function to make lock releasing simpler.
	err = func() error {
		chunkMtx.Lock()
		defer chunkMtx.Unlock()

		for _, c := range cs {
			// Ensure that new blocks are cut before flushing as data in the head block is not included otherwise.
			if err := c.chunk.Close(); err != nil {
				return err
			}
			firstTime, lastTime := loki_util.RoundToMilliseconds(c.chunk.Bounds())
			c := chunk.NewChunk(
				userID, fp, metric,
				chunkenc.NewFacade(c.chunk, i.cfg.BlockSize, i.cfg.TargetChunkSize),
				firstTime,
				lastTime,
			)

			start := time.Now()
			if err := c.Encode(); err != nil {
				return err
			}
			chunkEncodeTime.Observe(time.Since(start).Seconds())
			wireChunks = append(wireChunks, c)
		}
		return nil
	}()

	if err != nil {
		return err
	}

	if err := i.store.Put(ctx, wireChunks); err != nil {
		return err
	}

	// Record statistics only when actual put request did not return error.
	sizePerTenant := chunkSizePerTenant.WithLabelValues(userID)
	countPerTenant := chunksPerTenant.WithLabelValues(userID)

	chunkMtx.Lock()
	defer chunkMtx.Unlock()

	for i, wc := range wireChunks {

		// flush successful, write while we have lock
		cs[i].flushed = time.Now()

		numEntries := cs[i].chunk.Size()
		byt, err := wc.Encoded()
		if err != nil {
			continue
		}

		compressedSize := float64(len(byt))
		uncompressedSize, ok := chunkenc.UncompressedSize(wc.Data)

		if ok && compressedSize > 0 {
			chunkCompressionRatio.Observe(float64(uncompressedSize) / compressedSize)
		}

		chunkUtilization.Observe(wc.Data.Utilization())
		chunkEntries.Observe(float64(numEntries))
		chunkSize.Observe(compressedSize)
		sizePerTenant.Add(compressedSize)
		countPerTenant.Inc()
		firstTime, lastTime := cs[i].chunk.Bounds()
		chunkAge.Observe(time.Since(firstTime).Seconds())
		chunkLifespan.Observe(lastTime.Sub(firstTime).Hours())
	}

	return nil
}
