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

	"github.com/grafana/loki/v3/pkg/ingester-rf1/metastore/metastorepb"
	"github.com/grafana/loki/v3/pkg/storage/wal"
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

		err = i.flush(l, j, it)
		if err != nil {
			level.Error(l).Log("msg", "failed to flush", "err", err)
		}

		it.Result.SetDone(err)
		i.wal.Put(it)
	}
}

func (i *Ingester) flush(l log.Logger, j int, it *wal.PendingSegment) error {
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

func (i *Ingester) flushSegment(ctx context.Context, j int, w *wal.SegmentWriter) error {
	ctx, cancelFunc := context.WithTimeout(ctx, i.cfg.FlushOpTimeout)
	defer cancelFunc()

	start := time.Now()
	i.metrics.flushesTotal.Add(1)
	defer func() { i.metrics.flushDuration.Observe(time.Since(start).Seconds()) }()

	buf := i.flushBuffers[j]
	defer buf.Reset()
	if _, err := w.WriteTo(buf); err != nil {
		i.metrics.flushFailuresTotal.Inc()
		return err
	}

	stats := wal.GetSegmentStats(w, time.Now())
	wal.ReportSegmentStats(stats, i.metrics.segmentMetrics)

	id := ulid.MustNew(ulid.Timestamp(time.Now()), rand.Reader).String()
	if err := i.store.PutObject(ctx, wal.Dir+id, buf); err != nil {
		i.metrics.flushFailuresTotal.Inc()
		return fmt.Errorf("failed to put object: %w", err)
	}

	if _, err := i.metastoreClient.AddBlock(ctx, &metastorepb.AddBlockRequest{
		Block: w.Meta(id),
	}); err != nil {
		i.metrics.flushFailuresTotal.Inc()
		return fmt.Errorf("failed to update metastore: %w", err)
	}

	return nil
}
