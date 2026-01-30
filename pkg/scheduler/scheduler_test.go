package scheduler

import (
	"context"
	"os"
	"testing"

	"github.com/grafana/dskit/httpgrpc"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/metadata"

	"github.com/grafana/loki/v3/pkg/scheduler/schedulerpb"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

func TestScheduler_setRunState(t *testing.T) {

	// This test is a bit crude, the method is not the most directly testable but
	// this covers us to make sure we don't accidentally change the behavior of
	// the little bit of logic which runs/stops the scheduler and makes sure we
	// send a shutdown message to disconnect frontends.

	// To avoid a lot more complicated test setup of calling NewScheduler instead
	// we make a Scheduler with the things required to avoid nil pointers
	s := Scheduler{
		log: util_log.Logger,
		schedulerRunning: promauto.With(nil).NewGauge(prometheus.GaugeOpts{
			Name: "cortex_query_scheduler_running",
			Help: "Value will be 1 if the scheduler is in the ReplicationSet and actively receiving/processing requests",
		}),
	}
	mock := &mockSchedulerForFrontendFrontendLoopServer{}
	s.connectedFrontends = map[string]*connectedFrontend{
		"127.0.0.1:9095": {
			connections: 0,
			frontend:    mock,
			ctx:         nil,
			cancel:      nil,
		},
	}

	// not_running, shouldRun == false
	assert.False(t, s.shouldRun.Load())

	// not_running -> running, shouldRun == true
	s.setRunState(true)
	assert.True(t, s.shouldRun.Load())

	// running -> running, shouldRun == true
	s.setRunState(true)
	assert.True(t, s.shouldRun.Load())

	// running -> not_running, shouldRun == false, shutdown message sent
	s.setRunState(false)
	assert.False(t, s.shouldRun.Load())
	assert.Equal(t, schedulerpb.SHUTTING_DOWN, mock.msg.Status)
	mock.msg = nil

	// not_running -> not_running, shouldRun == false, no shutdown message sent
	s.setRunState(false)
	assert.Nil(t, mock.msg)

}
func TestFrontendConnectsToRunningSchedulerButBeforeShouldRun(t *testing.T) {

	// This test is even a bit bit cruder than the one above as we inject a noop BaseService
	// to have a way to transition into the RUNNING state.
	// This scheduler starts with no frontends connected

	s := Scheduler{
		log: util_log.Logger,
		schedulerRunning: promauto.With(nil).NewGauge(prometheus.GaugeOpts{
			Name: "cortex_query_scheduler_running",
			Help: "Value will be 1 if the scheduler is in the ReplicationSet and actively receiving/processing requests",
		}),
		Service: services.NewBasicService(func(serviceContext context.Context) error {
			return nil
		}, func(serviceContext context.Context) error {
			<-serviceContext.Done()
			return serviceContext.Err()
		}, func(failureCase error) error {
			return nil
		}),
	}
	require.NoError(t, s.StartAsync(t.Context()))
	require.NoError(t, s.AwaitRunning(t.Context()))
	require.Equal(t, services.Running, s.State())
	mock := &mockSchedulerForFrontendFrontendLoopServer{
		recvFn: func() (*schedulerpb.FrontendToScheduler, error) {
			return &schedulerpb.FrontendToScheduler{
				Type:            schedulerpb.INIT,
				FrontendAddress: "127.0.0.1:9095",
			}, nil
		},
	}
	s.connectedFrontends = map[string]*connectedFrontend{}

	// not_running, shouldRun == false
	assert.False(t, s.shouldRun.Load())

	err := s.FrontendLoop(mock)
	assert.NoError(t, err)

	// not_running -> running, shouldRun == true
	// to simulate last "setRunState(true)" happening after FrontendLoop started
	s.setRunState(true)
	assert.True(t, s.shouldRun.Load())

	// Now we expect the scheduler to have sent a ERROR message to the frontend
	// so the frontend will retry connecting now that the scheduler and is not waiting for an INIT response
	assert.Equal(t, schedulerpb.ERROR, mock.msg.Status)

}

func TestProtobufBackwardsCompatibility(t *testing.T) {
	t.Run("SchedulerToQuerier", func(t *testing.T) {
		expected := &schedulerpb.SchedulerToQuerier{
			QueryID: 42,
			UserID:  "100",
			Request: &schedulerpb.SchedulerToQuerier_HttpRequest{
				HttpRequest: &httpgrpc.HTTPRequest{
					Headers: []*httpgrpc.Header{{Key: "foo", Values: []string{"bar"}}},
					Body:    []byte("Hello echo!"),
				},
			},
			StatsEnabled: true,
		}

		b, err := os.ReadFile("testdata/scheduler_to_querier_k173.bin")
		assert.NoError(t, err)

		actual := &schedulerpb.SchedulerToQuerier{}
		err = actual.Unmarshal(b)
		assert.NoError(t, err)

		assert.IsType(t, &schedulerpb.SchedulerToQuerier_HttpRequest{}, actual.Request)
		assert.EqualValues(t, expected, actual)
	})

	t.Run("FrontendToScheduler", func(t *testing.T) {
		expected := &schedulerpb.FrontendToScheduler{
			QueryID: 42,
			UserID:  "100",
			Request: &schedulerpb.FrontendToScheduler_HttpRequest{
				HttpRequest: &httpgrpc.HTTPRequest{
					Headers: []*httpgrpc.Header{{Key: "foo", Values: []string{"bar"}}},
					Body:    []byte("Hello echo!"),
				},
			},
		}
		b, err := os.ReadFile("testdata/frontend_to_scheduler_k173.bin")
		assert.NoError(t, err)

		actual := &schedulerpb.FrontendToScheduler{}
		err = actual.Unmarshal(b)
		assert.NoError(t, err)

		assert.IsType(t, &schedulerpb.FrontendToScheduler_HttpRequest{}, actual.Request)
		assert.EqualValues(t, expected, actual)
	})
}

type mockSchedulerForFrontendFrontendLoopServer struct {
	msg    *schedulerpb.SchedulerToFrontend
	recvFn func() (*schedulerpb.FrontendToScheduler, error)
}

func (m *mockSchedulerForFrontendFrontendLoopServer) Send(frontend *schedulerpb.SchedulerToFrontend) error {
	m.msg = frontend
	return nil
}

func (m mockSchedulerForFrontendFrontendLoopServer) Recv() (*schedulerpb.FrontendToScheduler, error) {
	if m.recvFn != nil {
		return m.recvFn()
	}
	panic("implement me")
}

func (m mockSchedulerForFrontendFrontendLoopServer) SetHeader(_ metadata.MD) error {
	panic("implement me")
}

func (m mockSchedulerForFrontendFrontendLoopServer) SendHeader(_ metadata.MD) error {
	panic("implement me")
}

func (m mockSchedulerForFrontendFrontendLoopServer) SetTrailer(_ metadata.MD) {
	panic("implement me")
}

func (m mockSchedulerForFrontendFrontendLoopServer) Context() context.Context {
	panic("implement me")
}

func (m mockSchedulerForFrontendFrontendLoopServer) SendMsg(_ interface{}) error {
	panic("implement me")
}

func (m mockSchedulerForFrontendFrontendLoopServer) RecvMsg(_ interface{}) error {
	panic("implement me")
}
