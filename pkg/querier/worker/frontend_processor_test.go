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
