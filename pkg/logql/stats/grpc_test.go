package stats

import (
	"context"
	"io"
	"log"
	"net"
	"testing"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/test/bufconn"
)

const bufSize = 1024 * 1024

var lis *bufconn.Listener
var server *grpc.Server

func init() {
	lis = bufconn.Listen(bufSize)
	server = grpc.NewServer()
}

func bufDialer(context.Context, string) (net.Conn, error) {
	return lis.Dial()
}

func TestCollectTrailer(t *testing.T) {
	ctx := context.Background()
	conn, err := grpc.DialContext(ctx, "bufnet", grpc.WithContextDialer(bufDialer), grpc.WithInsecure())
	if err != nil {
		t.Fatalf("Failed to dial bufnet: %v", err)
	}
	defer conn.Close()
	ing := ingesterFn(func(req *logproto.QueryRequest, s logproto.Querier_QueryServer) error {
		ingCtx := NewContext(s.Context())
		defer SendAsTrailer(ingCtx, s)
		GetIngesterData(ingCtx).TotalChunksMatched++
		GetIngesterData(ingCtx).TotalBatches = +2
		GetIngesterData(ingCtx).TotalLinesSent = +3
		GetChunkData(ingCtx).BytesUncompressed++
		GetChunkData(ingCtx).LinesUncompressed++
		GetChunkData(ingCtx).BytesDecompressed++
		GetChunkData(ingCtx).LinesDecompressed++
		GetChunkData(ingCtx).BytesCompressed++
		GetChunkData(ingCtx).TotalDuplicates++
		return nil
	})
	logproto.RegisterQuerierServer(server, ing)
	go func() {
		if err := server.Serve(lis); err != nil {
			log.Fatalf("Server exited with error: %v", err)
		}
	}()

	ingClient := logproto.NewQuerierClient(conn)

	ctx = NewContext(ctx)

	// query the ingester twice.
	clientStream, err := ingClient.Query(ctx, &logproto.QueryRequest{}, CollectTrailer(ctx))
	if err != nil {
		t.Fatal(err)
	}
	_, err = clientStream.Recv()
	if err != nil && err != io.EOF {
		t.Fatal(err)
	}
	clientStream, err = ingClient.Query(ctx, &logproto.QueryRequest{}, CollectTrailer(ctx))
	if err != nil {
		t.Fatal(err)
	}
	_, err = clientStream.Recv()
	if err != nil && err != io.EOF {
		t.Fatal(err)
	}
	err = clientStream.CloseSend()
	if err != nil {
		t.Fatal(err)
	}
	res := decodeTrailers(ctx)
	require.Equal(t, 2, res.TotalReached)
	require.Equal(t, int64(2), res.TotalChunksMatched)
	require.Equal(t, int64(4), res.TotalBatches)
	require.Equal(t, int64(6), res.TotalLinesSent)
	require.Equal(t, int64(2), res.BytesUncompressed)
	require.Equal(t, int64(2), res.LinesUncompressed)
	require.Equal(t, int64(2), res.BytesDecompressed)
	require.Equal(t, int64(2), res.LinesDecompressed)
	require.Equal(t, int64(2), res.BytesCompressed)
	require.Equal(t, int64(2), res.TotalDuplicates)
}

type ingesterFn func(*logproto.QueryRequest, logproto.Querier_QueryServer) error

func (i ingesterFn) Query(req *logproto.QueryRequest, s logproto.Querier_QueryServer) error {
	return i(req, s)
}
func (ingesterFn) Label(context.Context, *logproto.LabelRequest) (*logproto.LabelResponse, error) {
	return nil, nil
}
func (ingesterFn) Tail(*logproto.TailRequest, logproto.Querier_TailServer) error { return nil }
func (ingesterFn) Series(context.Context, *logproto.SeriesRequest) (*logproto.SeriesResponse, error) {
	return nil, nil
}
