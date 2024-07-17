package ingesterrf1

import (
	"crypto/rand"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/backoff"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/runutil"
	"github.com/oklog/ulid"
	"golang.org/x/net/context"

	"github.com/grafana/loki/v3/pkg/storage/wal"
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
func (i *Ingester) InitFlushWorkers() {
	i.flushWorkersDone.Add(i.cfg.ConcurrentFlushes)
	for j := 0; j < i.cfg.ConcurrentFlushes; j++ {
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

		start := time.Now()

		// We'll use this to log the size of the segment that was flushed.
		n := it.Writer.InputSize()
		humanized := humanize.Bytes(uint64(n))

		err = i.flushItem(l, it)
		d := time.Since(start)
		if err != nil {
			level.Error(l).Log("msg", "failed to flush", "size", humanized, "duration", d, "err", err)
		} else {
			level.Debug(l).Log("msg", "flushed", "size", humanized, "duration", d)
		}

		it.Result.SetDone(err)
		i.wal.Put(it)
	}
}

func (i *Ingester) flushItem(l log.Logger, it *wal.PendingItem) error {
	ctx, cancelFunc := context.WithCancel(context.Background())
	defer cancelFunc()

	b := backoff.New(ctx, i.cfg.FlushOpBackoff)
	for b.Ongoing() {
		err := i.flushSegment(ctx, it.Writer)
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
	id := ulid.MustNew(ulid.Timestamp(time.Now()), rand.Reader)
	r := ch.Reader()

	start := time.Now()
	defer func() {
		runutil.CloseWithLogOnErr(util_log.Logger, r, "flushSegment")
		i.metrics.flushDuration.Observe(time.Since(start).Seconds())
		ch.Observe()
	}()

	i.metrics.flushesTotal.Add(1)
	if err := i.store.PutObject(ctx, fmt.Sprintf("loki-v2/wal/anon/"+id.String()), r); err != nil {
		i.metrics.flushFailuresTotal.Inc()
		return fmt.Errorf("store put chunk: %w", err)
	}

	return nil
}
