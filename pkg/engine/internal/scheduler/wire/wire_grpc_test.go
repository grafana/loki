package wire_test

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/go-kit/log"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/proto/wirepb"
	"github.com/grafana/loki/v3/pkg/engine/internal/scheduler/wire"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/engine/internal/util/dag"
	"github.com/grafana/loki/v3/pkg/engine/internal/workflow"
)

// prepareGRPCListener starts a real gRPC server with GRPCListener registered,
// returns the listener and a shutdown func.
//
// Note: the test gRPC server uses grpc.MaxRecvMsgSize(wireMaxGRPCMessageBytes)
// to allow large Arrow payloads in TestGRPCLargeStreamDataMessage.  The
// production server must be configured identically via
// server.grpc_server_max_recv_msg_size (see wireMaxGRPCMessageBytes FIXME).
func prepareGRPCListener(t *testing.T) (*wire.GRPCListener, func()) {
	t.Helper()

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	addr := lis.Addr()
	grpcLis := wire.NewGRPCListener(addr, wire.WithGRPCListenerLogger(log.NewNopLogger()))

	// 256 MiB to match wireMaxGRPCMessageBytes on the client side.
	const maxMsgSize = 256 * 1024 * 1024
	srv := grpc.NewServer(
		grpc.MaxRecvMsgSize(maxMsgSize),
		grpc.MaxSendMsgSize(maxMsgSize),
	)
	wirepb.RegisterWireServiceServer(srv, grpcLis)

	var wg sync.WaitGroup
	wg.Go(func() {
		_ = srv.Serve(lis)
	})

	return grpcLis, func() {
		require.NoError(t, grpcLis.Close(context.Background()))
		srv.GracefulStop()
		wg.Wait()
	}
}

// dialAndAccept dials the gRPCListener and returns the client and server Conns.
// It fails the test if either operation fails.
func dialAndAccept(ctx context.Context, t *testing.T, listener *wire.GRPCListener) (clientConn, serverConn wire.Conn) {
	t.Helper()

	type acceptResult struct {
		conn wire.Conn
		err  error
	}
	acceptCh := make(chan acceptResult, 1)
	go func() {
		conn, err := listener.Accept(ctx)
		acceptCh <- acceptResult{conn, err}
	}()

	var err error
	clientConn, err = wire.NewGRPCDialer().Dial(ctx, listener.Addr(), listener.Addr())
	require.NoError(t, err)

	res := <-acceptCh
	require.NoError(t, res.err)
	serverConn = res.conn
	return clientConn, serverConn
}

// TestGRPCBasicConnectivity verifies basic connection establishment and
// bidirectional frame exchange.
func TestGRPCBasicConnectivity(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	listener, shutdown := prepareGRPCListener(t)
	defer shutdown()

	clientConn, serverConn := dialAndAccept(ctx, t, listener)
	defer clientConn.Close()
	defer serverConn.Close()

	// Client → Server
	want := wire.AckFrame{ID: 42}
	require.NoError(t, clientConn.Send(ctx, want))
	got, err := serverConn.Recv(ctx)
	require.NoError(t, err)
	require.Equal(t, want, got)

	// Server → Client
	reply := wire.AckFrame{ID: 43}
	require.NoError(t, serverConn.Send(ctx, reply))
	got, err = clientConn.Recv(ctx)
	require.NoError(t, err)
	require.Equal(t, reply, got)
}

// TestGRPCMetadataHandshake verifies that serverConn.RemoteAddr() equals
// the from address passed to Dial.
func TestGRPCMetadataHandshake(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	listener, shutdown := prepareGRPCListener(t)
	defer shutdown()

	// Use a distinct "from" address so the assertion is meaningful.
	from := &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 19876}
	to := listener.Addr()

	acceptCh := make(chan wire.Conn, 1)
	go func() {
		conn, err := listener.Accept(ctx)
		if err == nil {
			acceptCh <- conn
		}
	}()

	clientConn, err := wire.NewGRPCDialer().Dial(ctx, from, to)
	require.NoError(t, err)
	defer clientConn.Close()

	var serverConn wire.Conn
	select {
	case serverConn = <-acceptCh:
	case <-ctx.Done():
		t.Fatal("timeout waiting for Accept")
	}
	defer serverConn.Close()

	// The server conn's RemoteAddr must equal the 'from' address we dialled with.
	require.Equal(t, from.String(), serverConn.RemoteAddr().String(),
		"server conn RemoteAddr should equal the from address passed to Dial")
}

// TestGRPCMissingMetadataRejected verifies the server rejects connections
// that omit the "wire-peer-addr" metadata key.
func TestGRPCMissingMetadataRejected(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	listener, shutdown := prepareGRPCListener(t)
	defer shutdown()

	// Open a raw gRPC connection deliberately omitting the "wire-peer-addr" metadata.
	cc, err := grpc.NewClient(
		listener.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	defer cc.Close()

	// No metadata on the context — the server handler will reject with InvalidArgument.
	stream, err := wirepb.NewWireServiceClient(cc).Pipe(ctx)
	require.NoError(t, err, "opening the stream should succeed; rejection surfaces on first Recv")

	// The server immediately returns an error because metadata is missing.
	// Recv propagates that error to the client.
	_, err = stream.Recv()
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err),
		"server should reject with InvalidArgument when wire-peer-addr metadata is absent")
}

// TestGRPCLargeStreamDataMessage verifies that a StreamDataMessage with
// an Arrow payload near wireMaxGRPCMessageBytes/2 round-trips successfully
// with the client-side call options applied.
//
// The test server is configured with grpc.MaxRecvMsgSize(256 MiB) to mirror
// the production requirement (see wireMaxGRPCMessageBytes FIXME comment).
func TestGRPCLargeStreamDataMessage(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	listener, shutdown := prepareGRPCListener(t)
	defer shutdown()

	clientConn, serverConn := dialAndAccept(ctx, t, listener)
	defer clientConn.Close()
	defer serverConn.Close()

	// Build a ~32 MB Arrow record batch (int64 column with 4 million rows).
	// Serialised via Arrow IPC this is ~32 MB, well above gRPC's 4 MB default.
	const numRows = 4_000_000
	largeRecord := makeLargeArrowRecord(numRows)

	streamID := ulid.Make()
	msg := wire.MessageFrame{
		ID: 1,
		Message: wire.StreamDataMessage{
			StreamID: streamID,
			Data:     largeRecord,
		},
	}

	require.NoError(t, clientConn.Send(ctx, msg), "sending large StreamDataMessage must succeed with 256 MiB call options")

	received, err := serverConn.Recv(ctx)
	require.NoError(t, err)
	receivedFrame, ok := received.(wire.MessageFrame)
	require.True(t, ok)
	receivedMsg, ok := receivedFrame.Message.(wire.StreamDataMessage)
	require.True(t, ok)
	require.Equal(t, streamID, receivedMsg.StreamID)
	require.EqualValues(t, numRows, receivedMsg.Data.NumRows(),
		"deserialized record should have the same number of rows as the original")
}

// TestGRPCMultipleClients verifies three concurrent Dial→Accept→Send/Recv
// round-trips all succeed independently.
//
// The server echoes each received frame back to the client.  Clients verify
// they receive their own unique frame.  This design avoids any dependence on
// the order in which connections are accepted.
func TestGRPCMultipleClients(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	listener, shutdown := prepareGRPCListener(t)
	defer shutdown()

	const numClients = 3

	// Server: accept numClients connections and echo each received frame.
	go func() {
		for range numClients {
			conn, err := listener.Accept(ctx)
			if err != nil {
				return
			}
			go func(c wire.Conn) {
				defer c.Close()
				f, err := c.Recv(ctx)
				if err != nil {
					return
				}
				// Echo the frame back so the client can verify receipt.
				_ = c.Send(ctx, f)
			}(conn)
		}
	}()

	// Clients: each dials, sends a unique frame, and verifies the echo.
	var wg sync.WaitGroup
	for i := range numClients {
		wg.Go(func() {
			conn, err := wire.NewGRPCDialer().Dial(ctx, listener.Addr(), listener.Addr())
			require.NoError(t, err)
			defer conn.Close()

			sent := wire.AckFrame{ID: uint64(i + 1)}
			require.NoError(t, conn.Send(ctx, sent))

			received, err := conn.Recv(ctx)
			require.NoError(t, err)
			require.Equal(t, sent, received, "client %d: echo mismatch", i)
		})
	}
	wg.Wait()
}

// TestGRPCConnectionClose_ClientCloses verifies that after the client closes
// its Conn, the server's Recv returns ErrConnClosed.
func TestGRPCConnectionClose_ClientCloses(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	listener, shutdown := prepareGRPCListener(t)
	defer shutdown()

	clientConn, serverConn := dialAndAccept(ctx, t, listener)
	defer serverConn.Close()

	// Close the client side.
	require.NoError(t, clientConn.Close())

	// The server should see ErrConnClosed on the next Recv.
	_, err := serverConn.Recv(ctx)
	require.True(t, errors.Is(err, wire.ErrConnClosed),
		"expected ErrConnClosed after client close, got: %v", err)
}

// TestGRPCConnectionClose_ServerCloses verifies that after the server closes
// its Conn, the client's Recv returns ErrConnClosed.
func TestGRPCConnectionClose_ServerCloses(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	listener, shutdown := prepareGRPCListener(t)
	defer shutdown()

	clientConn, serverConn := dialAndAccept(ctx, t, listener)
	defer clientConn.Close()

	// Close the server side.
	require.NoError(t, serverConn.Close())

	// The client should see ErrConnClosed on the next Recv.
	_, err := clientConn.Recv(ctx)
	require.True(t, errors.Is(err, wire.ErrConnClosed),
		"expected ErrConnClosed after server close, got: %v", err)
}

// TestGRPCListenerClose_DropsNewConnections verifies that after the listener
// is closed, Accept returns net.ErrClosed.
func TestGRPCListenerClose_DropsNewConnections(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	listener, shutdown := prepareGRPCListener(t)
	defer shutdown()

	// Close the listener.
	require.NoError(t, listener.Close(ctx))

	// Accept should return net.ErrClosed immediately.
	_, err := listener.Accept(ctx)
	require.ErrorIs(t, err, net.ErrClosed)
}

// TestGRPCContextCancellation verifies that Recv with a cancelled context
// returns ctx.Err().
func TestGRPCContextCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	listener, shutdown := prepareGRPCListener(t)
	defer shutdown()

	clientConn, serverConn := dialAndAccept(ctx, t, listener)
	defer clientConn.Close()
	defer serverConn.Close()

	// Cancel the context used for Recv.
	recvCtx, recvCancel := context.WithCancel(ctx)
	recvCancel() // cancel immediately

	_, err := clientConn.Recv(recvCtx)
	require.ErrorIs(t, err, context.Canceled)
}

// TestGRPCAllMessageFrameTypes verifies each of the message frame types
// round-trips correctly over the gRPC transport.
func TestGRPCAllMessageFrameTypes(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	listener, shutdown := prepareGRPCListener(t)
	defer shutdown()

	clientConn, serverConn := dialAndAccept(ctx, t, listener)
	defer clientConn.Close()
	defer serverConn.Close()

	// Build a minimal TaskAssign message.
	plan := &physical.Plan{}
	scanSet := plan.Graph().Add(&physical.ScanSet{
		NodeID: ulid.Make(),
		Targets: []*physical.ScanTarget{
			{Type: physical.ScanTypeDataObject, DataObject: &physical.DataObjScan{NodeID: ulid.Make(), Location: "obj1", Section: 1}},
		},
	})
	parallelize := plan.Graph().Add(&physical.Parallelize{NodeID: ulid.Make()})
	compat := plan.Graph().Add(&physical.ColumnCompat{
		NodeID:      ulid.Make(),
		Source:      types.ColumnTypeMetadata,
		Destination: types.ColumnTypeMetadata,
		Collisions:  []types.ColumnType{types.ColumnTypeLabel},
	})
	_ = plan.Graph().AddEdge(dag.Edge[physical.Node]{Parent: parallelize, Child: compat})
	_ = plan.Graph().AddEdge(dag.Edge[physical.Node]{Parent: compat, Child: scanSet})

	receiverAddr := &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 20001}

	testMessages := []wire.Message{
		wire.WorkerHelloMessage{Threads: 4},
		wire.WorkerSubscribeMessage{},
		wire.WorkerReadyMessage{},
		wire.TaskAssignMessage{
			Task: &workflow.Task{
				ULID:     ulid.Make(),
				TenantID: "test-tenant",
				Fragment: plan,
				Sources:  map[physical.Node][]*workflow.Stream{},
				Sinks:    map[physical.Node][]*workflow.Stream{},
			},
		},
		wire.TaskCancelMessage{ID: ulid.Make()},
		wire.TaskFlagMessage{ID: ulid.Make(), Interruptible: true},
		wire.TaskStatusMessage{ID: ulid.Make(), Status: workflow.TaskStatus{}},
		wire.StreamBindMessage{StreamID: ulid.Make(), Receiver: receiverAddr},
		wire.StreamDataMessage{StreamID: ulid.Make(), Data: makeSmallArrowRecord()},
		wire.StreamStatusMessage{StreamID: ulid.Make(), State: workflow.StreamStateOpen},
	}

	for i, msg := range testMessages {
		t.Run(typeName(msg), func(t *testing.T) {
			frame := wire.MessageFrame{ID: uint64(i + 1), Message: msg}
			require.NoError(t, clientConn.Send(ctx, frame))

			received, err := serverConn.Recv(ctx)
			require.NoError(t, err)

			receivedFrame, ok := received.(wire.MessageFrame)
			require.True(t, ok)
			require.EqualValues(t, i+1, receivedFrame.ID)
			require.IsType(t, msg, receivedFrame.Message)
		})
	}
}

// TestGRPCWithPeers verifies two wire.Peer instances over a gRPC connection
// can exchange messages.
func TestGRPCWithPeers(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	listener, shutdown := prepareGRPCListener(t)
	defer shutdown()

	clientConn, serverConn := dialAndAccept(ctx, t, listener)
	defer clientConn.Close()
	defer serverConn.Close()

	serverReceived := make(chan wire.Message, 1)
	clientReceived := make(chan wire.Message, 1)

	serverHandler := func(ctx context.Context, peer *wire.Peer, message wire.Message) error {
		serverReceived <- message
		if _, ok := message.(wire.TaskStatusMessage); ok {
			return peer.SendMessageAsync(ctx, wire.WorkerReadyMessage{})
		}
		return nil
	}

	clientHandler := func(_ context.Context, _ *wire.Peer, message wire.Message) error {
		clientReceived <- message
		return nil
	}

	serverPeer := &wire.Peer{
		Logger:  log.NewNopLogger(),
		Metrics: wire.NewMetrics(),
		Conn:    serverConn,
		Handler: serverHandler,
		Buffer:  10,
	}
	clientPeer := &wire.Peer{
		Logger:  log.NewNopLogger(),
		Metrics: wire.NewMetrics(),
		Conn:    clientConn,
		Handler: clientHandler,
		Buffer:  10,
	}

	peerCtx, peerCancel := context.WithCancel(ctx)
	defer peerCancel()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); _ = serverPeer.Serve(peerCtx) }()
	go func() { defer wg.Done(); _ = clientPeer.Serve(peerCtx) }()

	// Send TaskStatusMessage from client to server; server echoes WorkerReadyMessage.
	require.NoError(t, clientPeer.SendMessage(ctx, wire.TaskStatusMessage{}))

	select {
	case msg := <-serverReceived:
		require.IsType(t, wire.TaskStatusMessage{}, msg)
	case <-ctx.Done():
		t.Fatal("timeout waiting for server to receive message")
	}

	select {
	case msg := <-clientReceived:
		require.IsType(t, wire.WorkerReadyMessage{}, msg)
	case <-ctx.Done():
		t.Fatal("timeout waiting for client to receive echo")
	}

	peerCancel()
	wg.Wait()
}

// TestGRPC_NoPanicOnWriteAfterClose verifies that concurrent sends after
// conn close do not panic.  100 goroutines each attempt 10,000 sends to the
// server conn while the client closes mid-flight.  All goroutines must exit
// cleanly with either no error or ErrConnClosed.
func TestGRPC_NoPanicOnWriteAfterClose(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	listener, shutdown := prepareGRPCListener(t)
	defer shutdown()

	clientConn, serverConn := dialAndAccept(ctx, t, listener)
	defer serverConn.Close()
	defer clientConn.Close()

	var wg sync.WaitGroup

	// One goroutine drains the server conn.
	wg.Go(func() {
		for {
			_, err := serverConn.Recv(ctx)
			if err != nil {
				if errors.Is(err, wire.ErrConnClosed) {
					return
				}
				if ctx.Err() != nil {
					return
				}
				require.NoError(t, err)
			}
		}
	})

	// 100 goroutines each try to send 10,000 frames to the server.
	for range 100 {
		wg.Go(func() {
			for range 10_000 {
				err := clientConn.Send(ctx, wire.AckFrame{ID: 1})
				if err != nil {
					if errors.Is(err, wire.ErrConnClosed) {
						return
					}
					if ctx.Err() != nil {
						return
					}
					require.NoError(t, err)
				}
			}
		})
	}

	// Close the client connection while senders are running.
	wg.Go(func() {
		_ = clientConn.Close()
	})

	wg.Wait()
}

// --- helpers ---

// makeLargeArrowRecord creates an Arrow RecordBatch with a single int64 column
// containing numRows rows.  At 8 bytes per int64, 4 million rows ≈ 32 MB of
// column data in Arrow IPC format, well above gRPC's default 4 MB limit.
func makeLargeArrowRecord(numRows int) arrow.RecordBatch {
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "value", Type: arrow.PrimitiveTypes.Int64, Nullable: false},
	}, nil)
	builder := array.NewInt64Builder(memory.DefaultAllocator)
	for i := range numRows {
		builder.Append(int64(i))
	}
	col := builder.NewArray()
	return array.NewRecordBatch(schema, []arrow.Array{col}, int64(numRows))
}

// makeSmallArrowRecord creates a tiny Arrow RecordBatch suitable for use in
// round-trip tests where message size is not the focus.
func makeSmallArrowRecord() arrow.RecordBatch {
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: false},
	}, nil)
	builder := array.NewInt64Builder(memory.DefaultAllocator)
	builder.Append(1)
	builder.Append(2)
	col := builder.NewArray()
	return array.NewRecordBatch(schema, []arrow.Array{col}, 2)
}

// typeName returns a short name for the given Message, suitable for use as a
// test sub-test name.
func typeName(m wire.Message) string {
	switch m.(type) {
	case wire.WorkerHelloMessage:
		return "WorkerHello"
	case wire.WorkerSubscribeMessage:
		return "WorkerSubscribe"
	case wire.WorkerReadyMessage:
		return "WorkerReady"
	case wire.TaskAssignMessage:
		return "TaskAssign"
	case wire.TaskCancelMessage:
		return "TaskCancel"
	case wire.TaskFlagMessage:
		return "TaskFlag"
	case wire.TaskStatusMessage:
		return "TaskStatus"
	case wire.StreamBindMessage:
		return "StreamBind"
	case wire.StreamDataMessage:
		return "StreamData"
	case wire.StreamStatusMessage:
		return "StreamStatus"
	default:
		return "Unknown"
	}
}
