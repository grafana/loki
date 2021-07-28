package shipper

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"testing"

	"github.com/cortexproject/cortex/pkg/util/flagext"

	"github.com/stretchr/testify/require"
	"github.com/weaveworks/common/user"
	"google.golang.org/grpc"

	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/stores/shipper/indexgateway/indexgatewaypb"
	"github.com/grafana/loki/pkg/storage/stores/shipper/util"
)

const (
	// query prefixes
	tableNamePrefix        = "table-name"
	hashValuePrefix        = "hash-value"
	rangeValuePrefixPrefix = "range-value-prefix"
	rangeValueStartPrefix  = "range-value-start"
	valueEqualPrefix       = "value-equal"

	// response prefixes
	rangeValuePrefix = "range-value"
	valuePrefix      = "value"
)

type mockIndexGatewayServer struct{}

func (m mockIndexGatewayServer) QueryIndex(request *indexgatewaypb.QueryIndexRequest, server indexgatewaypb.IndexGateway_QueryIndexServer) error {
	for i, query := range request.Queries {
		resp := indexgatewaypb.QueryIndexResponse{
			QueryKey: "",
			Rows:     nil,
		}

		if query.TableName != fmt.Sprintf("%s%d", tableNamePrefix, i) {
			return errors.New("incorrect TableName in query")
		}
		if query.HashValue != fmt.Sprintf("%s%d", hashValuePrefix, i) {
			return errors.New("incorrect HashValue in query")
		}
		if string(query.RangeValuePrefix) != fmt.Sprintf("%s%d", rangeValuePrefixPrefix, i) {
			return errors.New("incorrect RangeValuePrefix in query")
		}
		if string(query.RangeValueStart) != fmt.Sprintf("%s%d", rangeValueStartPrefix, i) {
			return errors.New("incorrect RangeValueStart in query")
		}
		if string(query.ValueEqual) != fmt.Sprintf("%s%d", valueEqualPrefix, i) {
			return errors.New("incorrect ValueEqual in query")
		}

		for j := 0; j <= i; j++ {
			resp.Rows = append(resp.Rows, &indexgatewaypb.Row{
				RangeValue: []byte(fmt.Sprintf("%s%d", rangeValuePrefix, j)),
				Value:      []byte(fmt.Sprintf("%s%d", valuePrefix, j)),
			})
		}

		resp.QueryKey = util.QueryKey(chunk.IndexQuery{
			TableName:        query.TableName,
			HashValue:        query.HashValue,
			RangeValuePrefix: query.RangeValuePrefix,
			RangeValueStart:  query.RangeValueStart,
			ValueEqual:       query.ValueEqual,
		})

		if err := server.Send(&resp); err != nil {
			return err
		}
	}

	return nil
}

func createTestGrpcServer(t *testing.T) (func(), string) {
	var server mockIndexGatewayServer
	lis, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err)
	s := grpc.NewServer()

	indexgatewaypb.RegisterIndexGatewayServer(s, &server)
	go func() {
		if err := s.Serve(lis); err != nil {
			log.Fatalf("Failed to serve: %v", err)
		}
	}()
	cleanup := func() {
		s.GracefulStop()
	}

	return cleanup, lis.Addr().String()
}

func TestGatewayClient(t *testing.T) {
	cleanup, storeAddress := createTestGrpcServer(t)
	defer cleanup()

	var cfg IndexGatewayClientConfig
	flagext.DefaultValues(&cfg)
	cfg.Address = storeAddress

	gatewayClient, err := NewGatewayClient(cfg, nil)
	require.NoError(t, err)

	ctx := user.InjectOrgID(context.Background(), "fake")

	queries := []chunk.IndexQuery{}
	for i := 0; i < 10; i++ {
		queries = append(queries, chunk.IndexQuery{
			TableName:        fmt.Sprintf("%s%d", tableNamePrefix, i),
			HashValue:        fmt.Sprintf("%s%d", hashValuePrefix, i),
			RangeValuePrefix: []byte(fmt.Sprintf("%s%d", rangeValuePrefixPrefix, i)),
			RangeValueStart:  []byte(fmt.Sprintf("%s%d", rangeValueStartPrefix, i)),
			ValueEqual:       []byte(fmt.Sprintf("%s%d", valueEqualPrefix, i)),
		})
	}

	numCallbacks := 0
	err = gatewayClient.QueryPages(ctx, queries, func(query chunk.IndexQuery, batch chunk.ReadBatch) (shouldContinue bool) {
		itr := batch.Iterator()

		for j := 0; j <= numCallbacks; j++ {
			require.True(t, itr.Next())
			require.Equal(t, fmt.Sprintf("%s%d", rangeValuePrefix, j), string(itr.RangeValue()))
			require.Equal(t, fmt.Sprintf("%s%d", valuePrefix, j), string(itr.Value()))
		}

		require.False(t, itr.Next())
		numCallbacks++
		return true
	})
	require.NoError(t, err)

	require.Equal(t, len(queries), numCallbacks)
}
