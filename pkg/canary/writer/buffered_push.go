package writer

import (
	"context"
	"sync"
	"time"

	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/backoff"
	"github.com/grafana/loki/v3/pkg/logproto"
)

const (
	DefaultLogBatchSize    = 1  // don't batch logs -- just send immediately
	DefaultLogBatchTimeout = 30 // every 'n' seconds, force the batch to flush if it's not empty
)

// `logBatch` is a wrapper struct providing locking around the
// array of log lines which are batched before being sent.
//
// This struct implements a number of array access functions
// which first acquire the lock and then access the
// internal array structure.  All lock releases are done
// via `defer`
type logBatch struct {
	sync.Mutex
	lines []entry
}

// lock the array and push the `entry` to the internal array
func (bf *logBatch) append(e entry) {
	bf.Lock()
	defer func() {
		bf.Unlock()
	}()
	bf.lines = append(bf.lines, e)
}

// lock the array and allow iteration across entries
// in the array
func (bf *logBatch) entries(yield func(entry) bool) {
	bf.Lock()
	defer func() {
		bf.Unlock()
	}()

	for _, e := range bf.lines {
		if !yield(e) {
			return
		}
	}
}

// lock the array and remove everything in it
func (bf *logBatch) clear() {
	bf.Lock()
	defer func() {
		bf.Unlock()
	}()

	bf.lines = bf.lines[:0]
}

// lock the array and return the # of entries
func (bf *logBatch) length() int {
	bf.Lock()
	defer func() {
		bf.Unlock()
	}()
	return len(bf.lines)
}

// lock the array and return the first entry
func (bf *logBatch) first() (*entry, bool) {
	bf.Lock()
	defer func() {
		bf.Unlock()
	}()

	if len(bf.lines) == 0 {
		return nil, false
	}
	return &bf.lines[0], true
}

// lock the array and return the last entry
func (bf *logBatch) last() (*entry, bool) {
	bf.Lock()
	defer func() {
		bf.Unlock()
	}()

	if len(bf.lines) == 0 {
		return nil, false
	}
	return &bf.lines[len(bf.lines)-1], true
}

func newLogsBatch(bufferSize int) logBatch {
	return logBatch{
		lines: make([]entry, 0, bufferSize),
	}
}

// BatchedPush is an extension of a Push writer.  This Writer implements
// the standard `EntryWriter` functions, but implements a different push mechanism
// from the standard push.
//
// Rather than receiving a log and pushing a log (effectively, a batch size of 1),
// this writer builds logs up in-memory and pushes/flushes them based on a few conditions:
//  1. the batch of logs hits a threshold
//  2. a timeout of 30s has been reached
//  3. the writer is terminating and flushes all remaining logs
//
// This Writer is given a standard Push writer; this writer's standard channels
// and base functionality is used for listening for logs, building serializable
// objects, and whatnot
type BatchedPush struct {
	pusher       *Push
	logBatchSize int
}

// `buildPayload` receives the array of log lines and converts them
// to a serialized byte array which may be pushed to the loki endpoint.
func (p *BatchedPush) buildPayload(logs *logBatch) ([]byte, error) {
	streams := make([]logproto.Stream, 0, logs.length())

	for e := range logs.entries {
		streams = append(streams, p.pusher.buildStream(e))
	}

	return p.pusher.serializePayload(&logproto.PushRequest{Streams: streams})
}

// implements `EntryWriter` by delegating to the `Push` reference
func (p *BatchedPush) WriteEntry(ts time.Time, e string) {
	p.pusher.WriteEntry(ts, e)
}

// implements `EntryWriter` by delegating to the `Push` reference
func (p *BatchedPush) Stop() {
	p.pusher.Stop()
}

// `run` pulls lines from the `Push` channel and batches them to be pushed
// to loki.  Once the batch is "full", or after 30s has transpired, the
// log lines are then serialized to a byte array and the `Push.send()` function
// is used to send them to loki.
//
// If the `quit` channel is poked, this function will flush the remaining log
// lines and then terminate.
func (p *BatchedPush) run() {
	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		close(p.pusher.done)
	}()

	// lockable structure to keep track of log lines as we receive from the channel
	logs := newLogsBatch(p.logBatchSize)

	// use a channel to flush the logs array every 30s at minimum
	forceFlush := make(chan bool, 1)
	go func() {
		time.Sleep(DefaultLogBatchTimeout * time.Second)
		forceFlush <- true
	}()

	// helper function to force the list of log lines to be serialized and sent to
	// Loki.  This may be invoked when receiving from the different channels
	flush := func() {
		// shortcuts -- if somehow there are no logs, or we can't get the first item, return
		if logs.length() == 0 {
			return
		}

		firstLog, ok := logs.first()
		if !ok {
			return
		}

		oldestTs := firstLog.ts.UnixNano()
		newestTs := oldestTs
		lastLog, ok := logs.last()

		if ok {
			newestTs = lastLog.ts.UnixNano()
		}

		linesSent := logs.length()

		// We will use a timeout within each attempt to send
		backoff := backoff.New(context.Background(), *p.pusher.backoff)

		payload, err := p.buildPayload(&logs)

		// we don't want the log array to grow out-of-bound from repeated failures and
		// we will log a warning if lines get dropped
		// therefore, immediately clear the logs -- we have the serialized content to send to the
		// loki instance, so don't hang on them - the channel can keep appending
		logs.clear()

		if err != nil {
			level.Error(p.pusher.logger).Log("msg", "failed to build payload", "err", err)
		} else {
			for {
				status, err := p.pusher.send(ctx, payload)
				if err == nil {
					break
				}

				if status > 0 && status != 429 && status/100 != 5 {
					level.Error(p.pusher.logger).Log("msg", "failed to send entries, server rejected push with a non-retryable status code", "entry", newestTs, "oldest_entry", oldestTs, "log_line_count", linesSent, "status", status, "err", err)
					break
				}

				if !backoff.Ongoing() {
					level.Error(p.pusher.logger).Log("msg", "failed to send entries, retries exhausted, entries will be dropped", "entry", newestTs, "oldest_entry", oldestTs, "log_line_count", linesSent, "status", status, "error", err)
					break
				}

				level.Warn(p.pusher.logger).Log("msg", "failed to send entries, retrying", "entry", newestTs, "oldest_entry", oldestTs, "log_line_count", linesSent, "status", status, "error", err)
				backoff.Wait()
			}
		}
	}

	for {
		select {
		case <-p.pusher.quit:
			// flush whatever is remaining before we terminate
			flush()
			cancel()
			return
		case <-forceFlush:
			// force a flush if we've exceeded the min duration
			// we don't want to keep logs for too long if the
			// channel is open byt we don't receive more logs
			flush()
		case e := <-p.pusher.entries:
			logs.append(e)
			if logs.length() >= p.logBatchSize {
				flush()
			}
		}
	}
}
