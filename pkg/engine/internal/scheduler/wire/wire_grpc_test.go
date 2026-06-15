package wire_test

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	"github.com/grafana/loki/v3/pkg/engine/internal/scheduler/wire"
)

// TestGRPCBasicConnectivity tests connection establishment, address
// propagation, and bidirectional frame delivery over the gRPC transport.
func TestGRPCBasicConnectivity(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	listener, dialer, serverAddr, shutdown := prepareGRPCTransport(t)
	defer shutdown()

	clientAddr := &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 8001}

	var serverConn wire.Conn
	acceptErr := make(chan error, 1)
	go func() {
		var err error
		serverConn, err = listener.Accept(ctx)
		acceptErr <- err
	}()

	clientConn, err := dialer.Dial(ctx, clientAddr, serverAddr)
	require.NoError(t, err)
	defer clientConn.Close()

	require.NoError(t, <-acceptErr)
	require.NotNil(t, serverConn)
	defer serverConn.Close()

	// The dialing peer's advertised address must reach the server.
	require.Equal(t, serverAddr.String(), serverConn.LocalAddr().String())
	require.Equal(t, clientAddr.String(), serverConn.RemoteAddr().String(), "advertise address not propagated")

	// Client -> server.
	require.NoError(t, clientConn.Send(ctx, wire.AckFrame{ID: 42}))
	got, err := serverConn.Recv(ctx)
	require.NoError(t, err)
	require.Equal(t, wire.AckFrame{ID: 42}, got)

	// Server -> client.
	require.NoError(t, serverConn.Send(ctx, wire.AckFrame{ID: 43}))
	got, err = clientConn.Recv(ctx)
	require.NoError(t, err)
	require.Equal(t, wire.AckFrame{ID: 43}, got)
}

// TestGRPCPeerWithULIDCustomType drives a full Peer ack cycle and, crucially,
// round-trips a message carrying a ULID gogo customtype. This verifies that the
// default gRPC codec used by the generated stubs dispatches to the message's own
// gogo Marshal/Unmarshal, preserving customtype fields.
func TestGRPCPeerWithULIDCustomType(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	listener, dialer, serverAddr, shutdown := prepareGRPCTransport(t)
	defer shutdown()

	clientAddr := &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 8002}

	serverReceived := make(chan wire.Message, 1)
	serverHandler := func(_ context.Context, _ *wire.Peer, message wire.Message) error {
		serverReceived <- message
		return nil
	}

	var serverConn wire.Conn
	acceptErr := make(chan error, 1)
	go func() {
		var err error
		serverConn, err = listener.Accept(ctx)
		acceptErr <- err
	}()

	clientConn, err := dialer.Dial(ctx, clientAddr, serverAddr)
	require.NoError(t, err)
	defer clientConn.Close()

	require.NoError(t, <-acceptErr)
	require.NotNil(t, serverConn)
	defer serverConn.Close()

	serverPeer := &wire.Peer{Logger: log.NewNopLogger(), Metrics: wire.NewMetrics(), Conn: serverConn, Handler: serverHandler, Buffer: 10}
	clientPeer := &wire.Peer{Logger: log.NewNopLogger(), Metrics: wire.NewMetrics(), Conn: clientConn, Buffer: 10}

	peerCtx, peerCancel := context.WithCancel(ctx)
	defer peerCancel()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); _ = serverPeer.Serve(peerCtx) }()
	go func() { defer wg.Done(); _ = clientPeer.Serve(peerCtx) }()

	// TaskCancelMessage carries a ULID customtype.
	id := ulid.Make()
	require.NoError(t, clientPeer.SendMessage(ctx, wire.TaskCancelMessage{ID: id}))

	select {
	case msg := <-serverReceived:
		cancelMsg, ok := msg.(wire.TaskCancelMessage)
		require.True(t, ok, "expected TaskCancelMessage, got %T", msg)
		require.Equal(t, id, cancelMsg.ID, "ULID customtype did not round-trip")
	case <-ctx.Done():
		t.Fatal("timeout waiting for server to receive message")
	}

	peerCancel()
	wg.Wait()
}

func prepareGRPCTransport(t *testing.T) (*wire.GRPCListener, wire.Dialer, net.Addr, func()) {
	t.Helper()

	lis := bufconn.Listen(1024 * 1024)
	advertise := &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 9999}

	listener := wire.NewGRPCListener(advertise, wire.WithGRPCListenerLogger(log.NewNopLogger()))
	srv := grpc.NewServer()
	listener.Register(srv)

	serveDone := make(chan struct{})
	go func() {
		defer close(serveDone)
		_ = srv.Serve(lis)
	}()

	dialer := wire.NewGRPCDialer(
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)

	cleanup := func() {
		_ = listener.Close(context.Background())
		_ = dialer.Close()
		srv.Stop()
		<-serveDone
	}
	return listener, dialer, advertise, cleanup
}
