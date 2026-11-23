package worker

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	"github.com/grafana/loki/v3/pkg/lokifrontend/frontend/v1/frontendv1pb"
	"github.com/grafana/loki/v3/pkg/querier/queryrange"
	"github.com/grafana/loki/v3/pkg/util/test"
)

const bufConnSize = 1024 * 1024

func TestRecvFailDoesntCancelProcess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	listener := bufconn.Listen(bufConnSize)
	defer listener.Close()

	// nolint:staticcheck // grpc.DialContext() has been deprecated; we'll address it before upgrading to gRPC 2.
	cc, err := grpc.DialContext(ctx, "bufnet", grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
		return listener.Dial()
	}), grpc.WithTransportCredentials(insecure.NewCredentials()))

	require.NoError(t, err)

	cfg := Config{}
	mgr := newFrontendProcessor(cfg, nil, log.NewNopLogger(), queryrange.DefaultCodec)
	running := atomic.NewBool(false)
	go func() {
		running.Store(true)
		defer running.Store(false)

		mgr.processQueriesOnSingleStream(ctx, cc, "test:12345", "")
	}()

	test.Poll(t, time.Second, true, func() interface{} {
		return running.Load()
	})

	// Wait a bit, and verify that processQueriesOnSingleStream is still running, and hasn't stopped
	// just because it cannot contact frontend.
	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, true, running.Load())

	cancel()
	test.Poll(t, time.Second, false, func() interface{} {
		return running.Load()
	})
}

func TestFrontendProcessor_ReconnectsWithoutBackoffWhenStreamIsCanceled(t *testing.T) {
	frontend := &cancelingFrontend{cancellations: 5}
	server := grpc.NewServer()
	frontendv1pb.RegisterFrontendServer(server, frontend)
	listener := bufconn.Listen(bufConnSize)
	go func() {
		_ = server.Serve(listener)
	}()
	t.Cleanup(server.Stop)

	// nolint:staticcheck // grpc.DialContext() has been deprecated; we'll address it before upgrading to gRPC 2.
	conn, err := grpc.DialContext(context.Background(), "bufnet", grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
		return listener.Dial()
	}), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = conn.Close()
	})

	cfg := Config{QuerierID: "test-querier-id"}
	fp := newFrontendProcessor(cfg, nil, log.NewNopLogger(), queryrange.DefaultCodec)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		fp.processQueriesOnSingleStream(ctx, conn, "bufnet", "")
	}()

	// A single backoff would take at least processorBackoffConfig.MinBackoff.
	test.Poll(t, processorBackoffConfig.MinBackoff, int64(frontend.cancellations+1), func() interface{} {
		return frontend.streams.Load()
	})

	cancel()
	<-done
}

// cancelingFrontend closes the first streams with the error the query-frontend returns when it cancels the query
// a querier is running, and keeps the stream that follows open until the querier closes it.
type cancelingFrontend struct {
	frontendv1pb.UnimplementedFrontendServer

	cancellations int
	streams       atomic.Int64
}

func (f *cancelingFrontend) Process(server frontendv1pb.Frontend_ProcessServer) error {
	if f.streams.Inc() <= int64(f.cancellations) {
		return context.Canceled
	}

	<-server.Context().Done()
	return nil
}

func TestContextCancelStopsProcess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	listener := bufconn.Listen(bufConnSize)
	defer listener.Close()

	// nolint:staticcheck // grpc.DialContext() has been deprecated; we'll address it before upgrading to gRPC 2.
	cc, err := grpc.DialContext(ctx, "bufnet", grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
		return listener.Dial()
	}), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)

	pm := newProcessorManager(ctx, &mockProcessor{}, cc, "test")
	pm.concurrency(1)

	test.Poll(t, time.Second, 1, func() interface{} {
		return int(pm.currentProcessors.Load())
	})

	cancel()

	test.Poll(t, time.Second, 0, func() interface{} {
		return int(pm.currentProcessors.Load())
	})

	pm.stop()
	test.Poll(t, time.Second, 0, func() interface{} {
		return int(pm.currentProcessors.Load())
	})
}
