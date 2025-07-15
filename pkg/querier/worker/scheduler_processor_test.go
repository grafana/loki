// SPDX-License-Identifier: AGPL-3.0-only

package worker

import (
	"context"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/gogo/status"
	"github.com/grafana/dskit/concurrency"
	"github.com/grafana/dskit/httpgrpc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"

	"github.com/grafana/loki/v3/pkg/querier/queryrange"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/v3/pkg/scheduler/schedulerpb"
)

func TestSchedulerProcessor_processQueriesOnSingleStream(t *testing.T) {
	t.Run("should immediately return if worker context is canceled and there's no inflight query", func(t *testing.T) {
		sp, loopClient, requestHandler := prepareSchedulerProcessor()

		workerCtx, workerCancel := context.WithCancel(context.Background())

		loopClient.On("Recv").Return(func() (*schedulerpb.SchedulerToQuerier, error) {
			// Simulate the querier received a SIGTERM while waiting for a query to execute.
			workerCancel()

			// No query to execute, so wait until terminated.
			<-loopClient.Context().Done()
			return nil, loopClient.Context().Err()
		})

		requestHandler.On("Do", mock.Anything, mock.Anything).Return(&queryrange.LokiResponse{}, nil)

		sp.processQueriesOnSingleStream(workerCtx, nil, "127.0.0.1", "1")

		// We expect at this point, the execution context has been canceled too.
		require.Error(t, loopClient.Context().Err())

		// We expect Send() has been called only once, to send the querier ID to scheduler.
		loopClient.AssertNumberOfCalls(t, "Send", 1)
		loopClient.AssertCalled(t, "Send", &schedulerpb.QuerierToScheduler{QuerierID: "test-querier-id"})
	})

	t.Run("should wait until inflight query execution is completed before returning when worker context is canceled", func(t *testing.T) {
		sp, loopClient, requestHandler := prepareSchedulerProcessor()

		recvCount := atomic.NewInt64(0)

		loopClient.On("Recv").Return(func() (*schedulerpb.SchedulerToQuerier, error) {
			switch recvCount.Inc() {
			case 1:
				return &schedulerpb.SchedulerToQuerier{
					QueryID: 1,
					Request: &schedulerpb.SchedulerToQuerier_HttpRequest{
						HttpRequest: &httpgrpc.HTTPRequest{
							Method: "GET",
							Url:    `/loki/api/v1/query_range?query={foo="bar"}&step=10&limit=200&direction=FORWARD`,
						},
					},
					FrontendAddress: "127.0.0.2",
					UserID:          "user-1",
				}, nil
			default:
				// No more messages to process, so waiting until terminated.
				<-loopClient.Context().Done()
				return nil, loopClient.Context().Err()
			}
		})

		workerCtx, workerCancel := context.WithCancel(context.Background())

		requestHandler.On("Do", mock.Anything, mock.Anything).Run(func(_ mock.Arguments) {
			// Cancel the worker context while the query execution is in progress.
			workerCancel()

			// Ensure the execution context hasn't been canceled yet.
			require.Nil(t, loopClient.Context().Err())

			// Intentionally slow down the query execution, to double check the worker waits until done.
			time.Sleep(time.Second)
		}).Return(&queryrange.LokiResponse{}, nil)

		startTime := time.Now()
		sp.processQueriesOnSingleStream(workerCtx, nil, "127.0.0.1", "1")
		assert.GreaterOrEqual(t, time.Since(startTime), time.Second)

		// We expect at this point, the execution context has been canceled too.
		require.Error(t, loopClient.Context().Err())

		// We expect Send() to be called twice: first to send the querier ID to scheduler
		// and then to send the query result.
		loopClient.AssertNumberOfCalls(t, "Send", 2)
		loopClient.AssertCalled(t, "Send", &schedulerpb.QuerierToScheduler{QuerierID: "test-querier-id"})
	})

	t.Run("should not log an error when the query-scheduler is terminates while waiting for the next query to run", func(t *testing.T) {
		sp, loopClient, requestHandler := prepareSchedulerProcessor()

		// Override the logger to capture the logs.
		logs := &concurrency.SyncBuffer{}
		sp.log = log.NewLogfmtLogger(logs)

		workerCtx, workerCancel := context.WithCancel(context.Background())

		// As soon as the Recv() is called for the first time, we cancel the worker context and
		// return the "scheduler not running" error. The reason why we cancel the worker context
		// is to let processQueriesOnSingleStream() terminate.
		loopClient.On("Recv").Return(func() (*schedulerpb.SchedulerToQuerier, error) {
			workerCancel()
			return nil, status.Error(codes.Unknown, schedulerpb.ErrSchedulerIsNotRunning.Error())
		})

		requestHandler.On("Do", mock.Anything, mock.Anything).Return(&queryrange.LokiResponse{}, nil)

		sp.processQueriesOnSingleStream(workerCtx, nil, "127.0.0.1", "1")

		// We expect no error in the log.
		assert.NotContains(t, logs.String(), "error")
		assert.NotContains(t, logs.String(), schedulerpb.ErrSchedulerIsNotRunning)
	})
}

func prepareSchedulerProcessor() (*schedulerProcessor, *querierLoopClientMock, *requestHandlerMock) {
	var querierLoopCtx context.Context

	loopClient := &querierLoopClientMock{}
	loopClient.On("Send", mock.Anything).Return(nil)
	loopClient.On("Context").Return(func() context.Context {
		return querierLoopCtx
	})

	schedulerClient := &schedulerForQuerierClientMock{}
	schedulerClient.On("QuerierLoop", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
		querierLoopCtx = args.Get(0).(context.Context)
	}).Return(loopClient, nil)

	requestHandler := &requestHandlerMock{}
	metrics := NewMetrics(Config{}, nil)
	sp, _ := newSchedulerProcessor(Config{QuerierID: "test-querier-id"}, requestHandler, log.NewNopLogger(), metrics, queryrange.DefaultCodec)
	sp.schedulerClientFactory = func(_ *grpc.ClientConn) schedulerpb.SchedulerForQuerierClient {
		return schedulerClient
	}

	return sp, loopClient, requestHandler
}

type schedulerForQuerierClientMock struct {
	mock.Mock
}

func (m *schedulerForQuerierClientMock) QuerierLoop(ctx context.Context, opts ...grpc.CallOption) (schedulerpb.SchedulerForQuerier_QuerierLoopClient, error) {
	args := m.Called(ctx, opts)
	return args.Get(0).(schedulerpb.SchedulerForQuerier_QuerierLoopClient), args.Error(1)
}

func (m *schedulerForQuerierClientMock) NotifyQuerierShutdown(ctx context.Context, in *schedulerpb.NotifyQuerierShutdownRequest, opts ...grpc.CallOption) (*schedulerpb.NotifyQuerierShutdownResponse, error) {
	args := m.Called(ctx, in, opts)
	return args.Get(0).(*schedulerpb.NotifyQuerierShutdownResponse), args.Error(1)
}

type querierLoopClientMock struct {
	mock.Mock
}

func (m *querierLoopClientMock) Send(msg *schedulerpb.QuerierToScheduler) error {
	args := m.Called(msg)
	return args.Error(0)
}

func (m *querierLoopClientMock) Recv() (*schedulerpb.SchedulerToQuerier, error) {
	args := m.Called()

	// Allow to mock the Recv() with a function which is called each time.
	if fn, ok := args.Get(0).(func() (*schedulerpb.SchedulerToQuerier, error)); ok {
		return fn()
	}

	return args.Get(0).(*schedulerpb.SchedulerToQuerier), args.Error(1)
}

func (m *querierLoopClientMock) Header() (metadata.MD, error) {
	args := m.Called()
	return args.Get(0).(metadata.MD), args.Error(1)
}

func (m *querierLoopClientMock) Trailer() metadata.MD {
	args := m.Called()
	return args.Get(0).(metadata.MD)
}

func (m *querierLoopClientMock) CloseSend() error {
	args := m.Called()
	return args.Error(0)
}

func (m *querierLoopClientMock) Context() context.Context {
	args := m.Called()

	// Allow to mock the Context() with a function which is called each time.
	if fn, ok := args.Get(0).(func() context.Context); ok {
		return fn()
	}

	return args.Get(0).(context.Context)
}

func (m *querierLoopClientMock) SendMsg(msg interface{}) error {
	args := m.Called(msg)
	return args.Error(0)
}

func (m *querierLoopClientMock) RecvMsg(msg interface{}) error {
	args := m.Called(msg)
	return args.Error(0)
}

type requestHandlerMock struct {
	mock.Mock
}

func (m *requestHandlerMock) Do(ctx context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
	args := m.Called(ctx, req)
	return args.Get(0).(queryrangebase.Response), args.Error(1)
}
