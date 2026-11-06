package scheduler

import (
	"context"
	"io"
	"os"
	"testing"
	"time"

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
		Service: services.NewBasicService(func(_ context.Context) error {
			return nil
		}, func(serviceContext context.Context) error {
			<-serviceContext.Done()
			return serviceContext.Err()
		}, func(_ error) error {
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

	// Now we expect the scheduler to have sent a ERROR message to the frontend
	// so the frontend will retry connecting now that the scheduler and is not waiting for an INIT response
	assert.Equal(t, schedulerpb.ERROR, mock.msg.Status)

}

// An idle frontend blocks in Recv() inside FrontendLoop(). When the scheduler stops it has to tell the frontend, otherwise
// the connection stays open and the gRPC server cannot stop gracefully.
func TestSchedulerStoppingDisconnectsIdleFrontend(t *testing.T) {
	var s *Scheduler
	s = &Scheduler{
		log: util_log.Logger,
		schedulerRunning: promauto.With(nil).NewGauge(prometheus.GaugeOpts{
			Name: "cortex_query_scheduler_running",
			Help: "Value will be 1 if the scheduler is in the ReplicationSet and actively receiving/processing requests",
		}),
		connectedFrontends: map[string]*connectedFrontend{},
	}
	var err error
	s.subservices, err = services.NewManager(services.NewIdleService(nil, nil))
	require.NoError(t, err)
	s.Service = services.NewBasicService(func(_ context.Context) error {
		return nil
	}, func(serviceContext context.Context) error {
		<-serviceContext.Done()
		return nil
	}, s.stopping)
	s.shouldRun.Store(true)
	require.NoError(t, s.StartAsync(t.Context()))
	require.NoError(t, s.AwaitRunning(t.Context()))

	frontend := newBlockingFrontendLoopServer("127.0.0.1:9095")
	loopDone := make(chan error, 1)
	go func() { loopDone <- s.FrontendLoop(frontend) }()

	// Wait for the INIT response, after which the loop is idle in Recv().
	select {
	case msg := <-frontend.sent:
		require.Equal(t, schedulerpb.OK, msg.Status)
	case <-time.After(5 * time.Second):
		t.Fatal("scheduler did not answer INIT")
	}

	s.StopAsync()

	select {
	case msg := <-frontend.sent:
		require.Equal(t, schedulerpb.SHUTTING_DOWN, msg.Status)
	case <-time.After(5 * time.Second):
		t.Fatal("scheduler stopped without telling the idle frontend that it is shutting down")
	}
	select {
	case err := <-loopDone:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("FrontendLoop did not return after the scheduler stopped")
	}
	require.NoError(t, s.AwaitTerminated(t.Context()))
}

// blockingFrontendLoopServer plays a frontend that sent INIT and then stays idle: Recv() blocks until the scheduler
// sends SHUTTING_DOWN, which makes a real frontend close its side of the stream.
type blockingFrontendLoopServer struct {
	mockSchedulerForFrontendFrontendLoopServer
	sent chan *schedulerpb.SchedulerToFrontend
	init chan struct{}
	done chan struct{}
}

func newBlockingFrontendLoopServer(addr string) *blockingFrontendLoopServer {
	b := &blockingFrontendLoopServer{
		sent: make(chan *schedulerpb.SchedulerToFrontend, 10),
		init: make(chan struct{}, 1),
		done: make(chan struct{}),
	}
	b.init <- struct{}{}
	b.recvFn = func() (*schedulerpb.FrontendToScheduler, error) {
		select {
		case <-b.init:
			return &schedulerpb.FrontendToScheduler{Type: schedulerpb.INIT, FrontendAddress: addr}, nil
		case <-b.done:
			return nil, io.EOF
		}
	}
	return b
}

func (b *blockingFrontendLoopServer) Send(msg *schedulerpb.SchedulerToFrontend) error {
	b.sent <- msg
	if msg.Status == schedulerpb.SHUTTING_DOWN {
		select {
		case <-b.done:
		default:
			close(b.done)
		}
	}
	return nil
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
