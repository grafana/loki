package indexgateway

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/kv"
	"github.com/grafana/dskit/kv/consul"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/services"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/grafana/loki/v3/pkg/distributor/clientpool"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/storage/stores/series/index"
	"github.com/grafana/loki/v3/pkg/util/constants"
	"github.com/grafana/loki/v3/pkg/validation"
)

// const (
// the number of index entries for benchmarking will be divided amongst numTables
// benchMarkNumEntries = 1000000
// numTables           = 50
// )

type mockIndexGatewayServer struct {
	logproto.IndexGatewayServer
}

func (m mockIndexGatewayServer) QueryIndex(request *logproto.QueryIndexRequest, server logproto.IndexGateway_QueryIndexServer) error {
	for i, query := range request.Queries {
		resp := logproto.QueryIndexResponse{
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
			resp.Rows = append(resp.Rows, &logproto.Row{
				RangeValue: []byte(fmt.Sprintf("%s%d", rangeValuePrefix, j)),
				Value:      []byte(fmt.Sprintf("%s%d", valuePrefix, j)),
			})
		}

		resp.QueryKey = index.QueryKey(index.Query{
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

func (m mockIndexGatewayServer) GetChunkRef(context.Context, *logproto.GetChunkRefRequest) (*logproto.GetChunkRefResponse, error) {
	return &logproto.GetChunkRefResponse{}, nil
}

func createTestGrpcServer(t *testing.T) (func(), string) {
	var server mockIndexGatewayServer
	lis, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err)
	s := grpc.NewServer()

	logproto.RegisterIndexGatewayServer(s, &server)
	go func() {
		if err := s.Serve(lis); err != nil {
			t.Logf("Failed to serve: %v", err)
		}
	}()

	return s.GracefulStop, lis.Addr().String()
}

type mockTenantLimits map[string]*validation.Limits

func (tl mockTenantLimits) TenantLimits(userID string) *validation.Limits {
	return tl[userID]
}

func (tl mockTenantLimits) AllByUserID() map[string]*validation.Limits {
	return tl
}

func TestGatewayClient_RingMode(t *testing.T) {
	// prepare servers and ring
	logger := log.NewNopLogger()
	ringKey := "test"
	n := 6  // nuber of index gateway instances
	rf := 1 // replication factor
	s := 3  // shard size

	nodes := make([]*mockIndexGatewayServer, n)
	for i := 0; i < n; i++ {
		nodes[i] = &mockIndexGatewayServer{}
	}

	nodeDescs := map[string]ring.InstanceDesc{}

	for i := range nodes {
		addr := fmt.Sprintf("index-gateway-%d", i)
		nodeDescs[addr] = ring.InstanceDesc{
			Addr:                addr,
			State:               ring.ACTIVE,
			Timestamp:           time.Now().Unix(),
			RegisteredTimestamp: time.Now().Add(-10 * time.Minute).Unix(),
			Tokens:              []uint32{uint32((math.MaxUint32 / n) * i)},
		}
	}

	kvStore, closer := consul.NewInMemoryClient(ring.GetCodec(), logger, nil)
	t.Cleanup(func() { closer.Close() })

	err := kvStore.CAS(context.Background(), ringKey,
		func(_ interface{}) (interface{}, bool, error) {
			return &ring.Desc{
				Ingesters: nodeDescs,
			}, true, nil
		},
	)
	require.NoError(t, err)

	ringCfg := ring.Config{
		KVStore: kv.Config{
			Mock: kvStore,
		},
		HeartbeatTimeout:     time.Hour,
		ZoneAwarenessEnabled: false,
		ReplicationFactor:    rf,
	}

	igwRing, err := ring.New(ringCfg, "indexgateway", ringKey, logger, nil)
	require.NoError(t, err)
	require.NoError(t, services.StartAndAwaitRunning(context.Background(), igwRing))
	require.Eventually(t, func() bool {
		return igwRing.InstancesCount() == n
	}, time.Minute, time.Second)

	t.Cleanup(func() {
		igwRing.StopAsync()
	})

	t.Run("global shard size", func(t *testing.T) {
		o, err := validation.NewOverrides(validation.Limits{IndexGatewayShardSize: s}, nil)
		require.NoError(t, err)

		cfg := ClientConfig{}
		flagext.DefaultValues(&cfg)
		cfg.Mode = RingMode
		cfg.Ring = igwRing

		c, err := NewGatewayClient(cfg, nil, o, logger, constants.Loki)
		require.NoError(t, err)
		require.NotNil(t, c)

		// Shuffle sharding is deterministic
		// The same tenant ID gets the same servers assigned every time

		addrs, err := c.getServerAddresses("12345")
		require.NoError(t, err)
		require.Len(t, addrs, s)
		require.ElementsMatch(t, addrs, []string{"index-gateway-0", "index-gateway-3", "index-gateway-5"})

		addrs, err = c.getServerAddresses("67890")
		require.NoError(t, err)
		require.Len(t, addrs, s)
		require.ElementsMatch(t, addrs, []string{"index-gateway-2", "index-gateway-3", "index-gateway-5"})
	})

	t.Run("per tenant shard size", func(t *testing.T) {
		tl := mockTenantLimits{
			"12345": &validation.Limits{IndexGatewayShardSize: 1},
			// tenant 67890 has not tenant specific overrides
		}
		o, err := validation.NewOverrides(validation.Limits{IndexGatewayShardSize: s}, tl)
		require.NoError(t, err)

		cfg := ClientConfig{}
		flagext.DefaultValues(&cfg)
		cfg.Mode = RingMode
		cfg.Ring = igwRing

		c, err := NewGatewayClient(cfg, nil, o, logger, constants.Loki)
		require.NoError(t, err)
		require.NotNil(t, c)

		// Shuffle sharding is deterministic
		// The same tenant ID gets the same servers assigned every time

		addrs, err := c.getServerAddresses("12345")
		require.NoError(t, err)
		require.Len(t, addrs, 1)
		require.ElementsMatch(t, addrs, []string{"index-gateway-3"})

		addrs, err = c.getServerAddresses("67890")
		require.NoError(t, err)
		require.Len(t, addrs, s)
		require.ElementsMatch(t, addrs, []string{"index-gateway-2", "index-gateway-3", "index-gateway-5"})
	})
}

func TestGatewayClient(t *testing.T) {
	logger := log.NewNopLogger()
	cleanup, storeAddress := createTestGrpcServer(t)
	t.Cleanup(cleanup)

	var cfg ClientConfig
	cfg.Mode = SimpleMode
	flagext.DefaultValues(&cfg)
	cfg.Address = storeAddress
	cfg.PoolConfig = clientpool.PoolConfig{ClientCleanupPeriod: 500 * time.Millisecond}

	overrides, _ := validation.NewOverrides(validation.Limits{}, nil)
	gatewayClient, err := NewGatewayClient(cfg, prometheus.DefaultRegisterer, overrides, logger, constants.Loki)
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
	err = gatewayClient.QueryPages(ctx, queries, func(_ index.Query, batch index.ReadBatchResult) (shouldContinue bool) {
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

/*
ToDo(Sandeep): Comment out benchmark code for now to fix circular dependency
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
	logproto.RegisterIndexGatewayServer(s, gw)
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
	gatewayClient.grpcClient = logproto.NewIndexGatewayClient(conn)

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
}*/

func TestDoubleRegistration(t *testing.T) {
	logger := log.NewNopLogger()
	r := prometheus.NewRegistry()
	o, _ := validation.NewOverrides(validation.Limits{}, nil)

	clientCfg := ClientConfig{
		Address: "my-store-address:1234",
	}

	client, err := NewGatewayClient(clientCfg, r, o, logger, constants.Loki)
	require.NoError(t, err)
	defer client.Stop()

	client, err = NewGatewayClient(clientCfg, r, o, logger, constants.Loki)
	require.NoError(t, err)
	defer client.Stop()
}
