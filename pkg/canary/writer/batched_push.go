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
func (p *BatchedPush) buildPayload(logs []entry) ([]byte, error) {
	streams := make([]logproto.Stream, 0, len(logs))

	for _, e := range logs {
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
	<-p.pusher.quit
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
	logs := make([]entry, 0, p.logBatchSize)

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
		if len(logs) == 0 {
			return
		}

		level.Info(p.pusher.logger).Log("msg", "running a batched push", "log_count", len(logs))
		firstLog := logs[0]

		oldestTs := firstLog.ts.UnixNano()
		newestTs := logs[len(logs)-1]
		linesSent := len(logs)

		// We will use a timeout within each attempt to send
		backoff := backoff.New(context.Background(), *p.pusher.backoff)

		payload, err := p.buildPayload(logs)

		// we don't want the log array to grow out-of-bound from repeated failures and
		// we will log a warning if lines get dropped
		// therefore, immediately clear the logs -- we have the serialized content to send to the
		// loki instance, so don't hang on them - the channel can keep appending
		logs = logs[:0]

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

	var lock sync.Mutex
	withLock := func(runner func()) {
		lock.Lock()
		defer func() {
			lock.Unlock()
		}()
		runner()
	}

	for {
		select {
		case <-p.pusher.quit:
			// flush whatever is remaining before we terminate
			withLock(flush)
			cancel()
			return
		case <-forceFlush:
			// force a flush if we've exceeded the min duration
			// we don't want to keep logs for too long if the
			// channel is open byt we don't receive more logs
			withLock(flush)
		case e := <-p.pusher.entries:
			withLock(func() {
				logs = append(logs, e)
				if len(logs) >= p.logBatchSize {
					flush()
				}
			})
		}
	}
}
