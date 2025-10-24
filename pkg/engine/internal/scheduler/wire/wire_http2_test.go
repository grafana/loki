package wire

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"
)

// TestHTTP2BasicConnectivity tests basic connection establishment and communication.
func TestHTTP2BasicConnectivity(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Start HTTP/2 listener
	listener := prepareHTTP2Listener(t)
	defer listener.Close(ctx)

	addr := listener.Addr().String()
	t.Logf("Server listening on %s", addr)

	// Accept connection in goroutine
	var serverConn Conn
	acceptErr := make(chan error, 1)
	go func() {
		var err error
		serverConn, err = listener.Accept(ctx)
		acceptErr <- err
	}()

	// Dial from client
	clientConn, err := NewHTTP2Dialer().Dial(ctx, addr, NewJSONProtocol)
	require.NoError(t, err)
	defer clientConn.Close()

	// Wait for server to accept
	require.NoError(t, <-acceptErr)
	require.NotNil(t, serverConn)
	defer serverConn.Close()

	// Verify addresses
	require.Equal(t, listener.Addr(), serverConn.LocalAddr())
	t.Logf("Client local: %s, remote: %s", clientConn.LocalAddr(), clientConn.RemoteAddr())
	t.Logf("Server local: %s, remote: %s", serverConn.LocalAddr(), serverConn.RemoteAddr())

	// Send a frame from client to server
	testFrame := AckFrame{ID: 42}
	err = clientConn.Send(ctx, testFrame)
	require.NoError(t, err)

	// Receive on server
	receivedFrame, err := serverConn.Recv(ctx)
	require.NoError(t, err)
	require.Equal(t, testFrame, receivedFrame)

	// Send a frame back from server to client
	responseFrame := AckFrame{ID: 43}
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
	listener := prepareHTTP2Listener(t)
	defer listener.Close(ctx)

	addr := listener.Addr().String()
	t.Logf("Server listening on %s", addr)

	// Track received messages
	var (
		serverReceivedMu sync.Mutex
		serverReceived   []Message
		clientReceivedMu sync.Mutex
		clientReceived   []Message
	)

	// Server handler
	serverHandler := func(ctx context.Context, peer *Peer, message Message) error {
		serverReceivedMu.Lock()
		serverReceived = append(serverReceived, message)
		serverReceivedMu.Unlock()

		t.Logf("Server received: %T %+v", message, message)

		// Echo back a WorkerReadyMessage
		if _, ok := message.(TaskStatusMessage); ok {
			return peer.SendMessageAsync(ctx, WorkerReadyMessage{})
		}
		return nil
	}

	// Client handler
	clientHandler := func(_ context.Context, _ *Peer, message Message) error {
		clientReceivedMu.Lock()
		clientReceived = append(clientReceived, message)
		clientReceivedMu.Unlock()

		t.Logf("Client received: %T %+v", message, message)
		return nil
	}

	// Accept server connection in goroutine
	var serverConn Conn
	acceptErr := make(chan error, 1)
	go func() {
		var err error
		serverConn, err = listener.Accept(ctx)
		acceptErr <- err
	}()

	// Dial from client
	clientConn, err := NewHTTP2Dialer().Dial(ctx, addr, NewJSONProtocol)
	require.NoError(t, err)
	defer clientConn.Close()

	// Wait for server to accept
	require.NoError(t, <-acceptErr)
	require.NotNil(t, serverConn)
	defer serverConn.Close()

	// Create server peer
	serverPeer := &Peer{
		Logger:  log.NewNopLogger(),
		Conn:    serverConn,
		Handler: serverHandler,
		Buffer:  10,
	}

	// Create client peer
	clientPeer := &Peer{
		Logger:  log.NewNopLogger(),
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

	// Give peers time to start
	time.Sleep(100 * time.Millisecond)

	// Send message from client to server (synchronous)
	err = clientPeer.SendMessage(ctx, TaskStatusMessage{})
	require.NoError(t, err)

	// Wait for message to be processed
	require.Eventually(t, func() bool {
		serverReceivedMu.Lock()
		defer serverReceivedMu.Unlock()
		return len(serverReceived) > 0
	}, 2*time.Second, 50*time.Millisecond)

	// Verify server received the message
	serverReceivedMu.Lock()
	require.Len(t, serverReceived, 1)
	require.IsType(t, TaskStatusMessage{}, serverReceived[0])
	serverReceivedMu.Unlock()

	// Wait for echo response
	require.Eventually(t, func() bool {
		clientReceivedMu.Lock()
		defer clientReceivedMu.Unlock()
		return len(clientReceived) > 0
	}, 2*time.Second, 50*time.Millisecond)

	// Verify client received the echo
	clientReceivedMu.Lock()
	require.Len(t, clientReceived, 1)
	require.IsType(t, WorkerReadyMessage{}, clientReceived[0])
	clientReceivedMu.Unlock()

	// Clean shutdown
	peerCancel()
	wg.Wait()
}

// TestHTTP2MultipleClients demonstrates multiple clients connecting to a single server.
func TestHTTP2MultipleClients(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Start HTTP/2 listener
	listener := prepareHTTP2Listener(t)
	defer listener.Close(ctx)

	addr := listener.Addr().String()
	t.Logf("Server listening on %s", addr)

	const numClients = 3

	// Track messages received by server from each client
	var (
		serverReceivedMu sync.Mutex
		serverReceived   = make(map[string][]Message)
	)

	// Server handler
	serverHandler := func(ctx context.Context, peer *Peer, message Message) error {
		remoteAddr := peer.RemoteAddr().String()
		serverReceivedMu.Lock()
		serverReceived[remoteAddr] = append(serverReceived[remoteAddr], message)
		count := len(serverReceived[remoteAddr])
		serverReceivedMu.Unlock()

		t.Logf("Server received from %s: %T (total: %d)", remoteAddr, message, count)

		// Send acknowledgment back
		return peer.SendMessageAsync(ctx, WorkerReadyMessage{})
	}

	// Accept connections and create server peers
	var serverPeers []*Peer
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

			peer := &Peer{
				Logger:  log.NewNopLogger(),
				Conn:    conn,
				Handler: serverHandler,
				Buffer:  10,
			}
			serverPeers = append(serverPeers, peer)

			serverWg.Add(1)
			go func(p *Peer) {
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
			conn, err := NewHTTP2Dialer().Dial(ctx, addr, NewJSONProtocol)
			if err != nil {
				t.Errorf("Client %d dial failed: %v", clientIdx, err)
				return
			}
			defer conn.Close()

			// Handler to count received messages
			handler := func(_ context.Context, _ *Peer, message Message) error {
				clientReceivedMu.Lock()
				clientReceivedCounts[clientIdx]++
				count := clientReceivedCounts[clientIdx]
				clientReceivedMu.Unlock()

				t.Logf("Client %d received: %T (total: %d)", clientIdx, message, count)
				return nil
			}

			peer := &Peer{
				Logger:  log.NewNopLogger(),
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

			// Give peer time to start
			time.Sleep(100 * time.Millisecond)

			// Send messages
			for j := 0; j < 3; j++ {
				msg := TaskStatusMessage{}
				err := peer.SendMessage(ctx, msg)
				if err != nil {
					t.Errorf("Client %d send failed: %v", clientIdx, err)
					return
				}
				t.Logf("Client %d sent message %d", clientIdx, j+1)
			}

			// Wait a bit for responses
			time.Sleep(500 * time.Millisecond)

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
	listener := prepareHTTP2Listener(t)
	defer listener.Close(ctx)

	addr := listener.Addr().String()

	// Handler that returns an error
	errorHandler := func(_ context.Context, _ *Peer, _ Message) error {
		return errors.New("simulated error")
	}

	// Accept connection
	var serverConn Conn
	acceptErr := make(chan error, 1)
	go func() {
		var err error
		serverConn, err = listener.Accept(ctx)
		acceptErr <- err
	}()

	// Dial from client
	clientConn, err := NewHTTP2Dialer().Dial(ctx, addr, NewJSONProtocol)
	require.NoError(t, err)
	defer clientConn.Close()

	// Wait for server to accept
	require.NoError(t, <-acceptErr)
	require.NotNil(t, serverConn)
	defer serverConn.Close()

	// Create peers
	serverPeer := &Peer{
		Logger:  log.NewNopLogger(),
		Conn:    serverConn,
		Handler: errorHandler,
		Buffer:  10,
	}

	clientPeer := &Peer{
		Logger:  log.NewNopLogger(),
		Conn:    clientConn,
		Handler: func(_ context.Context, _ *Peer, _ Message) error { return nil },
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

	// Give peers time to start
	time.Sleep(100 * time.Millisecond)

	// Send message that will trigger error
	err = clientPeer.SendMessage(ctx, WorkerReadyMessage{})
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
	listener := prepareHTTP2Listener(t)
	defer listener.Close(ctx)

	addr := listener.Addr().String()

	// Accept connection
	var serverConn Conn
	acceptErr := make(chan error, 1)
	go func() {
		var err error
		serverConn, err = listener.Accept(ctx)
		acceptErr <- err
	}()

	// Dial from client
	clientConn, err := NewHTTP2Dialer().Dial(ctx, addr, NewJSONProtocol)
	require.NoError(t, err)
	defer clientConn.Close()

	// Wait for server to accept
	require.NoError(t, <-acceptErr)
	require.NotNil(t, serverConn)
	defer serverConn.Close()

	testCases := []struct {
		name    string
		message Message
	}{
		{"WorkerReadyMessage", WorkerReadyMessage{}},
		{"TaskCancelMessage", TaskCancelMessage{}},
		{"TaskFlagMessage", TaskFlagMessage{Interruptible: true}},
		{"TaskStatusMessage", TaskStatusMessage{}},
		{"StreamBindMessage", StreamBindMessage{}},
		{"StreamStatusMessage", StreamStatusMessage{}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Send message frame
			frame := MessageFrame{ID: 123, Message: tc.message}
			err := clientConn.Send(ctx, frame)
			require.NoError(t, err)

			// Receive and verify
			received, err := serverConn.Recv(ctx)
			require.NoError(t, err)

			receivedFrame, ok := received.(MessageFrame)
			require.True(t, ok, "expected MessageFrame")
			require.Equal(t, uint64(123), receivedFrame.ID)
			require.IsType(t, tc.message, receivedFrame.Message)
		})
	}
}

func prepareHTTP2Listener(t *testing.T) *HTTP2Listener {
	listener, err := NewHTTP2Listener(
		"127.0.0.1:0",
		NewJSONProtocol,
		WithHTTP2ListenerConnAcceptTimeout(1*time.Second),
		WithHTTP2ListenerServerShutdownTimeout(1*time.Second),
		WithHTTP2ListenerMaxPendingConns(1),
		WithHTTP2ListenerReadHeaderTimeout(1*time.Second),
		WithHTTP2ListenerLogger(log.NewNopLogger()),
	)
	require.NoError(t, err)
	return listener
}
