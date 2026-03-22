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
	DefaultLogBatchSize    = 1  // '1' means don't batch logs -- just send immediately
	DefaultLogBatchSizeMax = 20 // w/1MB log line restriction, don't allow more than 20MB in memory -- used only by the CLI to restrict the logBatchSize input
	DefaultLogBatchTimeout = 30 // every 'n' seconds, force the batch to flush if it's not empty
)

// BatchedPush is wrapper around the `Push` writer.  This writer delegates
// the standard `EntryWriter` functions to the wrapped `Push` writer, but
// reimplements the push mechanism:
//
// Rather than receiving a log and pushing a log (effectively, a batch size of 1),
// this writer builds logs up in-memory and pushes/flushes them based on a few conditions:
//  1. the batch of logs hits the specified limit
//  2. a timeout of 30s has been reached
//  3. the writer is terminating
//
// This writer utilizes the wrapped `Push` struct's internals for listening for logs,
// building serializable objects, etc.
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

// implements `EntryWriter.WriteEntry` by delegating to the `Push` reference
func (p *BatchedPush) WriteEntry(ts time.Time, e string) {
	p.pusher.WriteEntry(ts, e)
}

// implements `EntryWriter.Stop` by delegating to the `Push` reference
func (p *BatchedPush) Stop() {
	p.pusher.Stop()
}

// `run` pulls lines from the `Push` channel and batches them to before sending
// to Loki.  Once the batch is "full", or after 30s has transpired, the log lines
// are then serialized to a byte array and the `Push.send()` function is used
// to send them to Loki.
//
// If the `quit` channel is poked, this function will flush the remaining log
// lines and then terminate.
func (p *BatchedPush) run() {
	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		close(p.pusher.done)
	}()

	// use a channel to flush the logs array every "timeout" seconds at minimum
	forceFlush := make(chan bool, 1)
	go func() {
		time.Sleep(DefaultLogBatchTimeout * time.Second)
		forceFlush <- true
	}()

	logs := make([]entry, 0, p.logBatchSize)

	// helper function to force the list of log lines to be serialized and sent to
	// Loki.  This may be invoked when receiving from the different channels
	flush := func() {
		// shortcuts -- if somehow there are no logs, or we can't get the first item, return
		if len(logs) == 0 {
			return
		}

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

	// helper function to apply a lock to a function before it executes.
	// used to ensure the `logs` array is not checked/manipulated while
	// we are in the process of appending or flushing it
	withLock := func(f func()) {
		defer func() {
			lock.Unlock()
		}()

		lock.Lock()
		f()
	}

	for {
		select {
		case <-p.pusher.quit:
			// ensure all batched logs are pushed before shutting down
			withLock(flush)
			cancel()
			return
		case <-forceFlush:
			// we don't want to keep logs for too long if the
			// channel is open but not receiving new logs
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
