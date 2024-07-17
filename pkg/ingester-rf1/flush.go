package ingesterrf1

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/backoff"
	"github.com/grafana/dskit/ring"
	"github.com/oklog/ulid"
	"golang.org/x/net/context"

	"github.com/grafana/loki/v3/pkg/storage/wal"
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
func (i *Ingester) InitFlushWorkers() {
	i.flushWorkersDone.Add(i.cfg.ConcurrentFlushes)
	for j := 0; j < i.cfg.ConcurrentFlushes; j++ {
		i.flushBuffers[j] = new(bytes.Buffer)
		go i.flushWorker(j)
	}
}

// Flush implements ring.FlushTransferer
// Flush triggers a flush of all the chunks and closes the flush queues.
// Called from the Lifecycler as part of the ingester shutdown.
func (i *Ingester) Flush() {
	i.wal.Close()
	i.flushWorkersDone.Wait()
}

// TransferOut implements ring.FlushTransferer
// Noop implemenetation because ingesters have a WAL now that does not require transferring chunks any more.
// We return ErrTransferDisabled to indicate that we don't support transfers, and therefore we may flush on shutdown if configured to do so.
func (i *Ingester) TransferOut(_ context.Context) error {
	return ring.ErrTransferDisabled
}

// FlushHandler triggers a flush of all in memory chunks.  Mainly used for
// local testing.
func (i *Ingester) FlushHandler(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

func (i *Ingester) flushWorker(j int) {
	l := log.With(i.logger, "worker", j)
	defer func() {
		level.Debug(l).Log("msg", "Ingester.flushWorker() exited")
		i.flushWorkersDone.Done()
	}()

	for {
		it, err := i.wal.NextPending()
		if errors.Is(err, wal.ErrClosed) {
			return
		}

		if it == nil {
			// TODO: Do something more clever here instead.
			time.Sleep(100 * time.Millisecond)
			continue
		}

		err = i.flushItem(l, j, it)
		if err != nil {
			level.Error(l).Log("msg", "failed to flush", "err", err)
		}

		it.Result.SetDone(err)
		i.wal.Put(it)
	}
}

func (i *Ingester) flushItem(l log.Logger, j int, it *wal.PendingItem) error {
	ctx, cancelFunc := context.WithCancel(context.Background())
	defer cancelFunc()

	b := backoff.New(ctx, i.cfg.FlushOpBackoff)
	for b.Ongoing() {
		err := i.flushSegment(ctx, j, it.Writer)
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
func (i *Ingester) flushSegment(ctx context.Context, j int, w *wal.SegmentWriter) error {
	id := ulid.MustNew(ulid.Timestamp(time.Now()), rand.Reader)

	start := time.Now()
	defer func() {
		i.metrics.flushDuration.Observe(time.Since(start).Seconds())
		w.Observe()
	}()

	buf := i.flushBuffers[j]
	defer buf.Reset()
	if _, err := w.WriteTo(buf); err != nil {
		return err
	}

	i.metrics.flushesTotal.Add(1)
	if err := i.store.PutObject(ctx, fmt.Sprintf("loki-v2/wal/anon/"+id.String()), buf); err != nil {
		i.metrics.flushFailuresTotal.Inc()
		return fmt.Errorf("store put chunk: %w", err)
	}

	return nil
}
