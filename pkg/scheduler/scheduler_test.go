package scheduler

import (
	"context"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/metadata"

	"github.com/grafana/loki/pkg/scheduler/schedulerpb"
	util_log "github.com/grafana/loki/pkg/util/log"
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
		schedulerRunning: promauto.With(prometheus.DefaultRegisterer).NewGauge(prometheus.GaugeOpts{
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

type mockSchedulerForFrontendFrontendLoopServer struct {
	msg *schedulerpb.SchedulerToFrontend
}

func (m *mockSchedulerForFrontendFrontendLoopServer) Send(frontend *schedulerpb.SchedulerToFrontend) error {
	m.msg = frontend
	return nil
}

func (m mockSchedulerForFrontendFrontendLoopServer) Recv() (*schedulerpb.FrontendToScheduler, error) {
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
