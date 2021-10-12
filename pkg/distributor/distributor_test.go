package distributor

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/ring"
	ring_client "github.com/cortexproject/cortex/pkg/ring/client"
	"github.com/cortexproject/cortex/pkg/util/test"
	"github.com/go-kit/kit/log"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/kv"
	"github.com/grafana/dskit/kv/consul"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/weaveworks/common/httpgrpc"
	"github.com/weaveworks/common/user"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/grafana/loki/pkg/ingester/client"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/runtime"
	fe "github.com/grafana/loki/pkg/util/flagext"
	"github.com/grafana/loki/pkg/validation"
)

const (
	numIngesters = 5
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
			expectedError: httpgrpc.Errorf(http.StatusTooManyRequests, validation.RateLimitedErrorMsg, 100, 100, 1000),
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
		t.Run(fmt.Sprintf("[%d](samples=%v)", i, tc.lines), func(t *testing.T) {
			limits := &validation.Limits{}
			flagext.DefaultValues(limits)
			limits.EnforceMetricName = false
			limits.IngestionRateMB = ingestionRateLimit
			limits.IngestionBurstSizeMB = ingestionRateLimit
			limits.MaxLineSize = fe.ByteSize(tc.maxLineSize)

			d := prepare(t, limits, nil, nil)
			defer services.StopAndAwaitTerminated(context.Background(), d) //nolint:errcheck

			request := makeWriteRequest(tc.lines, 10)

			if tc.mangleLabels {
				request.Streams[0].Labels = `{ab"`
			}

			response, err := d.Push(ctx, request)
			assert.Equal(t, tc.expectedResponse, response)
			assert.Equal(t, tc.expectedError, err)
		})
	}
}

func Test_SortLabelsOnPush(t *testing.T) {
	limits := &validation.Limits{}
	flagext.DefaultValues(limits)
	limits.EnforceMetricName = false
	ingester := &mockIngester{}
	d := prepare(t, limits, nil, func(addr string) (ring_client.PoolClient, error) { return ingester, nil })
	defer services.StopAndAwaitTerminated(context.Background(), d) //nolint:errcheck

	request := makeWriteRequest(10, 10)
	request.Streams[0].Labels = `{buzz="f", a="b"}`
	_, err := d.Push(ctx, request)
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

		d := prepare(t, limits, nil, func(addr string) (ring_client.PoolClient, error) { return ingester, nil })
		defer services.StopAndAwaitTerminated(context.Background(), d) //nolint:errcheck

		_, err := d.Push(ctx, makeWriteRequest(1, 10))
		require.NoError(t, err)
		require.Len(t, ingester.pushed[0].Streams[0].Entries[0].Line, 5)
	})
}

func Benchmark_SortLabelsOnPush(b *testing.B) {
	limits := &validation.Limits{}
	flagext.DefaultValues(limits)
	limits.EnforceMetricName = false
	ingester := &mockIngester{}
	d := prepare(&testing.T{}, limits, nil, func(addr string) (ring_client.PoolClient, error) { return ingester, nil })
	defer services.StopAndAwaitTerminated(context.Background(), d) //nolint:errcheck
	request := makeWriteRequest(10, 10)
	vCtx := d.validator.getValidationContextFor("123")
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
	ingester := &mockIngester{}
	d := prepare(&testing.T{}, limits, nil, func(addr string) (ring_client.PoolClient, error) { return ingester, nil })
	defer services.StopAndAwaitTerminated(context.Background(), d) //nolint:errcheck
	request := makeWriteRequest(100000, 100)

	b.ResetTimer()
	b.ReportAllocs()

	for n := 0; n < b.N; n++ {

		_, err := d.Push(ctx, request)
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

	ingester := &mockIngester{}
	d := prepare(&testing.T{}, limits, nil, func(addr string) (ring_client.PoolClient, error) { return ingester, nil })
	defer services.StopAndAwaitTerminated(context.Background(), d) //nolint:errcheck
	request := makeWriteRequest(100000, 100)

	b.ResetTimer()
	b.ReportAllocs()

	for n := 0; n < b.N; n++ {

		_, err := d.Push(ctx, request)
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
				{bytes: 6, expectedError: httpgrpc.Errorf(http.StatusTooManyRequests, validation.RateLimitedErrorMsg, 10, 1, 6)},
				{bytes: 5, expectedError: nil},
				{bytes: 1, expectedError: httpgrpc.Errorf(http.StatusTooManyRequests, validation.RateLimitedErrorMsg, 10, 1, 1)},
			},
		},
		"global strategy: limit should be evenly shared across distributors": {
			distributors:          2,
			ingestionRateStrategy: validation.GlobalIngestionRateStrategy,
			ingestionRateMB:       10 * (1.0 / float64(bytesInMB)),
			ingestionBurstSizeMB:  5 * (1.0 / float64(bytesInMB)),
			pushes: []testPush{
				{bytes: 3, expectedError: nil},
				{bytes: 3, expectedError: httpgrpc.Errorf(http.StatusTooManyRequests, validation.RateLimitedErrorMsg, 5, 1, 3)},
				{bytes: 2, expectedError: nil},
				{bytes: 1, expectedError: httpgrpc.Errorf(http.StatusTooManyRequests, validation.RateLimitedErrorMsg, 5, 1, 1)},
			},
		},
		"global strategy: burst should set to each distributor": {
			distributors:          2,
			ingestionRateStrategy: validation.GlobalIngestionRateStrategy,
			ingestionRateMB:       10 * (1.0 / float64(bytesInMB)),
			ingestionBurstSizeMB:  20 * (1.0 / float64(bytesInMB)),
			pushes: []testPush{
				{bytes: 15, expectedError: nil},
				{bytes: 6, expectedError: httpgrpc.Errorf(http.StatusTooManyRequests, validation.RateLimitedErrorMsg, 5, 1, 6)},
				{bytes: 5, expectedError: nil},
				{bytes: 1, expectedError: httpgrpc.Errorf(http.StatusTooManyRequests, validation.RateLimitedErrorMsg, 5, 1, 1)},
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

			// Init a shared KVStore
			kvStore, closer := consul.NewInMemoryClient(ring.GetCodec(), log.NewNopLogger(), nil)
			t.Cleanup(func() { assert.NoError(t, closer.Close()) })

			// Start all expected distributors
			distributors := make([]*Distributor, testData.distributors)
			for i := 0; i < testData.distributors; i++ {
				distributors[i] = prepare(t, limits, kvStore, nil)
				defer services.StopAndAwaitTerminated(context.Background(), distributors[i]) //nolint:errcheck
			}

			// If the distributors ring is setup, wait until the first distributor
			// updates to the expected size
			if distributors[0].distributorsRing != nil {
				test.Poll(t, time.Second, testData.distributors, func() interface{} {
					return distributors[0].distributorsRing.HealthyInstancesCount()
				})
			}

			// Push samples in multiple requests to the first distributor
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

// loopbackInterfaceName search for the name of a loopback interface in the list
// of the system's network interfaces.
func loopbackInterfaceName() (string, error) {
	is, err := net.Interfaces()
	if err != nil {
		return "", fmt.Errorf("can't retrieve loopback interface name: %s", err)
	}

	for _, i := range is {
		if i.Flags&net.FlagLoopback != 0 {
			return i.Name, nil
		}
	}

	return "", fmt.Errorf("can't retrieve loopback interface name")
}

func prepare(t *testing.T, limits *validation.Limits, kvStore kv.Client, factory func(addr string) (ring_client.PoolClient, error)) *Distributor {
	var (
		distributorConfig Config
		clientConfig      client.Config
	)
	flagext.DefaultValues(&distributorConfig, &clientConfig)

	overrides, err := validation.NewOverrides(*limits, nil)
	require.NoError(t, err)

	// Mock the ingesters ring
	ingesters := map[string]*mockIngester{}
	for i := 0; i < numIngesters; i++ {
		ingesters[fmt.Sprintf("ingester%d", i)] = &mockIngester{}
	}

	ingestersRing := &mockRing{
		replicationFactor: 3,
	}
	for addr := range ingesters {
		ingestersRing.ingesters = append(ingestersRing.ingesters, ring.InstanceDesc{
			Addr: addr,
		})
	}

	loopbackName, err := loopbackInterfaceName()
	require.NoError(t, err)

	distributorConfig.DistributorRing.HeartbeatPeriod = 100 * time.Millisecond
	distributorConfig.DistributorRing.InstanceID = strconv.Itoa(rand.Int())
	distributorConfig.DistributorRing.KVStore.Mock = kvStore
	distributorConfig.DistributorRing.InstanceInterfaceNames = []string{loopbackName}
	distributorConfig.factory = factory
	if factory == nil {
		distributorConfig.factory = func(addr string) (ring_client.PoolClient, error) {
			return ingesters[addr], nil
		}
	}

	d, err := New(distributorConfig, clientConfig, runtime.DefaultTenantConfigs(), ingestersRing, overrides, nil)
	require.NoError(t, err)
	require.NoError(t, services.StartAndAwaitRunning(context.Background(), d))

	return d
}

func makeWriteRequest(lines int, size int) *logproto.PushRequest {
	req := logproto.PushRequest{
		Streams: []logproto.Stream{
			{
				Labels: `{foo="bar"}`,
			},
		},
	}

	for i := 0; i < lines; i++ {
		// Construct the log line, honoring the input size
		line := strconv.Itoa(i) + strings.Repeat(" ", size)
		line = line[:size]

		req.Streams[0].Entries = append(req.Streams[0].Entries, logproto.Entry{
			Timestamp: time.Now().Add(time.Duration(i) * time.Millisecond),
			Line:      line,
		})
	}
	return &req
}

type mockIngester struct {
	grpc_health_v1.HealthClient
	logproto.PusherClient

	pushed []*logproto.PushRequest
}

func (i *mockIngester) Push(ctx context.Context, in *logproto.PushRequest, opts ...grpc.CallOption) (*logproto.PushResponse, error) {
	i.pushed = append(i.pushed, in)
	return nil, nil
}

func (i *mockIngester) Close() error {
	return nil
}

// Copied from Cortex; TODO(twilkie) - factor this our and share it.
// mockRing doesn't do virtual nodes, just returns mod(key) + replicationFactor
// ingesters.
type mockRing struct {
	prometheus.Counter
	ingesters         []ring.InstanceDesc
	replicationFactor uint32
}

func (r mockRing) Get(key uint32, op ring.Operation, buf []ring.InstanceDesc, _ []string, _ []string) (ring.ReplicationSet, error) {
	result := ring.ReplicationSet{
		MaxErrors: 1,
		Instances: buf[:0],
	}
	for i := uint32(0); i < r.replicationFactor; i++ {
		n := (key + i) % uint32(len(r.ingesters))
		result.Instances = append(result.Instances, r.ingesters[n])
	}
	return result, nil
}

func (r mockRing) GetAllHealthy(op ring.Operation) (ring.ReplicationSet, error) {
	return r.GetReplicationSetForOperation(op)
}

func (r mockRing) GetReplicationSetForOperation(op ring.Operation) (ring.ReplicationSet, error) {
	return ring.ReplicationSet{
		Instances: r.ingesters,
		MaxErrors: 1,
	}, nil
}

func (r mockRing) ReplicationFactor() int {
	return int(r.replicationFactor)
}

func (r mockRing) InstancesCount() int {
	return len(r.ingesters)
}

func (r mockRing) Subring(key uint32, n int) ring.ReadRing {
	return r
}

func (r mockRing) HasInstance(instanceID string) bool {
	for _, ing := range r.ingesters {
		if ing.Addr != instanceID {
			return true
		}
	}
	return false
}

func (r mockRing) ShuffleShard(identifier string, size int) ring.ReadRing {
	// take advantage of pass by value to bound to size:
	r.ingesters = r.ingesters[:size]
	return r
}

func (r mockRing) ShuffleShardWithLookback(identifier string, size int, lookbackPeriod time.Duration, now time.Time) ring.ReadRing {
	return r
}

func (r mockRing) CleanupShuffleShardCache(identifier string) {}

func (r mockRing) GetInstanceState(instanceID string) (ring.InstanceState, error) {
	return 0, nil
}
