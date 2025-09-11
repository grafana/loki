package jobqueue

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/grafana/dskit/flagext"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"
	"google.golang.org/grpc"

	compactor_grpc "github.com/grafana/loki/v3/pkg/compactor/client/grpc"
)

type mockCompactorClient struct {
	conn *grpc.ClientConn
}

func (m mockCompactorClient) JobQueueClient() compactor_grpc.JobQueueClient {
	return compactor_grpc.NewJobQueueClient(m.conn)
}

type mockJobRunner struct {
	mock.Mock
}

func (m *mockJobRunner) Run(ctx context.Context, job *compactor_grpc.Job) ([]byte, error) {
	args := m.Called(ctx, job)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]byte), args.Error(1)
}

func TestWorkerManager(t *testing.T) {
	// create a new job queue
	q := NewQueue(nil)
	conn, closer := setupGRPC(t, q)
	defer closer()

	// create a mock job builder which would build only a single job
	mockJobBuilder := &mockBuilder{
		jobsToBuild: []*compactor_grpc.Job{
			{
				Id:   "1",
				Type: compactor_grpc.JOB_TYPE_DELETION,
			},
		},
	}

	// register the job builder with the queue and start the queue
	require.NoError(t, q.RegisterBuilder(compactor_grpc.JOB_TYPE_DELETION, mockJobBuilder, jobTimeout, jobRetries))
	go q.Start(context.Background())
	require.Equal(t, int32(0), mockJobBuilder.jobsSentCount.Load())

	jobRunner := &mockJobRunner{}
	jobRunner.On("Run", mock.Anything, mock.Anything).Return(nil, nil)

	workerCfg := WorkerConfig{}
	flagext.DefaultValues(&workerCfg)
	// run two workers so only one of them would get a job
	workerCfg.NumWorkers = 2

	// create a new worker manager and register the mock job runner
	wm := NewWorkerManager(workerCfg, mockCompactorClient{conn}, nil)
	require.NoError(t, wm.RegisterJobRunner(compactor_grpc.JOB_TYPE_DELETION, jobRunner))

	// trying to register job runner for same job type should throw an error
	require.Error(t, wm.RegisterJobRunner(compactor_grpc.JOB_TYPE_DELETION, &mockJobRunner{}))

	// start two workers so only one of them would get a job
	ctx, cancel := context.WithCancel(context.Background())
	wg := &sync.WaitGroup{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		require.NoError(t, wm.Start(ctx))
	}()

	// verify that the job builder got to send the job and that it got processed successfully
	require.Eventually(t, func() bool {
		if mockJobBuilder.jobsSentCount.Load() != 1 {
			return false
		}
		if mockJobBuilder.jobsSucceeded.Load() != 1 {
			return false
		}
		return true
	}, time.Second, time.Millisecond*100)

	// stop the worker manager
	cancel()
	wg.Wait()
}

func TestWorker_ProcessJob(t *testing.T) {
	// create a new job queue
	q := newQueue(50*time.Millisecond, nil)
	conn, closer := setupGRPC(t, q)
	defer closer()

	// create a mock job builder which would build a couple of jobs
	mockJobBuilder := &mockBuilder{
		jobsToBuild: []*compactor_grpc.Job{
			{
				Id:   "1",
				Type: compactor_grpc.JOB_TYPE_DELETION,
			},
			{
				Id:   "2",
				Type: compactor_grpc.JOB_TYPE_DELETION + 1, // an unknown job should not break anything in processing further valid jobs
			},
			{
				Id:   "3",
				Type: compactor_grpc.JOB_TYPE_DELETION,
			},
		},
	}

	jobRunner := &mockJobRunner{}
	jobRunner.On("Run", mock.Anything, mock.Anything).Return(nil, nil).Once()
	jobRunner.On("Run", mock.Anything, mock.Anything).Return(nil, fmt.Errorf("fail")).Times(jobRetries + 1)

	// register the job builder with the queue and start the queue
	require.NoError(t, q.RegisterBuilder(compactor_grpc.JOB_TYPE_DELETION, mockJobBuilder, jobTimeout, jobRetries))
	go q.Start(context.Background())
	require.Equal(t, int32(0), mockJobBuilder.jobsSentCount.Load())

	// build a worker and start it
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go newWorker(mockCompactorClient{conn: conn}, map[compactor_grpc.JobType]JobRunner{
		compactor_grpc.JOB_TYPE_DELETION: jobRunner,
	}, newWorkerMetrics(nil, func() bool {
		return true
	})).Start(ctx)

	// verify that the job builder got to send all 3 jobs and that both the valid jobs got processed
	require.Eventually(t, func() bool {
		if mockJobBuilder.jobsSentCount.Load() != 3 {
			return false
		}
		if mockJobBuilder.jobsSucceeded.Load() != 1 {
			return false
		}
		if mockJobBuilder.jobsFailed.Load() != 1 {
			return false
		}
		return true
	}, 2*time.Second, time.Millisecond*50)

	jobRunner.AssertExpectations(t)
}

func TestWorker_StreamClosure(t *testing.T) {
	// build a queue
	q := NewQueue(nil)
	conn, closer := setupGRPC(t, q)
	defer closer()

	// register a builder and start the queue
	require.NoError(t, q.RegisterBuilder(compactor_grpc.JOB_TYPE_DELETION, &mockBuilder{}, jobTimeout, jobRetries))
	ctx, cancelQueueCtx := context.WithCancel(context.Background())
	go q.Start(ctx)

	// build a worker
	worker := newWorker(mockCompactorClient{conn: conn}, map[compactor_grpc.JobType]JobRunner{
		compactor_grpc.JOB_TYPE_DELETION: &mockJobRunner{},
	}, newWorkerMetrics(nil, func() bool {
		return true
	}))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var running atomic.Bool
	// start the worker and ensure that it is running
	go func() {
		running.Store(true)
		defer running.Store(false)

		worker.Start(ctx)
	}()

	require.Eventually(t, func() bool {
		return running.Load()
	}, time.Second, time.Millisecond*100)

	// stop the queue by cancelling its context so that it closes the stream
	cancelQueueCtx()

	// sleep for a while and ensure that the worker is still running
	time.Sleep(100 * time.Millisecond)
	require.True(t, running.Load())
}
