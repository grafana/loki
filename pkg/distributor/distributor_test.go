package distributor

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/kv"
	"github.com/grafana/dskit/kv/consul"
	"github.com/grafana/dskit/ring"
	ring_client "github.com/grafana/dskit/ring/client"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/weaveworks/common/httpgrpc"
	"github.com/weaveworks/common/user"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/grafana/loki/pkg/ingester"
	"github.com/grafana/loki/pkg/ingester/client"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/runtime"
	fe "github.com/grafana/loki/pkg/util/flagext"
	loki_net "github.com/grafana/loki/pkg/util/net"
	"github.com/grafana/loki/pkg/util/test"
	"github.com/grafana/loki/pkg/validation"
)

var (
	success = &logproto.PushResponse{}
	ctx     = user.InjectOrgID(context.Background(), "test")
)

func TestDistributor(t *testing.T) {
	ingestionRateLimit := 0.000096 // 100 Bytes/s limit

	for i, tc := range []struct {
		lines            int
		maxLineSize      uint64
		mangleLabels     bool
		expectedResponse *logproto.PushResponse
		expectedError    error
	}{
		{
			lines:            10,
			expectedResponse: success,
		},
		{
			lines:         100,
			expectedError: httpgrpc.Errorf(http.StatusTooManyRequests, validation.RateLimitedErrorMsg, "test", 100, 100, 1000),
		},
		{
			lines:            100,
			maxLineSize:      1,
			expectedResponse: success,
			expectedError:    httpgrpc.Errorf(http.StatusBadRequest, validation.LineTooLongErrorMsg, 1, "{foo=\"bar\"}", 10),
		},
		{
			lines:            100,
			mangleLabels:     true,
			expectedResponse: success,
			expectedError:    httpgrpc.Errorf(http.StatusBadRequest, validation.InvalidLabelsErrorMsg, "{ab\"", "1:4: parse error: unterminated quoted string"),
		},
	} {
		t.Run(fmt.Sprintf("[%d](lines=%v)", i, tc.lines), func(t *testing.T) {
			limits := &validation.Limits{}
			flagext.DefaultValues(limits)
			limits.EnforceMetricName = false
			limits.IngestionRateMB = ingestionRateLimit
			limits.IngestionBurstSizeMB = ingestionRateLimit
			limits.MaxLineSize = fe.ByteSize(tc.maxLineSize)

			distributors, _ := prepare(t, 1, 5, limits, nil)

			request := makeWriteRequest(tc.lines, 10)

			if tc.mangleLabels {
				request.Streams[0].Labels = `{ab"`
			}

			response, err := distributors[i%len(distributors)].Push(ctx, request)
			assert.Equal(t, tc.expectedResponse, response)
			assert.Equal(t, tc.expectedError, err)
		})
	}
}

func TestDistributorPushConcurrently(t *testing.T) {
	limits := &validation.Limits{}
	flagext.DefaultValues(limits)

	distributors, ingesters := prepare(t, 1, 5, limits, nil)

	numReq := 1
	var wg sync.WaitGroup
	for i := 0; i < numReq; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			request := makeWriteRequestWithLabels(100, 100,
				[]string{
					fmt.Sprintf(`{app="foo-%d"}`, n),
					fmt.Sprintf(`{instance="bar-%d"}`, n),
				},
			)
			response, err := distributors[n%len(distributors)].Push(ctx, request)
			assert.NoError(t, err)
			assert.Equal(t, &logproto.PushResponse{}, response)
		}(i)
	}

	wg.Wait()
	// make sure the ingesters received the push requests
	time.Sleep(10 * time.Millisecond)

	counter := 0
	labels := make(map[string]int)

	for i := range ingesters {
		pushed := ingesters[i].pushed
		counter = counter + len(pushed)
		for _, pr := range pushed {
			for _, st := range pr.Streams {
				labels[st.Labels] = labels[st.Labels] + 1
			}
		}
	}
	assert.Equal(t, numReq*3, counter) // RF=3
	// each stream is present 3 times
	for i := 0; i < numReq; i++ {
		l := fmt.Sprintf(`{instance="bar-%d"}`, i)
		assert.Equal(t, 3, labels[l], "stream %s expected 3 times, got %d", l, labels[l])
		l = fmt.Sprintf(`{app="foo-%d"}`, i)
		assert.Equal(t, 3, labels[l], "stream %s expected 3 times, got %d", l, labels[l])
	}
}

func Test_SortLabelsOnPush(t *testing.T) {
	limits := &validation.Limits{}
	flagext.DefaultValues(limits)
	limits.EnforceMetricName = false
	ingester := &mockIngester{}
	distributors, _ := prepare(t, 1, 5, limits, func(addr string) (ring_client.PoolClient, error) { return ingester, nil })

	request := makeWriteRequest(10, 10)
	request.Streams[0].Labels = `{buzz="f", a="b"}`
	_, err := distributors[0].Push(ctx, request)
	require.NoError(t, err)
	require.Equal(t, `{a="b", buzz="f"}`, ingester.pushed[0].Streams[0].Labels)
}

func Test_TruncateLogLines(t *testing.T) {
	setup := func() (*validation.Limits, *mockIngester) {
		limits := &validation.Limits{}
		flagext.DefaultValues(limits)

		limits.EnforceMetricName = false
		limits.MaxLineSize = 5
		limits.MaxLineSizeTruncate = true
		return limits, &mockIngester{}
	}

	t.Run("it truncates lines to MaxLineSize when MaxLineSizeTruncate is true", func(t *testing.T) {
		limits, ingester := setup()
		distributors, _ := prepare(t, 1, 5, limits, func(addr string) (ring_client.PoolClient, error) { return ingester, nil })

		_, err := distributors[0].Push(ctx, makeWriteRequest(1, 10))
		require.NoError(t, err)
		require.Len(t, ingester.pushed[0].Streams[0].Entries[0].Line, 5)
	})
}

func Benchmark_SortLabelsOnPush(b *testing.B) {
	limits := &validation.Limits{}
	flagext.DefaultValues(limits)
	limits.EnforceMetricName = false
	distributors, _ := prepare(&testing.T{}, 1, 5, limits, nil)
	d := distributors[0]
	request := makeWriteRequest(10, 10)
	vCtx := d.validator.getValidationContextForTime(testTime, "123")
	for n := 0; n < b.N; n++ {
		stream := request.Streams[0]
		stream.Labels = `{buzz="f", a="b"}`
		_, err := d.parseStreamLabels(vCtx, stream.Labels, &stream)
		if err != nil {
			panic("parseStreamLabels fail,err:" + err.Error())
		}
	}
}

func Benchmark_Push(b *testing.B) {
	limits := &validation.Limits{}
	flagext.DefaultValues(limits)
	limits.IngestionBurstSizeMB = math.MaxInt32
	limits.CardinalityLimit = math.MaxInt32
	limits.IngestionRateMB = math.MaxInt32
	limits.EnforceMetricName = false
	limits.MaxLineSize = math.MaxInt32
	limits.RejectOldSamples = true
	limits.RejectOldSamplesMaxAge = model.Duration(24 * time.Hour)
	limits.CreationGracePeriod = model.Duration(24 * time.Hour)
	distributors, _ := prepare(&testing.T{}, 1, 5, limits, nil)
	request := makeWriteRequest(100000, 100)

	b.ResetTimer()
	b.ReportAllocs()

	for n := 0; n < b.N; n++ {
		_, err := distributors[0].Push(ctx, request)
		if err != nil {
			require.NoError(b, err)
		}
	}
}

func Benchmark_PushWithLineTruncation(b *testing.B) {
	limits := &validation.Limits{}
	flagext.DefaultValues(limits)

	limits.IngestionRateMB = math.MaxInt32
	limits.MaxLineSizeTruncate = true
	limits.MaxLineSize = 50

	distributors, _ := prepare(&testing.T{}, 1, 5, limits, nil)
	request := makeWriteRequest(100000, 100)

	b.ResetTimer()
	b.ReportAllocs()

	for n := 0; n < b.N; n++ {

		_, err := distributors[0].Push(ctx, request)
		if err != nil {
			require.NoError(b, err)
		}
	}
}

func TestDistributor_PushIngestionRateLimiter(t *testing.T) {
	type testPush struct {
		bytes         int
		expectedError error
	}

	tests := map[string]struct {
		distributors          int
		ingestionRateStrategy string
		ingestionRateMB       float64
		ingestionBurstSizeMB  float64
		pushes                []testPush
	}{
		"local strategy: limit should be set to each distributor": {
			distributors:          2,
			ingestionRateStrategy: validation.LocalIngestionRateStrategy,
			ingestionRateMB:       10 * (1.0 / float64(bytesInMB)),
			ingestionBurstSizeMB:  10 * (1.0 / float64(bytesInMB)),
			pushes: []testPush{
				{bytes: 5, expectedError: nil},
				{bytes: 6, expectedError: httpgrpc.Errorf(http.StatusTooManyRequests, validation.RateLimitedErrorMsg, "test", 10, 1, 6)},
				{bytes: 5, expectedError: nil},
				{bytes: 1, expectedError: httpgrpc.Errorf(http.StatusTooManyRequests, validation.RateLimitedErrorMsg, "test", 10, 1, 1)},
			},
		},
		"global strategy: limit should be evenly shared across distributors": {
			distributors:          2,
			ingestionRateStrategy: validation.GlobalIngestionRateStrategy,
			ingestionRateMB:       10 * (1.0 / float64(bytesInMB)),
			ingestionBurstSizeMB:  5 * (1.0 / float64(bytesInMB)),
			pushes: []testPush{
				{bytes: 3, expectedError: nil},
				{bytes: 3, expectedError: httpgrpc.Errorf(http.StatusTooManyRequests, validation.RateLimitedErrorMsg, "test", 5, 1, 3)},
				{bytes: 2, expectedError: nil},
				{bytes: 1, expectedError: httpgrpc.Errorf(http.StatusTooManyRequests, validation.RateLimitedErrorMsg, "test", 5, 1, 1)},
			},
		},
		"global strategy: burst should set to each distributor": {
			distributors:          2,
			ingestionRateStrategy: validation.GlobalIngestionRateStrategy,
			ingestionRateMB:       10 * (1.0 / float64(bytesInMB)),
			ingestionBurstSizeMB:  20 * (1.0 / float64(bytesInMB)),
			pushes: []testPush{
				{bytes: 15, expectedError: nil},
				{bytes: 6, expectedError: httpgrpc.Errorf(http.StatusTooManyRequests, validation.RateLimitedErrorMsg, "test", 5, 1, 6)},
				{bytes: 5, expectedError: nil},
				{bytes: 1, expectedError: httpgrpc.Errorf(http.StatusTooManyRequests, validation.RateLimitedErrorMsg, "test", 5, 1, 1)},
			},
		},
	}

	for testName, testData := range tests {
		testData := testData

		t.Run(testName, func(t *testing.T) {
			limits := &validation.Limits{}
			flagext.DefaultValues(limits)
			limits.EnforceMetricName = false
			limits.IngestionRateStrategy = testData.ingestionRateStrategy
			limits.IngestionRateMB = testData.ingestionRateMB
			limits.IngestionBurstSizeMB = testData.ingestionBurstSizeMB

			distributors, _ := prepare(t, testData.distributors, 5, limits, nil)

			for _, push := range testData.pushes {
				request := makeWriteRequest(1, push.bytes)
				response, err := distributors[0].Push(ctx, request)

				if push.expectedError == nil {
					assert.Equal(t, success, response)
					assert.Nil(t, err)
				} else {
					assert.Nil(t, response)
					assert.Equal(t, push.expectedError, err)
				}
			}
		})
	}
}

func prepare(t *testing.T, numDistributors, numIngesters int, limits *validation.Limits, factory func(addr string) (ring_client.PoolClient, error)) ([]*Distributor, []mockIngester) {
	t.Helper()

	ingesters := make([]mockIngester, numIngesters)
	for i := 0; i < numIngesters; i++ {
		ingesters[i] = mockIngester{}
	}

	ingesterByAddr := map[string]*mockIngester{}
	ingesterDescs := map[string]ring.InstanceDesc{}

	for i := range ingesters {
		addr := fmt.Sprintf("ingester-%d", i)
		ingesterDescs[addr] = ring.InstanceDesc{
			Addr:                addr,
			State:               ring.ACTIVE,
			Timestamp:           time.Now().Unix(),
			RegisteredTimestamp: time.Now().Add(-10 * time.Minute).Unix(),
			Tokens:              []uint32{uint32((math.MaxUint32 / numIngesters) * i)},
		}
		ingesterByAddr[addr] = &ingesters[i]
	}

	kvStore, closer := consul.NewInMemoryClient(ring.GetCodec(), log.NewNopLogger(), nil)

	err := kvStore.CAS(context.Background(), ingester.RingKey,
		func(_ interface{}) (interface{}, bool, error) {
			return &ring.Desc{
				Ingesters: ingesterDescs,
			}, true, nil
		},
	)
	require.NoError(t, err)

	ingestersRing, err := ring.New(ring.Config{
		KVStore: kv.Config{
			Mock: kvStore,
		},
		HeartbeatTimeout:  60 * time.Minute,
		ReplicationFactor: 3,
	}, ingester.RingKey, ingester.RingKey, nil, nil)

	require.NoError(t, err)
	require.NoError(t, services.StartAndAwaitRunning(context.Background(), ingestersRing))

	test.Poll(t, time.Second, numIngesters, func() interface{} {
		return ingestersRing.InstancesCount()
	})

	loopbackName, err := loki_net.LoopbackInterfaceName()
	require.NoError(t, err)

	distributors := make([]*Distributor, numDistributors)
	for i := 0; i < numDistributors; i++ {
		var distributorConfig Config
		var clientConfig client.Config
		flagext.DefaultValues(&distributorConfig, &clientConfig)

		distributorConfig.DistributorRing.HeartbeatPeriod = 100 * time.Millisecond
		distributorConfig.DistributorRing.InstanceID = strconv.Itoa(rand.Int())
		distributorConfig.DistributorRing.KVStore.Mock = kvStore
		distributorConfig.DistributorRing.InstanceAddr = "127.0.0.1"
		distributorConfig.DistributorRing.InstanceInterfaceNames = []string{loopbackName}
		distributorConfig.factory = factory
		if factory == nil {
			distributorConfig.factory = func(addr string) (ring_client.PoolClient, error) {
				return ingesterByAddr[addr], nil
			}
		}

		overrides, err := validation.NewOverrides(*limits, nil)
		require.NoError(t, err)

		d, err := New(distributorConfig, clientConfig, runtime.DefaultTenantConfigs(), ingestersRing, overrides, prometheus.NewPedanticRegistry())
		require.NoError(t, err)
		require.NoError(t, services.StartAndAwaitRunning(context.Background(), d))
		distributors[i] = d
	}

	if distributors[0].distributorsRing != nil {
		test.Poll(t, time.Second, numDistributors, func() interface{} {
			return distributors[0].distributorsLifecycler.HealthyInstancesCount()
		})
	}

	t.Cleanup(func() {
		assert.NoError(t, closer.Close())
		for _, d := range distributors {
			assert.NoError(t, services.StopAndAwaitTerminated(context.Background(), d))
		}
		ingestersRing.StopAsync()
	})

	return distributors, ingesters
}

func makeWriteRequestWithLabels(lines, size int, labels []string) *logproto.PushRequest {
	streams := make([]logproto.Stream, len(labels))
	for i := 0; i < len(labels); i++ {
		stream := logproto.Stream{Labels: labels[i]}

		for j := 0; j < lines; j++ {
			// Construct the log line, honoring the input size
			line := strconv.Itoa(j) + strings.Repeat("0", size)
			line = line[:size]

			stream.Entries = append(stream.Entries, logproto.Entry{
				Timestamp: time.Now().Add(time.Duration(j) * time.Millisecond),
				Line:      line,
			})
		}

		streams[i] = stream
	}

	return &logproto.PushRequest{
		Streams: streams,
	}
}

func makeWriteRequest(lines, size int) *logproto.PushRequest {
	return makeWriteRequestWithLabels(lines, size, []string{`{foo="bar"}`})
}

type mockIngester struct {
	grpc_health_v1.HealthClient
	logproto.PusherClient
	mu sync.Mutex

	pushed []*logproto.PushRequest
}

func (i *mockIngester) Push(ctx context.Context, in *logproto.PushRequest, opts ...grpc.CallOption) (*logproto.PushResponse, error) {
	i.mu.Lock()
	defer i.mu.Unlock()

	i.pushed = append(i.pushed, in)
	return nil, nil
}

func (i *mockIngester) Close() error {
	return nil
}
