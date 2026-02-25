package wire_test

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/scheduler/wire"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/engine/internal/util/dag"
	"github.com/grafana/loki/v3/pkg/engine/internal/workflow"
)

// TestHTTP2BasicConnectivity tests basic connection establishment and communication.
func TestHTTP2BasicConnectivity(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Start HTTP/2 listener
	listener, shutdown := prepareHTTP2Listener(t)
	defer shutdown()

	addr := listener.Addr().String()
	t.Logf("Server listening on %s", addr)

	// Accept connection in goroutine
	var serverConn wire.Conn
	acceptErr := make(chan error, 1)
	go func() {
		var err error
		serverConn, err = listener.Accept(ctx)
		acceptErr <- err
	}()

	// Dial from client
	clientConn, err := wire.NewHTTP2Dialer("/").Dial(ctx, listener.Addr(), listener.Addr())
	require.NoError(t, err)
	defer clientConn.Close()

	// Wait for server to accept
	require.NoError(t, <-acceptErr)
	require.NotNil(t, serverConn)
	defer serverConn.Close()

	// Verify addresses
	require.Equal(t, listener.Addr(), serverConn.LocalAddr())
	require.Equal(t, listener.Addr(), serverConn.RemoteAddr(), "advertise address not propagated")
	t.Logf("Client local: %s, remote: %s", clientConn.LocalAddr(), clientConn.RemoteAddr())
	t.Logf("Server local: %s, remote: %s", serverConn.LocalAddr(), serverConn.RemoteAddr())

	// Send a frame from client to server
	testFrame := wire.AckFrame{ID: 42}
	err = clientConn.Send(ctx, testFrame)
	require.NoError(t, err)

	// Receive on server
	receivedFrame, err := serverConn.Recv(ctx)
	require.NoError(t, err)
	require.Equal(t, testFrame, receivedFrame)

	// Send a frame back from server to client
	responseFrame := wire.AckFrame{ID: 43}
	err = serverConn.Send(ctx, responseFrame)
	require.NoError(t, err)

	// Receive on client
	receivedResponse, err := clientConn.Recv(ctx)
	require.NoError(t, err)
	require.Equal(t, responseFrame, receivedResponse)
}

// TestHTTP2WithPeers demonstrates using wire.Peer for bidirectional communication.
func TestHTTP2WithPeers(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Start HTTP/2 listener
	listener, shutdown := prepareHTTP2Listener(t)
	defer shutdown()

	addr := listener.Addr().String()
	t.Logf("Server listening on %s", addr)

	// Channels to signal message receipt
	serverReceived := make(chan wire.Message, 1)
	clientReceived := make(chan wire.Message, 1)

	// Server handler
	serverHandler := func(ctx context.Context, peer *wire.Peer, message wire.Message) error {
		t.Logf("Server received: %T %+v", message, message)
		serverReceived <- message

		// Echo back a WorkerReadyMessage
		if _, ok := message.(wire.TaskStatusMessage); ok {
			return peer.SendMessageAsync(ctx, wire.WorkerReadyMessage{})
		}
		return nil
	}

	// Client handler
	clientHandler := func(_ context.Context, _ *wire.Peer, message wire.Message) error {
		t.Logf("Client received: %T %+v", message, message)
		clientReceived <- message
		return nil
	}

	// Accept server connection in goroutine
	var serverConn wire.Conn
	acceptErr := make(chan error, 1)
	go func() {
		var err error
		serverConn, err = listener.Accept(ctx)
		acceptErr <- err
	}()

	// Dial from client
	clientConn, err := wire.NewHTTP2Dialer("/").Dial(ctx, listener.Addr(), listener.Addr())
	require.NoError(t, err)
	defer clientConn.Close()

	// Wait for server to accept
	require.NoError(t, <-acceptErr)
	require.NotNil(t, serverConn)
	defer serverConn.Close()

	// Create server peer
	serverPeer := &wire.Peer{
		Logger:  log.NewNopLogger(),
		Metrics: wire.NewMetrics(),
		Conn:    serverConn,
		Handler: serverHandler,
		Buffer:  10,
	}

	// Create client peer
	clientPeer := &wire.Peer{
		Logger:  log.NewNopLogger(),
		Metrics: wire.NewMetrics(),
		Conn:    clientConn,
		Handler: clientHandler,
		Buffer:  10,
	}

	// Start both peers
	peerCtx, peerCancel := context.WithCancel(ctx)
	defer peerCancel()

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		_ = serverPeer.Serve(peerCtx)
	}()

	go func() {
		defer wg.Done()
		_ = clientPeer.Serve(peerCtx)
	}()

	// Send message from client to server (synchronous)
	err = clientPeer.SendMessage(ctx, wire.TaskStatusMessage{})
	require.NoError(t, err)

	// Wait for server to receive the message
	select {
	case msg := <-serverReceived:
		require.IsType(t, wire.TaskStatusMessage{}, msg)
	case <-ctx.Done():
		t.Fatal("timeout waiting for server to receive message")
	}

	// Wait for client to receive the echo
	select {
	case msg := <-clientReceived:
		require.IsType(t, wire.WorkerReadyMessage{}, msg)
	case <-ctx.Done():
		t.Fatal("timeout waiting for client to receive echo")
	}

	// Clean shutdown
	peerCancel()
	wg.Wait()
}

// TestHTTP2MultipleClients demonstrates multiple clients connecting to a single server.
func TestHTTP2MultipleClients(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Start HTTP/2 listener
	listener, shutdown := prepareHTTP2Listener(t)
	defer shutdown()

	addr := listener.Addr().String()
	t.Logf("Server listening on %s", addr)

	const numClients = 3

	// Track messages received by server from each client
	var (
		serverReceivedMu sync.Mutex
		serverReceived   = make(map[string][]wire.Message)
	)

	// Server handler
	serverHandler := func(ctx context.Context, peer *wire.Peer, message wire.Message) error {
		remoteAddr := peer.RemoteAddr().String()
		serverReceivedMu.Lock()
		serverReceived[remoteAddr] = append(serverReceived[remoteAddr], message)
		count := len(serverReceived[remoteAddr])
		serverReceivedMu.Unlock()

		t.Logf("Server received from %s: %T (total: %d)", remoteAddr, message, count)

		// Send acknowledgment back
		return peer.SendMessageAsync(ctx, wire.WorkerReadyMessage{})
	}

	// Accept connections and create server peers
	var serverPeers []*wire.Peer
	var serverWg sync.WaitGroup

	peerCtx, peerCancel := context.WithCancel(ctx)
	defer peerCancel()

	go func() {
		for i := 0; i < numClients; i++ {
			conn, err := listener.Accept(peerCtx)
			if err != nil {
				if peerCtx.Err() != nil {
					return
				}
				t.Errorf("Accept failed: %v", err)
				return
			}

			peer := &wire.Peer{
				Logger:  log.NewNopLogger(),
				Metrics: wire.NewMetrics(),
				Conn:    conn,
				Handler: serverHandler,
				Buffer:  10,
			}
			serverPeers = append(serverPeers, peer)

			serverWg.Add(1)
			go func(p *wire.Peer) {
				defer serverWg.Done()
				_ = p.Serve(peerCtx)
			}(peer)

			t.Logf("Server accepted connection %d from %s", i+1, conn.RemoteAddr())
		}
	}()

	// Create multiple clients
	var clientWg sync.WaitGroup
	clientReceivedCounts := make([]int, numClients)
	var clientReceivedMu sync.Mutex

	for i := 0; i < numClients; i++ {
		clientIdx := i
		clientWg.Add(1)

		go func() {
			defer clientWg.Done()

			// Connect
			conn, err := wire.NewHTTP2Dialer("/").Dial(ctx, listener.Addr(), listener.Addr())
			if err != nil {
				t.Errorf("Client %d dial failed: %v", clientIdx, err)
				return
			}
			defer conn.Close()

			// Channel to signal when this client has received all responses
			clientDone := make(chan struct{})

			// Handler to count received messages
			handler := func(_ context.Context, _ *wire.Peer, message wire.Message) error {
				clientReceivedMu.Lock()
				clientReceivedCounts[clientIdx]++
				count := clientReceivedCounts[clientIdx]
				clientReceivedMu.Unlock()

				t.Logf("Client %d received: %T (total: %d)", clientIdx, message, count)

				if count == 3 {
					close(clientDone)
				}
				return nil
			}

			peer := &wire.Peer{
				Logger:  log.NewNopLogger(),
				Metrics: wire.NewMetrics(),
				Conn:    conn,
				Handler: handler,
				Buffer:  10,
			}

			// Start peer
			peerDoneCtx, peerDone := context.WithCancel(peerCtx)
			defer peerDone()

			var peerWg sync.WaitGroup
			peerWg.Add(1)
			go func() {
				defer peerWg.Done()
				_ = peer.Serve(peerDoneCtx)
			}()

			// Send messages
			for j := 0; j < 3; j++ {
				msg := wire.TaskStatusMessage{}
				err := peer.SendMessage(ctx, msg)
				if err != nil {
					t.Errorf("Client %d send failed: %v", clientIdx, err)
					return
				}
				t.Logf("Client %d sent message %d", clientIdx, j+1)
			}

			// Wait for all responses
			select {
			case <-clientDone:
			case <-ctx.Done():
				t.Errorf("Client %d timeout waiting for responses", clientIdx)
			}

			peerDone()
			peerWg.Wait()
		}()
	}

	// Wait for all clients to finish
	clientWg.Wait()

	// Verify messages were received
	serverReceivedMu.Lock()
	totalReceived := 0
	for addr, messages := range serverReceived {
		t.Logf("Server received %d messages from %s", len(messages), addr)
		totalReceived += len(messages)
	}
	serverReceivedMu.Unlock()

	require.Equal(t, numClients*3, totalReceived, "server should receive all messages from all clients")

	// Verify clients received responses
	clientReceivedMu.Lock()
	for i, count := range clientReceivedCounts {
		t.Logf("Client %d received %d messages", i, count)
		require.Equal(t, 3, count, "each client should receive 3 acknowledgments")
	}
	clientReceivedMu.Unlock()

	// Clean shutdown
	peerCancel()
	serverWg.Wait()
}

// TestHTTP2ErrorHandling tests error scenarios.
func TestHTTP2ErrorHandling(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Start HTTP/2 listener
	listener, shutdown := prepareHTTP2Listener(t)
	defer shutdown()

	// Handler that returns an error
	errorHandler := func(_ context.Context, _ *wire.Peer, _ wire.Message) error {
		return errors.New("simulated error")
	}

	// Accept connection
	var serverConn wire.Conn
	acceptErr := make(chan error, 1)
	go func() {
		var err error
		serverConn, err = listener.Accept(ctx)
		acceptErr <- err
	}()

	// Dial from client
	clientConn, err := wire.NewHTTP2Dialer("/").Dial(ctx, listener.Addr(), listener.Addr())
	require.NoError(t, err)
	defer clientConn.Close()

	// Wait for server to accept
	require.NoError(t, <-acceptErr)
	require.NotNil(t, serverConn)
	defer serverConn.Close()

	// Create peers
	serverPeer := &wire.Peer{
		Logger:  log.NewNopLogger(),
		Metrics: wire.NewMetrics(),
		Conn:    serverConn,
		Handler: errorHandler,
		Buffer:  10,
	}

	clientPeer := &wire.Peer{
		Logger:  log.NewNopLogger(),
		Metrics: wire.NewMetrics(),
		Conn:    clientConn,
		Handler: func(_ context.Context, _ *wire.Peer, _ wire.Message) error { return nil },
		Buffer:  10,
	}

	// Start peers
	peerCtx, peerCancel := context.WithCancel(ctx)
	defer peerCancel()

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		_ = serverPeer.Serve(peerCtx)
	}()

	go func() {
		defer wg.Done()
		_ = clientPeer.Serve(peerCtx)
	}()

	// Send message that will trigger error
	err = clientPeer.SendMessage(ctx, wire.WorkerReadyMessage{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "simulated error")

	// Clean shutdown
	peerCancel()
	wg.Wait()
}

// TestHTTP2MessageFrameSerialization tests that MessageFrames with different message types serialize correctly.
func TestHTTP2MessageFrameSerialization(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Start HTTP/2 listener
	listener, shutdown := prepareHTTP2Listener(t)
	defer shutdown()

	// Accept connection
	var serverConn wire.Conn
	acceptErr := make(chan error, 1)
	go func() {
		var err error
		serverConn, err = listener.Accept(ctx)
		acceptErr <- err
	}()

	// Dial from client
	clientConn, err := wire.NewHTTP2Dialer("/").Dial(ctx, listener.Addr(), listener.Addr())
	require.NoError(t, err)
	defer clientConn.Close()

	// Wait for server to accept
	require.NoError(t, <-acceptErr)
	require.NotNil(t, serverConn)
	defer serverConn.Close()

	expectedPlan := &physical.Plan{}
	parallelize := expectedPlan.Graph().Add(&physical.Parallelize{NodeID: ulid.Make()})
	compat := expectedPlan.Graph().Add(&physical.ColumnCompat{NodeID: ulid.Make(), Source: types.ColumnTypeMetadata, Destination: types.ColumnTypeMetadata, Collisions: []types.ColumnType{types.ColumnTypeLabel}})
	scanSet := expectedPlan.Graph().Add(&physical.ScanSet{
		NodeID: ulid.Make(),
		Targets: []*physical.ScanTarget{
			{Type: physical.ScanTypeDataObject, DataObject: &physical.DataObjScan{NodeID: ulid.Make(), Location: "obj1", Section: 3, StreamIDs: []int64{1, 2}}},
			{Type: physical.ScanTypeDataObject, DataObject: &physical.DataObjScan{NodeID: ulid.Make(), Location: "obj2", Section: 1, StreamIDs: []int64{3, 4}}},
			{Type: physical.ScanTypeDataObject, DataObject: &physical.DataObjScan{NodeID: ulid.Make(), Location: "obj3", Section: 2, StreamIDs: []int64{5, 1}}},
			{Type: physical.ScanTypeDataObject, DataObject: &physical.DataObjScan{NodeID: ulid.Make(), Location: "obj3", Section: 3, StreamIDs: []int64{5, 1}}},
		},
	})

	_ = expectedPlan.Graph().AddEdge(dag.Edge[physical.Node]{Parent: parallelize, Child: compat})
	_ = expectedPlan.Graph().AddEdge(dag.Edge[physical.Node]{Parent: compat, Child: scanSet})

	testCases := []struct {
		name    string
		message wire.Message
	}{
		{"WorkerReadyMessage", wire.WorkerReadyMessage{}},
		{"TaskCancelMessage", wire.TaskCancelMessage{}},
		{"TaskFlagMessage", wire.TaskFlagMessage{Interruptible: true}},
		{"TaskStatusMessage", wire.TaskStatusMessage{}},
		{"TaskAssignMessage", wire.TaskAssignMessage{
			Task: &workflow.Task{
				ULID:     ulid.Make(),
				TenantID: "fake",
				Fragment: expectedPlan,
				Sources: map[physical.Node][]*workflow.Stream{
					compat: {
						{ULID: ulid.Make(), TenantID: "fake"},
					},
				},
				Sinks: map[physical.Node][]*workflow.Stream{
					scanSet: {
						{ULID: ulid.Make(), TenantID: "fake"},
					},
				},
			},
			StreamStates: nil,
		}},
		{"StreamStatusMessage", wire.StreamStatusMessage{}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Send message frame
			frame := wire.MessageFrame{ID: 123, Message: tc.message}
			err := clientConn.Send(ctx, frame)
			require.NoError(t, err)

			// Receive and verify
			received, err := serverConn.Recv(ctx)
			require.NoError(t, err)

			receivedFrame, ok := received.(wire.MessageFrame)
			require.True(t, ok, "expected MessageFrame")
			require.Equal(t, uint64(123), receivedFrame.ID)
			require.IsType(t, tc.message, receivedFrame.Message)
		})
	}
}

func prepareHTTP2Listener(t *testing.T) (*wire.HTTP2Listener, func()) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	listener := wire.NewHTTP2Listener(
		l.Addr(),
		wire.WithHTTP2ListenerLogger(log.NewNopLogger()),
	)

	mux := http.NewServeMux()
	mux.Handle("/", listener)

	server := &http.Server{
		Handler: h2c.NewHandler(mux, &http2.Server{}),
	}
	wgServe := sync.WaitGroup{}
	wgServe.Add(1)

	go func() {
		defer wgServe.Done()

		err = server.Serve(l)
		require.Error(t, err, http.ErrServerClosed)
	}()

	return listener, func() {
		ctx := context.Background()
		require.NoError(t, listener.Close(ctx))
		require.NoError(t, server.Shutdown(ctx))
		wgServe.Wait()
	}
}
