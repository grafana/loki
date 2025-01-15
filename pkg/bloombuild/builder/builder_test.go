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

	"github.com/grafana/loki/v3/pkg/bloombuild/planner/plannertest"
	"github.com/grafana/loki/v3/pkg/bloombuild/protos"
	"github.com/grafana/loki/v3/pkg/storage"
	v1 "github.com/grafana/loki/v3/pkg/storage/bloom/v1"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/local"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/bloomshipper"
	bloomshipperconfig "github.com/grafana/loki/v3/pkg/storage/stores/shipper/bloomshipper/config"
	"github.com/grafana/loki/v3/pkg/storage/types"
)

func setupBuilder(t *testing.T, plannerAddr string, limits Limits, logger log.Logger) *Builder {
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

	cfg := Config{
		PlannerAddress: plannerAddr,
		BackoffConfig: backoff.Config{
			MinBackoff: 1 * time.Second,
			MaxBackoff: 10 * time.Second,
			MaxRetries: 5,
		},
	}
	flagext.DefaultValues(&cfg.GrpcConfig)

	metrics := storage.NewClientMetrics()
	metrics.Unregister()

	builder, err := New(cfg, limits, schemaCfg, storageCfg, metrics, nil, fakeBloomStore{}, nil, logger, prometheus.NewPedanticRegistry(), nil)
	require.NoError(t, err)

	return builder
}

func createTasks(n int) []*protos.ProtoTask {
	tasks := make([]*protos.ProtoTask, n)
	for i := range tasks {
		tasks[i] = protos.NewTask(
			plannertest.TestTable,
			"fake",
			v1.NewBounds(model.Fingerprint(i), model.Fingerprint(i+10)),
			plannertest.TsdbID(1),
			[]protos.Gap{
				{
					Bounds: v1.NewBounds(model.Fingerprint(i+1), model.Fingerprint(i+2)),
				},
				{
					Bounds: v1.NewBounds(model.Fingerprint(i+3), model.Fingerprint(i+9)),
				},
			},
		).ToProtoTask()
	}
	return tasks
}

func Test_BuilderLoop(t *testing.T) {
	logger := log.NewNopLogger()
	//logger := log.NewLogfmtLogger(os.Stdout)

	tasks := createTasks(256)
	server, err := newFakePlannerServer(tasks)
	require.NoError(t, err)

	// Start the server so the builder can connect and receive tasks.
	server.Start()

	builder := setupBuilder(t, server.Addr(), fakeLimits{}, logger)
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

func Test_BuilderLoop_Timeout(t *testing.T) {
	for _, tc := range []struct {
		name            string
		timeout         time.Duration
		allTasksSucceed bool
	}{
		{
			name:            "no timeout configured",
			timeout:         0,
			allTasksSucceed: true,
		},
		{
			name:            "long enough timeout",
			timeout:         15 * time.Minute,
			allTasksSucceed: true,
		},
		{
			name:            "task times out",
			timeout:         1 * time.Nanosecond, // Pretty much immediately.
			allTasksSucceed: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logger := log.NewNopLogger()
			//logger := log.NewLogfmtLogger(os.Stdout)

			tasks := createTasks(256)
			server, err := newFakePlannerServer(tasks)
			require.NoError(t, err)

			// Start the server so the builder can connect and receive tasks.
			server.Start()

			limits := fakeLimits{
				taskTimout: tc.timeout,
			}
			builder := setupBuilder(t, server.Addr(), limits, logger)
			t.Cleanup(func() {
				err = services.StopAndAwaitTerminated(context.Background(), builder)
				require.NoError(t, err)

				server.Stop()
			})

			err = services.StartAndAwaitRunning(context.Background(), builder)
			require.NoError(t, err)

			require.Eventually(t, func() bool {
				return server.CompletedTasks() >= len(tasks)
			}, 30*time.Second, 500*time.Millisecond)

			erroredTasks := server.ErroredTasks()
			if tc.allTasksSucceed {
				require.Equal(t, 0, erroredTasks)
			} else {
				require.Equal(t, len(tasks), erroredTasks)
			}
		})
	}
}

type fakePlannerServer struct {
	tasks          []*protos.ProtoTask
	completedTasks atomic.Int64
	erroredTasks   atomic.Int64
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

		result, err := srv.Recv()
		if err != nil {
			return fmt.Errorf("failed to receive task response: %w", err)
		}

		f.completedTasks.Inc()
		if result.Result.Error != "" {
			f.erroredTasks.Inc()
		}

		time.Sleep(10 * time.Millisecond) // Simulate task processing time to add some latency.
	}

	// No more tasks. Wait until shutdown.
	<-srv.Context().Done()
	return nil
}

func (f *fakePlannerServer) CompletedTasks() int {
	return int(f.completedTasks.Load())
}

func (f *fakePlannerServer) ErroredTasks() int {
	return int(f.erroredTasks.Load())
}

func (f *fakePlannerServer) NotifyBuilderShutdown(_ context.Context, _ *protos.NotifyBuilderShutdownRequest) (*protos.NotifyBuilderShutdownResponse, error) {
	f.shutdownCalled = true
	return &protos.NotifyBuilderShutdownResponse{}, nil
}

type fakeLimits struct {
	Limits
	taskTimout time.Duration
}

var _ Limits = fakeLimits{}

func (f fakeLimits) BloomBlockEncoding(_ string) string {
	return "none"
}

func (f fakeLimits) BloomMaxBlockSize(_ string) int {
	return 0
}

func (f fakeLimits) BloomMaxBloomSize(_ string) int {
	return 0
}

func (f fakeLimits) BuilderResponseTimeout(_ string) time.Duration {
	return f.taskTimout
}

type fakeBloomStore struct {
	bloomshipper.Store
}

func (f fakeBloomStore) BloomMetrics() *v1.Metrics {
	return nil
}

func (f fakeBloomStore) Client(_ model.Time) (bloomshipper.Client, error) {
	return fakeBloomClient{}, nil
}

func (f fakeBloomStore) Fetcher(_ model.Time) (*bloomshipper.Fetcher, error) {
	return &bloomshipper.Fetcher{}, nil
}

type fakeBloomClient struct {
	bloomshipper.Client
}

func (f fakeBloomClient) PutBlock(_ context.Context, _ bloomshipper.Block) error {
	return nil
}

func (f fakeBloomClient) PutMeta(_ context.Context, _ bloomshipper.Meta) error {
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
