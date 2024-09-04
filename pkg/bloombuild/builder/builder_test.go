package builder

import (
	"context"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/backoff"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"
	"google.golang.org/grpc"

	"github.com/grafana/loki/v3/pkg/bloombuild/protos"
	"github.com/grafana/loki/v3/pkg/storage"
	v1 "github.com/grafana/loki/v3/pkg/storage/bloom/v1"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/local"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/bloomshipper"
	bloomshipperconfig "github.com/grafana/loki/v3/pkg/storage/stores/shipper/bloomshipper/config"
	"github.com/grafana/loki/v3/pkg/storage/types"
)

func Test_BuilderLoop(t *testing.T) {
	logger := log.NewNopLogger()
	//logger := log.NewLogfmtLogger(os.Stdout)

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
		BackoffConfig: backoff.Config{
			MinBackoff: 1 * time.Second,
			MaxBackoff: 10 * time.Second,
			MaxRetries: 5,
		},
	}
	flagext.DefaultValues(&cfg.GrpcConfig)

	builder, err := New(cfg, limits, schemaCfg, storageCfg, storage.NewClientMetrics(), nil, fakeBloomStore{}, logger, prometheus.DefaultRegisterer, nil)
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

	require.Eventually(t, func() bool {
		return server.CompletedTasks() >= len(tasks)
	}, 30*time.Second, 500*time.Millisecond)

	err = services.StopAndAwaitTerminated(context.Background(), builder)
	require.NoError(t, err)

	require.True(t, server.shutdownCalled)
}

type fakePlannerServer struct {
	tasks          []*protos.ProtoTask
	completedTasks atomic.Int64
	shutdownCalled bool

	listenAddr string
	grpcServer *grpc.Server
	wg         sync.WaitGroup
}

func newFakePlannerServer(tasks []*protos.ProtoTask) (*fakePlannerServer, error) {
	server := &fakePlannerServer{
		tasks: tasks,
	}

	return server, nil
}

func (f *fakePlannerServer) Addr() string {
	if f.listenAddr == "" {
		panic("server not started")
	}
	return f.listenAddr
}

func (f *fakePlannerServer) Stop() {
	if f.grpcServer != nil {
		f.grpcServer.Stop()
	}

	f.wg.Wait()
}

func (f *fakePlannerServer) Start() {
	f.Stop()

	lisAddr := "localhost:0"
	if f.listenAddr != "" {
		// Reuse the same address if the server was stopped and started again.
		lisAddr = f.listenAddr
	}

	lis, err := net.Listen("tcp", lisAddr)
	if err != nil {
		panic(err)
	}
	f.listenAddr = lis.Addr().String()

	f.grpcServer = grpc.NewServer()
	protos.RegisterPlannerForBuilderServer(f.grpcServer, f)
	go func() {
		if err := f.grpcServer.Serve(lis); err != nil {
			panic(err)
		}
	}()
}

func (f *fakePlannerServer) BuilderLoop(srv protos.PlannerForBuilder_BuilderLoopServer) error {
	f.wg.Add(1)
	defer f.wg.Done()

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
		time.Sleep(10 * time.Millisecond) // Simulate task processing time to add some latency.
		f.completedTasks.Inc()
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

func (f fakeLimits) BloomMaxBlockSize(_ string) int {
	panic("implement me")
}

func (f fakeLimits) BloomMaxBloomSize(_ string) int {
	panic("implement me")
}

type fakeBloomStore struct {
	bloomshipper.Store
}

func (f fakeBloomStore) BloomMetrics() *v1.Metrics {
	return nil
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
