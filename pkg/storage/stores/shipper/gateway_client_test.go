package shipper

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/grafana/dskit/flagext"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"github.com/weaveworks/common/middleware"
	"github.com/weaveworks/common/user"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	"github.com/grafana/loki/pkg/storage/chunk/client/local"
	"github.com/grafana/loki/pkg/storage/stores/series/index"
	"github.com/grafana/loki/pkg/storage/stores/shipper/downloads"
	"github.com/grafana/loki/pkg/storage/stores/shipper/indexgateway"
	"github.com/grafana/loki/pkg/storage/stores/shipper/indexgateway/indexgatewaypb"
	"github.com/grafana/loki/pkg/storage/stores/shipper/storage"
	"github.com/grafana/loki/pkg/storage/stores/shipper/testutil"
	"github.com/grafana/loki/pkg/storage/stores/shipper/util"
	util_log "github.com/grafana/loki/pkg/util/log"
	util_math "github.com/grafana/loki/pkg/util/math"
	"github.com/grafana/loki/pkg/validation"
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

	// the number of index entries for benchmarking will be divided amongst numTables
	benchMarkNumEntries = 1000000
	numTables           = 50
)

type mockIndexGatewayServer struct {
	indexgatewaypb.IndexGatewayServer
}

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

		resp.QueryKey = util.QueryKey(index.Query{
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

func (m mockIndexGatewayServer) GetChunkRef(context.Context, *indexgatewaypb.GetChunkRefRequest) (*indexgatewaypb.GetChunkRefResponse, error) {
	return &indexgatewaypb.GetChunkRefResponse{}, nil
}

func createTestGrpcServer(t *testing.T) (func(), string) {
	var server mockIndexGatewayServer
	lis, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err)
	s := grpc.NewServer()

	indexgatewaypb.RegisterIndexGatewayServer(s, &server)
	go func() {
		if err := s.Serve(lis); err != nil {
			t.Logf("Failed to serve: %v", err)
		}
	}()

	return s.GracefulStop, lis.Addr().String()
}

func TestGatewayClient(t *testing.T) {
	cleanup, storeAddress := createTestGrpcServer(t)
	t.Cleanup(cleanup)

	var cfg IndexGatewayClientConfig
	cfg.Mode = indexgateway.SimpleMode
	flagext.DefaultValues(&cfg)
	cfg.Address = storeAddress

	gatewayClient, err := NewGatewayClient(cfg, prometheus.DefaultRegisterer, util_log.Logger)
	require.NoError(t, err)

	ctx := user.InjectOrgID(context.Background(), "fake")

	queries := []index.Query{}
	for i := 0; i < 10; i++ {
		queries = append(queries, index.Query{
			TableName:        fmt.Sprintf("%s%d", tableNamePrefix, i),
			HashValue:        fmt.Sprintf("%s%d", hashValuePrefix, i),
			RangeValuePrefix: []byte(fmt.Sprintf("%s%d", rangeValuePrefixPrefix, i)),
			RangeValueStart:  []byte(fmt.Sprintf("%s%d", rangeValueStartPrefix, i)),
			ValueEqual:       []byte(fmt.Sprintf("%s%d", valueEqualPrefix, i)),
		})
	}

	numCallbacks := 0
	err = gatewayClient.QueryPages(ctx, queries, func(query index.Query, batch index.ReadBatchResult) (shouldContinue bool) {
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

func buildTableName(i int) string {
	return fmt.Sprintf("%s%d", tableNamePrefix, i)
}

type mockLimits struct{}

func (m mockLimits) AllByUserID() map[string]*validation.Limits {
	return map[string]*validation.Limits{}
}

func (m mockLimits) DefaultLimits() *validation.Limits {
	return &validation.Limits{}
}

func benchmarkIndexQueries(b *testing.B, queries []index.Query) {
	buffer := 1024 * 1024
	listener := bufconn.Listen(buffer)

	// setup the grpc server
	s := grpc.NewServer(grpc.ChainStreamInterceptor(func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		return middleware.StreamServerUserHeaderInterceptor(srv, ss, info, handler)
	}))
	conn, _ := grpc.DialContext(context.Background(), "", grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
		return listener.Dial()
	}), grpc.WithTransportCredentials(insecure.NewCredentials()))
	defer func() {
		s.Stop()
		conn.Close()
	}()

	// setup test data
	dir := b.TempDir()
	bclient, err := local.NewBoltDBIndexClient(local.BoltDBConfig{
		Directory: dir + "/boltdb",
	})
	require.NoError(b, err)

	for i := 0; i < numTables; i++ {
		// setup directory for table in both cache and object storage
		tableName := buildTableName(i)
		objectStorageDir := filepath.Join(dir, "index", tableName)
		cacheDir := filepath.Join(dir, "cache", tableName)
		require.NoError(b, os.MkdirAll(objectStorageDir, 0o777))
		require.NoError(b, os.MkdirAll(cacheDir, 0o777))

		// add few rows at a time to the db because doing to many writes in a single transaction puts too much strain on boltdb and makes it slow
		for i := 0; i < benchMarkNumEntries/numTables; i += 10000 {
			end := util_math.Min(i+10000, benchMarkNumEntries/numTables)
			// setup index files in both the cache directory and object storage directory so that we don't spend time syncing files at query time
			testutil.AddRecordsToDB(b, filepath.Join(objectStorageDir, "db1"), bclient, i, end-i, []byte("index"))
			testutil.AddRecordsToDB(b, filepath.Join(cacheDir, "db1"), bclient, i, end-i, []byte("index"))
		}
	}

	fs, err := local.NewFSObjectClient(local.FSConfig{
		Directory: dir,
	})
	require.NoError(b, err)
	tm, err := downloads.NewTableManager(downloads.Config{
		CacheDir:          dir + "/cache",
		SyncInterval:      15 * time.Minute,
		CacheTTL:          15 * time.Minute,
		QueryReadyNumDays: 30,
		Limits:            mockLimits{},
	}, bclient, storage.NewIndexStorageClient(fs, "index/"), nil, nil)
	require.NoError(b, err)

	// initialize the index gateway server
	var cfg indexgateway.Config
	flagext.DefaultValues(&cfg)

	gw, err := indexgateway.NewIndexGateway(cfg, util_log.Logger, prometheus.DefaultRegisterer, nil, tm)
	require.NoError(b, err)
	indexgatewaypb.RegisterIndexGatewayServer(s, gw)
	go func() {
		if err := s.Serve(listener); err != nil {
			panic(err)
		}
	}()

	// setup context for querying
	ctx := user.InjectOrgID(context.Background(), "foo")
	ctx, _ = user.InjectIntoGRPCRequest(ctx)

	// initialize the gateway client
	gatewayClient := GatewayClient{}
	gatewayClient.grpcClient = indexgatewaypb.NewIndexGatewayClient(conn)

	// build the response we expect to get from queries
	expected := map[string]int{}
	for i := 0; i < benchMarkNumEntries/numTables; i++ {
		expected[strconv.Itoa(i)] = numTables
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		actual := map[string]int{}
		syncMtx := sync.Mutex{}

		err := gatewayClient.QueryPages(ctx, queries, func(query index.Query, batch index.ReadBatchResult) (shouldContinue bool) {
			itr := batch.Iterator()
			for itr.Next() {
				syncMtx.Lock()
				actual[string(itr.Value())]++
				syncMtx.Unlock()
			}
			return true
		})
		require.NoError(b, err)
		require.Equal(b, expected, actual)
	}
}

func Benchmark_QueriesMatchingSingleRow(b *testing.B) {
	queries := []index.Query{}
	// do a query per row from each of the tables
	for i := 0; i < benchMarkNumEntries/numTables; i++ {
		for j := 0; j < numTables; j++ {
			queries = append(queries, index.Query{
				TableName:        buildTableName(j),
				RangeValuePrefix: []byte(strconv.Itoa(i)),
				ValueEqual:       []byte(strconv.Itoa(i)),
			})
		}
	}

	benchmarkIndexQueries(b, queries)
}

func Benchmark_QueriesMatchingLargeNumOfRows(b *testing.B) {
	var queries []index.Query
	// do a query per table matching all the rows from it
	for j := 0; j < numTables; j++ {
		queries = append(queries, index.Query{
			TableName: buildTableName(j),
		})
	}
	benchmarkIndexQueries(b, queries)
}

func TestDoubleRegistration(t *testing.T) {
	r := prometheus.NewRegistry()
	cleanup, storeAddress := createTestGrpcServer(t)
	t.Cleanup(cleanup)

	_, err := NewGatewayClient(IndexGatewayClientConfig{
		Address: storeAddress,
	}, r, util_log.Logger)
	require.NoError(t, err)
	_, err = NewGatewayClient(IndexGatewayClientConfig{
		Address: storeAddress,
	}, r, util_log.Logger)
	require.NoError(t, err)
}
