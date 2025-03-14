package worker

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/crypto/tls"
	"github.com/grafana/dskit/grpcclient"
	"github.com/grafana/dskit/services"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/grafana/loki/v3/pkg/util/test"
)

func TestResetConcurrency(t *testing.T) {
	tests := []struct {
		name                string
		maxConcurrent       int
		numTargets          int
		expectedConcurrency int
	}{
		{
			name:                "create at least one processor per target",
			maxConcurrent:       0,
			numTargets:          2,
			expectedConcurrency: 2,
		},
		{
			name:                "concurrency is correct when numTargets does not divide evenly into maxConcurrent",
			maxConcurrent:       7,
			numTargets:          4,
			expectedConcurrency: 7,
		},
		{
			name:                "total concurrency dividing evenly",
			maxConcurrent:       6,
			numTargets:          2,
			expectedConcurrency: 6,
		},
		{
			name:                "total concurrency at least one processor per target",
			maxConcurrent:       3,
			numTargets:          6,
			expectedConcurrency: 6,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{
				MaxConcurrent: tt.maxConcurrent,
			}

			w, err := newQuerierWorkerWithProcessor(cfg.QuerySchedulerGRPCClientConfig, cfg.MaxConcurrent, cfg.DNSLookupPeriod, NewMetrics(cfg, nil), log.NewNopLogger(), &mockProcessor{}, "", nil, nil)
			require.NoError(t, err)
			require.NoError(t, services.StartAndAwaitRunning(context.Background(), w))

			for i := 0; i < tt.numTargets; i++ {
				// gRPC connections are virtual... they don't actually try to connect until they are needed.
				// This allows us to use dummy ports, and not get any errors.
				w.AddressAdded(fmt.Sprintf("127.0.0.1:%d", i))
			}

			test.Poll(t, 250*time.Millisecond, tt.expectedConcurrency, func() interface{} {
				return getConcurrentProcessors(w)
			})

			require.NoError(t, services.StopAndAwaitTerminated(context.Background(), w))
			assert.Equal(t, 0, getConcurrentProcessors(w))
		})
	}
}

func getConcurrentProcessors(w *querierWorker) int {
	result := 0
	w.mu.Lock()
	defer w.mu.Unlock()

	for _, mgr := range w.managers {
		result += int(mgr.currentProcessors.Load())
	}

	return result
}

type mockProcessor struct{}

func (m mockProcessor) processQueriesOnSingleStream(ctx context.Context, _ *grpc.ClientConn, _, _ string) {
	<-ctx.Done()
}

func (m mockProcessor) notifyShutdown(_ context.Context, _ *grpc.ClientConn, _ string) {}

func TestGRPCConfigBehavior(t *testing.T) {
	logger := log.NewNopLogger()

	t.Run("uses separated GRPC TLS server names", func(t *testing.T) {
		cfg := Config{
			SchedulerAddress: "scheduler:9095",
			QuerySchedulerGRPCClientConfig: grpcclient.Config{
				TLS: tls.ClientConfig{
					ServerName: "query-scheduler",
				},
			},
			NewQueryFrontendGRPCClientConfig: grpcclient.Config{
				TLS: tls.ClientConfig{
					ServerName: "query-frontend",
				},
			},
		}

		qw, err := NewQuerierWorker(cfg, nil, nil, logger, nil, nil)
		require.NoError(t, err)
		require.NoError(t, services.StopAndAwaitTerminated(context.Background(), qw))

		// grpc client the querier uses to talk to the scheduler, so the expected server name is "query-scheduler".
		castedQw := qw.(*querierWorker)
		require.Equal(t, "query-scheduler", castedQw.grpcClientConfig.TLS.ServerName)

		// grpc client the querier uses to return results to the frontend, so the expected server name is "query-frontend".
		sp := castedQw.processor.(*schedulerProcessor)
		require.Equal(t, "query-frontend", sp.grpcConfig.TLS.ServerName)
	})
}
