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
	logger := log.NewNopLogger()

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

	require.Eventually(t, func() bool {
		return int(server.completedTasks.Load()) == len(tasks)
	}, 5*time.Second, 100*time.Millisecond)

	err = services.StopAndAwaitTerminated(context.Background(), builder)
	require.NoError(t, err)

	require.True(t, server.shutdownCalled)
}

type fakePlannerServer struct {
	tasks          []*protos.ProtoTask
	completedTasks atomic.Int64
	shutdownCalled bool

	addr       string
	grpcServer *grpc.Server
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
		f.completedTasks.Add(1)
	}

	// No more tasks. Wait until shutdown.
	<-srv.Context().Done()
	return nil
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
