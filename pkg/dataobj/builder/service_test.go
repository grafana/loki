package builder

import (
	"context"
	"errors"
	"flag"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	dataobj_uploader "github.com/grafana/loki/v3/pkg/dataobj/uploader"
	"github.com/grafana/loki/v3/pkg/kafka"
)

func TestNew(t *testing.T) {
	t.Run("returns an error when the flush concurrency is 0", func(t *testing.T) {
		var cfg Config
		cfg.RegisterFlags(flag.NewFlagSet("test", flag.PanicOnError))
		cfg.Topic = "test"
		cfg.FlushConcurrency = 0

		_, err := New(kafka.Config{}, cfg, dataobj_uploader.Config{}, metastore.Config{}, nil, nil, prometheus.NewRegistry(), log.NewNopLogger(), nil)
		require.ErrorContains(t, err, "flush concurrency must be at least 1")
	})
}

func TestService(t *testing.T) {
	t.Run("fails with the processor error when the processor fails", func(t *testing.T) {
		consumer, processor := newFakeSubservice("consumer"), newFakeSubservice("processor")
		s := newService(consumer, fakeResumeOffsetReader{}, processor, nil, nil, 0, log.NewNopLogger())
		ctx := testContext(t)
		require.NoError(t, services.StartAndAwaitRunning(ctx, s))

		errProcessor := errors.New("processor failed")
		processor.fail(errProcessor)

		require.Error(t, s.AwaitTerminated(ctx))
		require.Equal(t, services.Failed, s.State())
		require.ErrorIs(t, s.FailureCase(), errProcessor)
		require.Equal(t, services.Terminated, consumer.State())
	})

	t.Run("fails with the consumer error when the consumer fails", func(t *testing.T) {
		consumer, processor := newFakeSubservice("consumer"), newFakeSubservice("processor")
		s := newService(consumer, fakeResumeOffsetReader{}, processor, nil, nil, 0, log.NewNopLogger())
		ctx := testContext(t)
		require.NoError(t, services.StartAndAwaitRunning(ctx, s))

		errConsumer := errors.New("consumer failed")
		consumer.fail(errConsumer)

		require.Error(t, s.AwaitTerminated(ctx))
		require.Equal(t, services.Failed, s.State())
		require.ErrorIs(t, s.FailureCase(), errConsumer)
		require.Equal(t, services.Terminated, processor.State())
	})

	t.Run("terminates without error when stopped", func(t *testing.T) {
		consumer, processor := newFakeSubservice("consumer"), newFakeSubservice("processor")
		s := newService(consumer, fakeResumeOffsetReader{}, processor, nil, nil, 0, log.NewNopLogger())
		ctx := testContext(t)
		require.NoError(t, services.StartAndAwaitRunning(ctx, s))

		require.NoError(t, services.StopAndAwaitTerminated(ctx, s))
		require.Equal(t, services.Terminated, s.State())
		require.NoError(t, s.FailureCase())
		require.Equal(t, services.Terminated, consumer.State())
		require.Equal(t, services.Terminated, processor.State())
	})

	t.Run("runs the reporter until stopped", func(t *testing.T) {
		consumer, processor, reporter := newFakeSubservice("consumer"), newFakeSubservice("processor"), newFakeSubservice("reporter")
		s := newService(consumer, fakeResumeOffsetReader{}, processor, reporter, nil, 0, log.NewNopLogger())
		ctx := testContext(t)
		require.NoError(t, services.StartAndAwaitRunning(ctx, s))
		require.Equal(t, services.Running, reporter.State())

		require.NoError(t, services.StopAndAwaitTerminated(ctx, s))
		require.Equal(t, services.Terminated, reporter.State())
	})

	t.Run("fails with the reporter error when the reporter fails", func(t *testing.T) {
		consumer, processor, reporter := newFakeSubservice("consumer"), newFakeSubservice("processor"), newFakeSubservice("reporter")
		s := newService(consumer, fakeResumeOffsetReader{}, processor, reporter, nil, 0, log.NewNopLogger())
		ctx := testContext(t)
		require.NoError(t, services.StartAndAwaitRunning(ctx, s))

		errReporter := errors.New("reporter failed")
		reporter.fail(errReporter)

		require.Error(t, s.AwaitTerminated(ctx))
		require.ErrorIs(t, s.FailureCase(), errReporter)
		require.Equal(t, services.Terminated, consumer.State())
		require.Equal(t, services.Terminated, processor.State())
	})
}

func testContext(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// fakeSubservice runs until it is stopped or until fail is called.
type fakeSubservice struct {
	*services.BasicService
	failures chan error
}

func newFakeSubservice(name string) *fakeSubservice {
	f := &fakeSubservice{failures: make(chan error, 1)}
	f.BasicService = services.NewBasicService(nil, f.running, nil).WithName(name)
	return f
}

func (f *fakeSubservice) fail(err error) {
	f.failures <- err
}

func (f *fakeSubservice) running(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return nil
	case err := <-f.failures:
		return err
	}
}

func (f *fakeSubservice) SetInitialOffset(int64) error {
	return nil
}

type fakeResumeOffsetReader struct{}

func (fakeResumeOffsetReader) ResumeOffset(context.Context, int32) (int64, error) {
	return 0, nil
}
