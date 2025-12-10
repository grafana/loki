package consumer

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/coder/quartz"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/backoff"
	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/dataobj/uploader"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/twmb/franz-go/pkg/kgo"
)

// committer allows mocking of certain [kgo.Client] methods in tests.
type committer interface {
	Commit(ctx context.Context, offset int64) error
}

// producer allows mocking of certain [kgo.Client] methods in tests.
type producer interface {
	ProduceSync(ctx context.Context, records ...*kgo.Record) kgo.ProduceResults
}

type job struct {
	builder
	offset       int64
	oldestRecord time.Time
	callbackDone func()
}

type flusher struct {
	queue                   chan job
	uploader                *uploader.Uploader
	committer               committer
	partition               int32
	metastoreEvents         producer
	metastorePartitionRatio int32
	logger                  log.Logger
	clock                   quartz.Clock

	// Metrics.
	commits        prometheus.Counter
	commitFailures prometheus.Counter
}

func newFlusher(uploader *uploader.Uploader, committer committer, metastoreEvents producer, partitionRatio int32, logger log.Logger, r prometheus.Registerer) *flusher {
	return &flusher{
		queue:                   make(chan job),
		uploader:                uploader,
		committer:               committer,
		metastoreEvents:         metastoreEvents,
		metastorePartitionRatio: partitionRatio,
		logger:                  logger,
		clock:                   quartz.NewReal(),
		commits: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "loki_dataobj_consumer_commits_total",
			Help: "Total number of commits",
		}),
		commitFailures: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "loki_dataobj_consumer_commit_failures_total",
			Help: "Total number of commit failures",
		}),
	}
}

func (f *flusher) UnregisterMetrics(r prometheus.Registerer) {
	r.Unregister(f.commits)
	r.Unregister(f.commitFailures)
}

func (f *flusher) Start(ctx context.Context) error {
	defer func() {
		level.Info(f.logger).Log("msg", "stopped flusher")
	}()
	level.Info(f.logger).Log("msg", "starting flusher")
	for {
		select {
		case j := <-f.queue:
			if err := f.flushAndCommit(ctx, j); err != nil {
				level.Error(f.logger).Log("msg", "failed to flush and commit", "err", err)
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (f *flusher) Enqueue(ctx context.Context, j job) {
	f.queue <- j
}

// flushAndCommit flushes the builder and commits its offset.
func (f *flusher) flushAndCommit(ctx context.Context, j job) error {
	if err := f.flush(ctx, j); err != nil {
		return fmt.Errorf("failed to flush: %w", err)
	}
	if err := f.commit(ctx, j.offset); err != nil {
		return fmt.Errorf("failed to commit offset: %w", err)
	}
	if fn := j.callbackDone; fn != nil {
		fn()
	}
	return nil
}

// flush builds a complete data object from the builder, uploads it, records
// it in the metastore, and emits an object written event to the events topic.
func (f *flusher) flush(ctx context.Context, j job) error {
	// The time range must be read before the flush as the builder is reset
	// at the end of each flush, resetting the time range.
	obj, closer, err := j.builder.Flush()
	if err != nil {
		level.Error(f.logger).Log("msg", "failed to flush builder", "err", err)
		return err
	}

	obj, closer, err = f.sort(j.builder, obj, closer)
	if err != nil {
		level.Error(f.logger).Log("msg", "failed to sort dataobj", "err", err)
		return err
	}
	defer closer.Close()

	objectPath, err := f.uploader.Upload(ctx, obj)
	if err != nil {
		level.Error(f.logger).Log("msg", "failed to upload object", "err", err)
		return err
	}

	if err := f.emitMetastoreEvent(ctx, j, objectPath); err != nil {
		level.Error(f.logger).Log("msg", "failed to emit metastore event", "err", err)
		return err
	}

	return nil
}

func (f *flusher) sort(builder builder, obj *dataobj.Object, closer io.Closer) (*dataobj.Object, io.Closer, error) {
	start := time.Now()
	defer func() {
		level.Debug(f.logger).Log(
			"msg",
			"partition processor sorted logs object-wide",
			"duration",
			time.Since(start),
		)
		closer.Close()
	}()
	return builder.CopyAndSort(obj)
}

func (f *flusher) commit(ctx context.Context, offset int64) error {
	backoff := backoff.New(ctx, backoff.Config{
		MinBackoff: 100 * time.Millisecond,
		MaxBackoff: 10 * time.Second,
		MaxRetries: 20,
	})
	var lastErr error
	backoff.Reset()
	for backoff.Ongoing() {
		f.commits.Inc()
		err := f.committer.Commit(ctx, offset)
		if err == nil {
			return nil
		}
		level.Error(f.logger).Log("msg", "failed to commit records", "err", err)
		f.commitFailures.Inc()
		lastErr = err
		backoff.Wait()
	}
	return lastErr
}

func (f *flusher) emitMetastoreEvent(ctx context.Context, j job, objectPath string) error {
	event := &metastore.ObjectWrittenEvent{
		ObjectPath:         objectPath,
		WriteTime:          f.clock.Now().Format(time.RFC3339),
		EarliestRecordTime: j.oldestRecord.Format(time.RFC3339),
	}
	b, err := event.Marshal()
	if err != nil {
		return err
	}
	// Apply the partition ratio to the incoming partition to find the metastore topic partition.
	// This has the effect of concentrating the log partitions to fewer metastore partitions for later processing.
	partition := f.partition / f.metastorePartitionRatio
	results := f.metastoreEvents.ProduceSync(ctx, &kgo.Record{
		Partition: partition,
		Value:     b,
	})
	return results.FirstErr()
}
