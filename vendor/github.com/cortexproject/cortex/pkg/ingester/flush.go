package ingester

import (
	"fmt"
	"net/http"
	"time"

	"golang.org/x/net/context"

	"github.com/go-kit/kit/log/level"
	ot "github.com/opentracing/opentracing-go"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/common/model"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/ingester/client"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/weaveworks/common/user"
)

const (
	// Backoff for retrying 'immediate' flushes. Only counts for queue
	// position, not wallclock time.
	flushBackoff = 1 * time.Second
)

var (
	chunkUtilization = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "cortex_ingester_chunk_utilization",
		Help:    "Distribution of stored chunk utilization (when stored).",
		Buckets: prometheus.LinearBuckets(0, 0.2, 6),
	})
	chunkLength = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "cortex_ingester_chunk_length",
		Help:    "Distribution of stored chunk lengths (when stored).",
		Buckets: prometheus.ExponentialBuckets(5, 2, 11), // biggest bucket is 5*2^(11-1) = 5120
	})
	chunkSize = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "cortex_ingester_chunk_size_bytes",
		Help:    "Distribution of stored chunk sizes (when stored).",
		Buckets: prometheus.ExponentialBuckets(500, 2, 5), // biggest bucket is 500*2^(5-1) = 8000
	})
	chunksPerUser = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "cortex_ingester_chunks_stored_total",
		Help: "Total stored chunks per user.",
	}, []string{"user"})
	chunkSizePerUser = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "cortex_ingester_chunk_stored_bytes_total",
		Help: "Total bytes stored in chunks per user.",
	}, []string{"user"})
	chunkAge = promauto.NewHistogram(prometheus.HistogramOpts{
		Name: "cortex_ingester_chunk_age_seconds",
		Help: "Distribution of chunk ages (when stored).",
		// with default settings chunks should flush between 5 min and 12 hours
		// so buckets at 1min, 5min, 10min, 30min, 1hr, 2hr, 4hr, 10hr, 12hr, 16hr
		Buckets: []float64{60, 300, 600, 1800, 3600, 7200, 14400, 36000, 43200, 57600},
	})
	memoryChunks = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "cortex_ingester_memory_chunks",
		Help: "The total number of chunks in memory.",
	})
	flushReasons = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "cortex_ingester_flush_reasons",
		Help: "Total number of series scheduled for flushing, with reasons.",
	}, []string{"reason"})
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
func (i *Ingester) FlushHandler(w http.ResponseWriter, r *http.Request) {
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
	return fmt.Sprintf("%s-%d-%v", o.userID, o.fp, o.immediate)
}

func (o *flushOp) Priority() int64 {
	return -int64(o.from)
}

// sweepUsers periodically schedules series for flushing and garbage collects users with no series
func (i *Ingester) sweepUsers(immediate bool) {
	if i.chunkStore == nil {
		return
	}

	for id, state := range i.userStates.cp() {
		for pair := range state.fpToSeries.iter() {
			state.fpLocker.Lock(pair.fp)
			i.sweepSeries(id, pair.fp, pair.series, immediate)
			i.removeFlushedChunks(state, pair.fp, pair.series)
			state.fpLocker.Unlock(pair.fp)
		}
	}
}

type flushReason int

const (
	noFlush = iota
	reasonImmediate
	reasonMultipleChunksInSeries
	reasonAged
	reasonIdle
)

func (f flushReason) String() string {
	switch f {
	case noFlush:
		return "NoFlush"
	case reasonImmediate:
		return "Immediate"
	case reasonMultipleChunksInSeries:
		return "MultipleChunksInSeries"
	case reasonAged:
		return "Aged"
	case reasonIdle:
		return "Idle"
	default:
		panic("unrecognised flushReason")
	}
}

// sweepSeries schedules a series for flushing based on a set of criteria
//
// NB we don't close the head chunk here, as the series could wait in the queue
// for some time, and we want to encourage chunks to be as full as possible.
func (i *Ingester) sweepSeries(userID string, fp model.Fingerprint, series *memorySeries, immediate bool) {
	if len(series.chunkDescs) <= 0 {
		return
	}

	firstTime := series.firstTime()
	flush := i.shouldFlushSeries(series, fp, immediate)
	if flush == noFlush {
		return
	}

	flushQueueIndex := int(uint64(fp) % uint64(i.cfg.ConcurrentFlushes))
	if i.flushQueues[flushQueueIndex].Enqueue(&flushOp{firstTime, userID, fp, immediate}) {
		flushReasons.WithLabelValues(flush.String()).Inc()
		util.Event().Log("msg", "add to flush queue", "userID", userID, "reason", flush, "firstTime", firstTime, "fp", fp, "series", series.metric, "queue", flushQueueIndex)
	}
}

func (i *Ingester) shouldFlushSeries(series *memorySeries, fp model.Fingerprint, immediate bool) flushReason {
	if immediate {
		return reasonImmediate
	}

	// Flush if we have more than one chunk, and haven't already flushed the first chunk
	if len(series.chunkDescs) > 1 && !series.chunkDescs[0].flushed {
		return reasonMultipleChunksInSeries
	} else if len(series.chunkDescs) > 0 {
		// Otherwise look in more detail at the first chunk
		return i.shouldFlushChunk(series.chunkDescs[0], fp)
	}

	return noFlush
}

func (i *Ingester) shouldFlushChunk(c *desc, fp model.Fingerprint) flushReason {
	if c.flushed { // don't flush chunks we've already flushed
		return noFlush
	}

	// Adjust max age slightly to spread flushes out over time
	var jitter time.Duration
	if i.cfg.ChunkAgeJitter != 0 {
		jitter = time.Duration(fp) % i.cfg.ChunkAgeJitter
	}
	// Chunks should be flushed if they span longer than MaxChunkAge
	if c.LastTime.Sub(c.FirstTime) > (i.cfg.MaxChunkAge - jitter) {
		return reasonAged
	}

	// Chunk should be flushed if their last update is older then MaxChunkIdle
	if model.Now().Sub(c.LastUpdate) > i.cfg.MaxChunkIdle {
		return reasonIdle
	}

	return noFlush
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

		err := i.flushUserSeries(j, op.userID, op.fp, op.immediate)
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

func (i *Ingester) flushUserSeries(flushQueueIndex int, userID string, fp model.Fingerprint, immediate bool) error {
	if i.preFlushUserSeries != nil {
		i.preFlushUserSeries()
	}

	userState, ok := i.userStates.get(userID)
	if !ok {
		return nil
	}

	series, ok := userState.fpToSeries.get(fp)
	if !ok {
		return nil
	}

	userState.fpLocker.Lock(fp)
	reason := i.shouldFlushSeries(series, fp, immediate)
	if reason == noFlush {
		userState.fpLocker.Unlock(fp)
		return nil
	}

	// Assume we're going to flush everything, and maybe don't flush the head chunk if it doesn't need it.
	chunks := series.chunkDescs
	if immediate || (len(chunks) > 0 && i.shouldFlushChunk(series.head(), fp) != noFlush) {
		series.closeHead()
	} else {
		chunks = chunks[:len(chunks)-1]
	}
	userState.fpLocker.Unlock(fp)

	if len(chunks) == 0 {
		return nil
	}

	// flush the chunks without locking the series, as we don't want to hold the series lock for the duration of the dynamo/s3 rpcs.
	ctx := user.InjectOrgID(context.Background(), userID)
	ctx, cancel := context.WithTimeout(ctx, i.cfg.FlushOpTimeout)
	defer cancel() // releases resources if slowOperation completes before timeout elapses

	sp, ctx := ot.StartSpanFromContext(ctx, "flushUserSeries")
	defer sp.Finish()
	sp.SetTag("organization", userID)

	util.Event().Log("msg", "flush chunks", "userID", userID, "reason", reason, "numChunks", len(chunks), "firstTime", chunks[0].FirstTime, "fp", fp, "series", series.metric, "queue", flushQueueIndex)
	err := i.flushChunks(ctx, fp, client.FromLabelPairs(series.metric), chunks)
	if err != nil {
		return err
	}

	userState.fpLocker.Lock(fp)
	if immediate {
		userState.removeSeries(fp, series.labels())
		memoryChunks.Sub(float64(len(chunks)))
	} else {
		for i := 0; i < len(chunks); i++ {
			// mark the chunks as flushed, so we can remove them after the retention period
			series.chunkDescs[i].flushed = true
			series.chunkDescs[i].LastUpdate = model.Now()
		}
	}
	userState.fpLocker.Unlock(fp)
	return nil
}

// must be called under fpLocker lock
func (i *Ingester) removeFlushedChunks(userState *userState, fp model.Fingerprint, series *memorySeries) {
	now := model.Now()
	for len(series.chunkDescs) > 0 {
		if series.chunkDescs[0].flushed && now.Sub(series.chunkDescs[0].LastUpdate) > i.cfg.RetainPeriod {
			series.chunkDescs[0] = nil // erase reference so the chunk can be garbage-collected
			series.chunkDescs = series.chunkDescs[1:]
			memoryChunks.Dec()
		} else {
			break
		}
	}
	if len(series.chunkDescs) == 0 {
		userState.removeSeries(fp, series.labels())
	}
}

func (i *Ingester) flushChunks(ctx context.Context, fp model.Fingerprint, metric model.Metric, chunkDescs []*desc) error {
	userID, err := user.ExtractOrgID(ctx)
	if err != nil {
		return err
	}

	wireChunks := make([]chunk.Chunk, 0, len(chunkDescs))
	for _, chunkDesc := range chunkDescs {
		c := chunk.NewChunk(userID, fp, metric, chunkDesc.C, chunkDesc.FirstTime, chunkDesc.LastTime)
		if err := c.Encode(); err != nil {
			return err
		}
		wireChunks = append(wireChunks, c)
	}

	if err := i.chunkStore.Put(ctx, wireChunks); err != nil {
		return err
	}

	sizePerUser := chunkSizePerUser.WithLabelValues(userID)
	countPerUser := chunksPerUser.WithLabelValues(userID)
	// Record statistsics only when actual put request did not return error.
	for _, chunkDesc := range chunkDescs {
		utilization, length, size := chunkDesc.C.Utilization(), chunkDesc.C.Len(), chunkDesc.C.Size()
		util.Event().Log("msg", "chunk flushed", "userID", userID, "fp", fp, "series", metric, "utilization", utilization, "length", length, "size", size, "firstTime", chunkDesc.FirstTime, "lastTime", chunkDesc.LastTime)
		chunkUtilization.Observe(utilization)
		chunkLength.Observe(float64(length))
		chunkSize.Observe(float64(size))
		sizePerUser.Add(float64(size))
		countPerUser.Inc()
		chunkAge.Observe(model.Now().Sub(chunkDesc.FirstTime).Seconds())
	}

	return nil
}
