package ingester

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/go-kit/kit/log/level"
	ot "github.com/opentracing/opentracing-go"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"golang.org/x/time/rate"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/log"
)

const (
	// Backoff for retrying 'immediate' flushes. Only counts for queue
	// position, not wallclock time.
	flushBackoff = 1 * time.Second
	// Lower bound on flushes per check period for rate-limiter
	minFlushes = 100
)

// Flush triggers a flush of all the chunks and closes the flush queues.
// Called from the Lifecycler as part of the ingester shutdown.
func (i *Ingester) Flush() {
	if i.cfg.BlocksStorageEnabled {
		i.v2LifecyclerFlush()
		return
	}

	level.Info(i.logger).Log("msg", "starting to flush all the chunks")
	i.sweepUsers(true)
	level.Info(i.logger).Log("msg", "chunks queued for flushing")

	// Close the flush queues, to unblock waiting workers.
	for _, flushQueue := range i.flushQueues {
		flushQueue.Close()
	}

	i.flushQueuesDone.Wait()
	level.Info(i.logger).Log("msg", "flushing of chunks complete")
}

// FlushHandler triggers a flush of all in memory chunks.  Mainly used for
// local testing.
func (i *Ingester) FlushHandler(w http.ResponseWriter, r *http.Request) {
	if i.cfg.BlocksStorageEnabled {
		i.v2FlushHandler(w, r)
		return
	}

	level.Info(i.logger).Log("msg", "starting to flush all the chunks")
	i.sweepUsers(true)
	level.Info(i.logger).Log("msg", "chunks queued for flushing")
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

	oldest := model.Time(0)

	for id, state := range i.userStates.cp() {
		for pair := range state.fpToSeries.iter() {
			state.fpLocker.Lock(pair.fp)
			i.sweepSeries(id, pair.fp, pair.series, immediate)
			i.removeFlushedChunks(state, pair.fp, pair.series)
			first := pair.series.firstUnflushedChunkTime()
			state.fpLocker.Unlock(pair.fp)

			if first > 0 && (oldest == 0 || first < oldest) {
				oldest = first
			}
		}
	}

	i.metrics.oldestUnflushedChunkTimestamp.Set(float64(oldest.Unix()))
	i.setFlushRate()
}

// Compute a rate such to spread calls to the store over nearly all of the flush period,
// for example if we have 600 items in the queue and period 1 min we will send 10.5 per second.
// Note if the store can't keep up with this rate then it doesn't make any difference.
func (i *Ingester) setFlushRate() {
	totalQueueLength := 0
	for _, q := range i.flushQueues {
		totalQueueLength += q.Length()
	}
	const fudge = 1.05 // aim to finish a little bit before the end of the period
	flushesPerSecond := float64(totalQueueLength) / i.cfg.FlushCheckPeriod.Seconds() * fudge
	// Avoid going very slowly with tiny queues
	if flushesPerSecond*i.cfg.FlushCheckPeriod.Seconds() < minFlushes {
		flushesPerSecond = minFlushes / i.cfg.FlushCheckPeriod.Seconds()
	}
	level.Debug(i.logger).Log("msg", "computed flush rate", "rate", flushesPerSecond)
	i.flushRateLimiter.SetLimit(rate.Limit(flushesPerSecond))
}

type flushReason int8

const (
	noFlush = iota
	reasonImmediate
	reasonMultipleChunksInSeries
	reasonAged
	reasonIdle
	reasonStale
	reasonSpreadFlush
	// Following are flush outcomes
	noUser
	noSeries
	noChunks
	flushError
	reasonDropped
	maxFlushReason // Used for testing String() method. Should be last.
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
	case reasonStale:
		return "Stale"
	case reasonSpreadFlush:
		return "Spread"
	case noUser:
		return "NoUser"
	case noSeries:
		return "NoSeries"
	case noChunks:
		return "NoChunksToFlush"
	case flushError:
		return "FlushError"
	case reasonDropped:
		return "Dropped"
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
		i.metrics.seriesEnqueuedForFlush.WithLabelValues(flush.String()).Inc()
		util.Event().Log("msg", "add to flush queue", "userID", userID, "reason", flush, "firstTime", firstTime, "fp", fp, "series", series.metric, "nlabels", len(series.metric), "queue", flushQueueIndex)
	}
}

func (i *Ingester) shouldFlushSeries(series *memorySeries, fp model.Fingerprint, immediate bool) flushReason {
	if len(series.chunkDescs) == 0 {
		return noFlush
	}
	if immediate {
		for _, cd := range series.chunkDescs {
			if !cd.flushed {
				return reasonImmediate
			}
		}
		return noFlush // Everything is flushed.
	}

	// Flush if we have more than one chunk, and haven't already flushed the first chunk
	if len(series.chunkDescs) > 1 && !series.chunkDescs[0].flushed {
		if series.chunkDescs[0].flushReason != noFlush {
			return series.chunkDescs[0].flushReason
		}
		return reasonMultipleChunksInSeries
	}
	// Otherwise look in more detail at the first chunk
	return i.shouldFlushChunk(series.chunkDescs[0], fp, series.isStale())
}

func (i *Ingester) shouldFlushChunk(c *desc, fp model.Fingerprint, lastValueIsStale bool) flushReason {
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

	// Chunk should be flushed if their last update is older then MaxChunkIdle.
	if model.Now().Sub(c.LastUpdate) > i.cfg.MaxChunkIdle {
		return reasonIdle
	}

	// A chunk that has a stale marker can be flushed if possible.
	if i.cfg.MaxStaleChunkIdle > 0 &&
		lastValueIsStale &&
		model.Now().Sub(c.LastUpdate) > i.cfg.MaxStaleChunkIdle {
		return reasonStale
	}

	return noFlush
}

func (i *Ingester) flushLoop(j int) {
	defer func() {
		level.Debug(i.logger).Log("msg", "Ingester.flushLoop() exited")
		i.flushQueuesDone.Done()
	}()

	for {
		o := i.flushQueues[j].Dequeue()
		if o == nil {
			return
		}
		op := o.(*flushOp)

		if !op.immediate {
			_ = i.flushRateLimiter.Wait(context.Background())
		}
		outcome, err := i.flushUserSeries(j, op.userID, op.fp, op.immediate)
		i.metrics.seriesDequeuedOutcome.WithLabelValues(outcome.String()).Inc()
		if err != nil {
			level.Error(log.WithUserID(op.userID, i.logger)).Log("msg", "failed to flush user", "err", err)
		}

		// If we're exiting & we failed to flush, put the failed operation
		// back in the queue at a later point.
		if op.immediate && err != nil {
			op.from = op.from.Add(flushBackoff)
			i.flushQueues[j].Enqueue(op)
		}
	}
}

// Returns flush outcome (either original reason, if series was flushed, noFlush if it doesn't need flushing anymore, or one of the errors)
func (i *Ingester) flushUserSeries(flushQueueIndex int, userID string, fp model.Fingerprint, immediate bool) (flushReason, error) {
	i.metrics.flushSeriesInProgress.Inc()
	defer i.metrics.flushSeriesInProgress.Dec()

	if i.preFlushUserSeries != nil {
		i.preFlushUserSeries()
	}

	userState, ok := i.userStates.get(userID)
	if !ok {
		return noUser, nil
	}

	series, ok := userState.fpToSeries.get(fp)
	if !ok {
		return noSeries, nil
	}

	userState.fpLocker.Lock(fp)
	reason := i.shouldFlushSeries(series, fp, immediate)
	if reason == noFlush {
		userState.fpLocker.Unlock(fp)
		return noFlush, nil
	}

	// shouldFlushSeries() has told us we have at least one chunk.
	// Make a copy of chunks descriptors slice, to avoid possible issues related to removing (and niling) elements from chunkDesc.
	// This can happen if first chunk is already flushed -- removeFlushedChunks may set such chunk to nil.
	// Since elements in the slice are pointers, we can still safely update chunk descriptors after the copy.
	chunks := append([]*desc(nil), series.chunkDescs...)
	if immediate {
		series.closeHead(reasonImmediate)
	} else if chunkReason := i.shouldFlushChunk(series.head(), fp, series.isStale()); chunkReason != noFlush {
		series.closeHead(chunkReason)
	} else {
		// The head chunk doesn't need flushing; step back by one.
		chunks = chunks[:len(chunks)-1]
	}

	if (reason == reasonIdle || reason == reasonStale) && series.headChunkClosed {
		if minChunkLength := i.limits.MinChunkLength(userID); minChunkLength > 0 {
			chunkLength := 0
			for _, c := range chunks {
				chunkLength += c.C.Len()
			}
			if chunkLength < minChunkLength {
				userState.removeSeries(fp, series.metric)
				i.metrics.memoryChunks.Sub(float64(len(chunks)))
				i.metrics.droppedChunks.Add(float64(len(chunks)))
				util.Event().Log(
					"msg", "dropped chunks",
					"userID", userID,
					"numChunks", len(chunks),
					"chunkLength", chunkLength,
					"fp", fp,
					"series", series.metric,
					"queue", flushQueueIndex,
				)
				chunks = nil
				reason = reasonDropped
			}
		}
	}

	userState.fpLocker.Unlock(fp)

	if reason == reasonDropped {
		return reason, nil
	}

	// No need to flush these chunks again.
	for len(chunks) > 0 && chunks[0].flushed {
		chunks = chunks[1:]
	}

	if len(chunks) == 0 {
		return noChunks, nil
	}

	// flush the chunks without locking the series, as we don't want to hold the series lock for the duration of the dynamo/s3 rpcs.
	ctx, cancel := context.WithTimeout(context.Background(), i.cfg.FlushOpTimeout)
	defer cancel() // releases resources if slowOperation completes before timeout elapses

	sp, ctx := ot.StartSpanFromContext(ctx, "flushUserSeries")
	defer sp.Finish()
	sp.SetTag("organization", userID)

	util.Event().Log("msg", "flush chunks", "userID", userID, "reason", reason, "numChunks", len(chunks), "firstTime", chunks[0].FirstTime, "fp", fp, "series", series.metric, "nlabels", len(series.metric), "queue", flushQueueIndex)
	err := i.flushChunks(ctx, userID, fp, series.metric, chunks)
	if err != nil {
		return flushError, err
	}

	userState.fpLocker.Lock(fp)
	for i := 0; i < len(chunks); i++ {
		// Mark the chunks as flushed, so we can remove them after the retention period.
		// We can safely use chunks[i] here, because elements are pointers to chunk descriptors.
		chunks[i].flushed = true
		chunks[i].LastUpdate = model.Now()
	}
	userState.fpLocker.Unlock(fp)
	return reason, err
}

// must be called under fpLocker lock
func (i *Ingester) removeFlushedChunks(userState *userState, fp model.Fingerprint, series *memorySeries) {
	now := model.Now()
	for len(series.chunkDescs) > 0 {
		if series.chunkDescs[0].flushed && now.Sub(series.chunkDescs[0].LastUpdate) > i.cfg.RetainPeriod {
			series.chunkDescs[0] = nil // erase reference so the chunk can be garbage-collected
			series.chunkDescs = series.chunkDescs[1:]
			i.metrics.memoryChunks.Dec()
		} else {
			break
		}
	}
	if len(series.chunkDescs) == 0 {
		userState.removeSeries(fp, series.metric)
	}
}

func (i *Ingester) flushChunks(ctx context.Context, userID string, fp model.Fingerprint, metric labels.Labels, chunkDescs []*desc) error {
	if i.preFlushChunks != nil {
		i.preFlushChunks()
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

	// Record statistics only when actual put request did not return error.
	for _, chunkDesc := range chunkDescs {
		utilization, length, size := chunkDesc.C.Utilization(), chunkDesc.C.Len(), chunkDesc.C.Size()
		util.Event().Log("msg", "chunk flushed", "userID", userID, "fp", fp, "series", metric, "nlabels", len(metric), "utilization", utilization, "length", length, "size", size, "firstTime", chunkDesc.FirstTime, "lastTime", chunkDesc.LastTime)
		i.metrics.chunkUtilization.Observe(utilization)
		i.metrics.chunkLength.Observe(float64(length))
		i.metrics.chunkSize.Observe(float64(size))
		i.metrics.chunkAge.Observe(model.Now().Sub(chunkDesc.FirstTime).Seconds())
	}

	return nil
}
