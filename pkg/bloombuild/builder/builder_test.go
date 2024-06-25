package builder

import (
	"context"
	"fmt"
	"net"
	"os"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"
	"google.golang.org/grpc"

	"github.com/grafana/loki/v3/pkg/bloombuild/protos"
	"github.com/grafana/loki/v3/pkg/storage"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/local"
	"github.com/grafana/loki/v3/pkg/storage/config"
	bloomshipperconfig "github.com/grafana/loki/v3/pkg/storage/stores/shipper/bloomshipper/config"
	"github.com/grafana/loki/v3/pkg/storage/types"
)

func Test_BuilderLoop(t *testing.T) {
	//logger := log.NewNopLogger()
	logger := log.NewLogfmtLogger(os.Stdout)

	schemaCfg := config.SchemaConfig{
		Configs: []config.PeriodConfig{
			{
				From: parseDayTime("2023-09-01"),
				IndexTables: config.IndexPeriodicTableConfig{
					PeriodicTableConfig: config.PeriodicTableConfig{
						Prefix: "index_",
						Period: 24 * time.Hour,
					},
				},
				IndexType:  types.TSDBType,
				ObjectType: types.StorageTypeFileSystem,
				Schema:     "v13",
				RowShards:  16,
			},
		},
	}
	storageCfg := storage.Config{
		BloomShipperConfig: bloomshipperconfig.Config{
			WorkingDirectory:    []string{t.TempDir()},
			DownloadParallelism: 1,
			BlocksCache: bloomshipperconfig.BlocksCacheConfig{
				SoftLimit: flagext.Bytes(10 << 20),
				HardLimit: flagext.Bytes(20 << 20),
				TTL:       time.Hour,
			},
		},
		FSConfig: local.FSConfig{
			Directory: t.TempDir(),
		},
	}

	tasks := make([]*protos.ProtoTask, 256)
	for i := range tasks {
		tasks[i] = &protos.ProtoTask{
			Id: fmt.Sprintf("task-%d", i),
		}
	}

	server, err := newFakePlannerServer(tasks)
	require.NoError(t, err)

	// Start the server so the builder can connect and receive tasks.
	server.Start()

	limits := fakeLimits{}
	cfg := Config{
		PlannerAddress: server.Addr(),
	}
	flagext.DefaultValues(&cfg.GrpcConfig)

	builder, err := New(cfg, limits, schemaCfg, storageCfg, storage.NewClientMetrics(), nil, nil, logger, prometheus.DefaultRegisterer)
	require.NoError(t, err)
	t.Cleanup(func() {
		err = services.StopAndAwaitTerminated(context.Background(), builder)
		require.NoError(t, err)

		server.Stop()
	})

	err = services.StartAndAwaitRunning(context.Background(), builder)
	require.NoError(t, err)

	// Wait for at least one task to be processed.
	require.Eventually(t, func() bool {
		return server.CompletedTasks() > 0
	}, 5*time.Second, 100*time.Millisecond)

	// Right after stop it so connection is broken, and builder will retry.
	server.Stop()

	// While the server is stopped, the builder should keep retrying to connect but no tasks should be processed.
	// Note this is just a way to sleep while making sure no tasks are processed.
	tasksProcessedSoFar := server.CompletedTasks()
	require.Never(t, func() bool {
		return server.CompletedTasks() > tasksProcessedSoFar
	}, 5*time.Second, 500*time.Millisecond)

	// Now we start the server so the builder can connect and receive tasks.
	server.Start()

	require.Eventuallyf(t, func() bool {
		return server.CompletedTasks() == len(tasks)
	}, 30*time.Second, 100*time.Millisecond, "expected all tasks to be processed, got %d. Expected %d.", server.CompletedTasks(), len(tasks))

	err = services.StopAndAwaitTerminated(context.Background(), builder)
	require.NoError(t, err)

	require.True(t, server.shutdownCalled)
}

type fakePlannerServer struct {
	tasks          []*protos.ProtoTask
	completedTasks atomic.Int64
	shutdownCalled bool

	lisAddr    string
	grpcServer *grpc.Server
}

func newFakePlannerServer(tasks []*protos.ProtoTask) (*fakePlannerServer, error) {
	server := &fakePlannerServer{
		tasks: tasks,
	}

	return server, nil
}

func (f *fakePlannerServer) Addr() string {
	if f.lisAddr == "" {
		panic("server not started")
	}
	return f.lisAddr
}

func (f *fakePlannerServer) Stop() {
	if f.grpcServer != nil {
		f.grpcServer.Stop()
	}
}

func (f *fakePlannerServer) Start() {
	f.Stop()

	lisAddr := "localhost:0"
	if f.lisAddr != "" {
		// Reuse the same address if the server was stopped and started again.
		lisAddr = f.lisAddr
	}

	lis, err := net.Listen("tcp", lisAddr)
	if err != nil {
		panic(err)
	}
	f.lisAddr = lis.Addr().String()

	f.grpcServer = grpc.NewServer()
	protos.RegisterPlannerForBuilderServer(f.grpcServer, f)
	go func() {
		if err := f.grpcServer.Serve(lis); err != nil {
			panic(err)
		}
	}()
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
		f.completedTasks.Add(1)
		time.Sleep(10 * time.Millisecond) // Simulate task processing time to add some latency.
	}

	// No more tasks. Wait until shutdown.
	<-srv.Context().Done()
	return nil
}

func (f *fakePlannerServer) CompletedTasks() int {
	return int(f.completedTasks.Load())
}

func (f *fakePlannerServer) NotifyBuilderShutdown(_ context.Context, _ *protos.NotifyBuilderShutdownRequest) (*protos.NotifyBuilderShutdownResponse, error) {
	f.shutdownCalled = true
	return &protos.NotifyBuilderShutdownResponse{}, nil
}

type fakeLimits struct {
}

func (f fakeLimits) BloomBlockEncoding(_ string) string {
	panic("implement me")
}

func (f fakeLimits) BloomNGramLength(_ string) int {
	panic("implement me")
}

func (f fakeLimits) BloomNGramSkip(_ string) int {
	panic("implement me")
}

func (f fakeLimits) BloomCompactorMaxBlockSize(_ string) int {
	panic("implement me")
}

func (f fakeLimits) BloomCompactorMaxBloomSize(_ string) int {
	panic("implement me")
}

func parseDayTime(s string) config.DayTime {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return config.DayTime{
		Time: model.TimeFromUnix(t.Unix()),
	}
}
