package consumer

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/backoff"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
)

// A committer allows mocking of certain [kgo.Client] methods in tests.
type committer interface {
	Commit(ctx context.Context, partition int32, offset int64) error
}

// A producer allows mocking of certain [kgo.Client] methods in tests.
type producer interface {
	ProduceSync(ctx context.Context, records ...*kgo.Record) kgo.ProduceResults
}

// An uploader allows mocking of [uploader.Uploader] in tests.
type uploader interface {
	Upload(ctx context.Context, obj *dataobj.Object) (string, error)
}

// A flushJob contains all information needed to flush a data object builder.
type flushJob struct {
	builder
	// startTime is the time of the oldest log line.
	startTime time.Time
	// offset contains the offset to commit on success. Typically, it is the
	// offset of the last record appended to the data object builder.
	offset int64
	// done is called when the job has finished. If err is non-nil then the
	// job failed.
	done func(error)
}

// A flusherImpl is responsible for flushing of data object builders to data
// objects.
type flusherImpl struct {
	*services.BasicService
	jobs                    chan flushJob
	uploader                uploader
	committer               committer
	partition               int32
	metastoreEvents         producer
	metastorePartitionRatio int32
	logger                  log.Logger

	// Metrics.
	commits        prometheus.Counter
	commitFailures prometheus.Counter

	// Used in tests.
	jobFunc func(context.Context, flushJob) error
}

func newFlusher(
	uploader uploader,
	committer committer,
	metastoreEvents producer,
	partition, partitionRatio int32,
	logger log.Logger,
	r prometheus.Registerer,
) *flusherImpl {
	f := &flusherImpl{
		jobs:                    make(chan flushJob),
		uploader:                uploader,
		committer:               committer,
		metastoreEvents:         metastoreEvents,
		partition:               partition,
		metastorePartitionRatio: partitionRatio,
		logger:                  logger,
		commits: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "loki_dataobj_consumer_commits_total",
			Help: "Total number of commits",
		}),
		commitFailures: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "loki_dataobj_consumer_commit_failures_total",
			Help: "Total number of commit failures",
		}),
	}
	f.jobFunc = f.doJob
	f.BasicService = services.NewBasicService(f.starting, f.running, f.stopping)
	return f
}

// starting implements [services.StartingFn].
func (f *flusherImpl) starting(_ context.Context) error {
	return nil
}

// running implements [services.RunningFn].
func (f *flusherImpl) running(ctx context.Context) error {
	return f.Run(ctx)
}

// running implements [services.StoppingFn].
func (f *flusherImpl) stopping(_ error) error {
	return nil
}

// FlushAsync schedules the builder to be flushed.
func (f *flusherImpl) FlushAsync(ctx context.Context, builder builder, startTime time.Time, offset int64, done func(error)) {
	job := flushJob{
		builder:   builder,
		startTime: startTime,
		offset:    offset,
		done:      done,
	}
	select {
	case <-ctx.Done():
		done(ctx.Err())
	case f.jobs <- job:
		return
	}
}

func (f *flusherImpl) Run(ctx context.Context) error {
	defer level.Info(f.logger).Log("msg", "stopped flusher")
	level.Info(f.logger).Log("msg", "starting flusher")
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case job := <-f.jobs:
			if err := f.jobFunc(ctx, job); err != nil {
				level.Error(f.logger).Log("msg", "failed to flush", "err", err)
				job.done(err)
			} else {
				job.done(nil)
			}
		}
	}
}

// doJob contains the default jobFunc implementation. It can be overidden in
// tests by replacing [jobFunc].
func (f *flusherImpl) doJob(ctx context.Context, job flushJob) error {
	if err := f.flush(ctx, job); err != nil {
		return fmt.Errorf("failed to flush: %w", err)
	}
	if err := f.commit(ctx, job.offset); err != nil {
		return fmt.Errorf("failed to commit offset: %w", err)
	}
	return nil
}

// flush builds a complete data object from the builder, uploads it, records
// it in the metastore, and emits an object written event to the events topic.
func (f *flusherImpl) flush(ctx context.Context, job flushJob) error {
	obj, closer, err := job.builder.Flush()
	if err != nil {
		level.Error(f.logger).Log("msg", "failed to flush builder", "err", err)
		return err
	}
	obj, closer, err = f.sort(ctx, job.builder, obj, closer)
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
	if err := f.produceMetastoreEvent(ctx, job.startTime, objectPath); err != nil {
		level.Error(f.logger).Log("msg", "failed to emit metastore event", "err", err)
		return err
	}
	return nil
}

func (f *flusherImpl) sort(ctx context.Context, builder builder, obj *dataobj.Object, closer io.Closer) (*dataobj.Object, io.Closer, error) {
	defer closer.Close()
	return builder.CopyAndSort(ctx, obj)
}

func (f *flusherImpl) commit(ctx context.Context, offset int64) error {
	backoff := backoff.New(ctx, backoff.Config{
		MinBackoff: 100 * time.Millisecond,
		MaxBackoff: 10 * time.Second,
		MaxRetries: 20,
	})
	var lastErr error
	backoff.Reset()
	for backoff.Ongoing() {
		f.commits.Inc()
		err := f.committer.Commit(ctx, f.partition, offset)
		if err == nil {
			level.Debug(f.logger).Log("msg", "committed offset", "partition", f.partition, "offset", offset)
			return nil
		}
		level.Error(f.logger).Log("msg", "failed to commit records", "err", err)
		f.commitFailures.Inc()
		lastErr = err
		backoff.Wait()
	}
	return lastErr
}

func (f *flusherImpl) produceMetastoreEvent(ctx context.Context, startTime time.Time, objectPath string) error {
	event := &metastore.ObjectWrittenEvent{
		ObjectPath:         objectPath,
		WriteTime:          time.Now().Format(time.RFC3339),
		EarliestRecordTime: startTime.Format(time.RFC3339),
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
