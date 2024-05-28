package builder

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/grafana/loki/v3/pkg/bloombuild/protos"
)

func Test_BuilderLoop(t *testing.T) {
	logger := log.NewNopLogger()

	tasks := make([]*protos.ProtoTask, 256)
	for i := range tasks {
		tasks[i] = &protos.ProtoTask{
			Id: fmt.Sprintf("task-%d", i),
		}
	}

	server, err := newFakePlannerServer(tasks)
	require.NoError(t, err)

	cfg := Config{
		PlannerAddress: server.Addr(),
	}
	flagext.DefaultValues(&cfg.GrpcConfig)

	builder, err := New(cfg, logger, prometheus.DefaultRegisterer)
	require.NoError(t, err)
	t.Cleanup(func() {
		err = services.StopAndAwaitTerminated(context.Background(), builder)
		require.NoError(t, err)

		server.Stop()
	})

	err = services.StartAndAwaitRunning(context.Background(), builder)
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		return server.completedTasks == len(tasks)
	}, 5*time.Second, 100*time.Millisecond)

	err = services.StopAndAwaitTerminated(context.Background(), builder)
	require.NoError(t, err)

	require.True(t, server.shutdownCalled)
}

type fakePlannerServer struct {
	tasks          []*protos.ProtoTask
	completedTasks int
	shutdownCalled bool

	addr       string
	grpcServer *grpc.Server
	stop       chan struct{}
}

func newFakePlannerServer(tasks []*protos.ProtoTask) (*fakePlannerServer, error) {
	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return nil, err
	}

	server := &fakePlannerServer{
		tasks:      tasks,
		addr:       lis.Addr().String(),
		grpcServer: grpc.NewServer(),
		stop:       make(chan struct{}),
	}

	protos.RegisterPlannerForBuilderServer(server.grpcServer, server)
	go func() {
		if err := server.grpcServer.Serve(lis); err != nil {
			panic(err)
		}
	}()

	return server, nil
}

func (f *fakePlannerServer) Addr() string {
	return f.addr
}

func (f *fakePlannerServer) Stop() {
	close(f.stop)
	f.grpcServer.Stop()
}

func (f *fakePlannerServer) BuilderLoop(srv protos.PlannerForBuilder_BuilderLoopServer) error {
	// Receive Ready
	if _, err := srv.Recv(); err != nil {
		return fmt.Errorf("failed to receive ready: %w", err)
	}

	for _, task := range f.tasks {
		if err := srv.Send(&protos.PlannerToBuilder{Task: task}); err != nil {
			return fmt.Errorf("failed to send task: %w", err)
		}
		if _, err := srv.Recv(); err != nil {
			return fmt.Errorf("failed to receive task response: %w", err)
		}
		f.completedTasks++
	}

	// No more tasks. Wait until shutdown.
	<-f.stop
	return nil
}

func (f *fakePlannerServer) NotifyBuilderShutdown(_ context.Context, _ *protos.NotifyBuilderShutdownRequest) (*protos.NotifyBuilderShutdownResponse, error) {
	f.shutdownCalled = true
	return &protos.NotifyBuilderShutdownResponse{}, nil
}
