package distributor

import (
	"context"
	"fmt"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/ring"
	ring_client "github.com/cortexproject/cortex/pkg/ring/client"
	"github.com/cortexproject/cortex/pkg/ring/kv"
	"github.com/cortexproject/cortex/pkg/ring/kv/consul"
	"github.com/cortexproject/cortex/pkg/util/flagext"
	"github.com/cortexproject/cortex/pkg/util/services"
	"github.com/cortexproject/cortex/pkg/util/test"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/weaveworks/common/httpgrpc"
	"github.com/weaveworks/common/user"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/grafana/loki/pkg/ingester/client"
	"github.com/grafana/loki/pkg/logproto"
	fe "github.com/grafana/loki/pkg/util/flagext"
	"github.com/grafana/loki/pkg/util/validation"
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
			expectedError: httpgrpc.Errorf(http.StatusTooManyRequests, validation.RateLimitedErrorMsg(100, 100, 1000)),
		},
		{
			lines:            100,
			maxLineSize:      1,
			expectedResponse: success,
			expectedError:    httpgrpc.Errorf(http.StatusBadRequest, validation.LineTooLongErrorMsg(1, 10, "{foo=\"bar\"}")),
		},
		{
			lines:            100,
			mangleLabels:     true,
			expectedResponse: success,
			expectedError:    httpgrpc.Errorf(http.StatusBadRequest, "error parsing labels: parse error at line 1, col 4: literal not terminated"),
		},
	} {
		t.Run(fmt.Sprintf("[%d](samples=%v)", i, tc.lines), func(t *testing.T) {
			limits := &validation.Limits{}
			flagext.DefaultValues(limits)
			limits.EnforceMetricName = false
			limits.IngestionRateMB = ingestionRateLimit
			limits.IngestionBurstSizeMB = ingestionRateLimit
			limits.MaxLineSize = fe.ByteSize(tc.maxLineSize)

			d := prepare(t, limits, nil)
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
				{bytes: 6, expectedError: httpgrpc.Errorf(http.StatusTooManyRequests, validation.RateLimitedErrorMsg(10, 1, 6))},
				{bytes: 5, expectedError: nil},
				{bytes: 1, expectedError: httpgrpc.Errorf(http.StatusTooManyRequests, validation.RateLimitedErrorMsg(10, 1, 1))},
			},
		},
		"global strategy: limit should be evenly shared across distributors": {
			distributors:          2,
			ingestionRateStrategy: validation.GlobalIngestionRateStrategy,
			ingestionRateMB:       10 * (1.0 / float64(bytesInMB)),
			ingestionBurstSizeMB:  5 * (1.0 / float64(bytesInMB)),
			pushes: []testPush{
				{bytes: 3, expectedError: nil},
				{bytes: 3, expectedError: httpgrpc.Errorf(http.StatusTooManyRequests, validation.RateLimitedErrorMsg(5, 1, 3))},
				{bytes: 2, expectedError: nil},
				{bytes: 1, expectedError: httpgrpc.Errorf(http.StatusTooManyRequests, validation.RateLimitedErrorMsg(5, 1, 1))},
			},
		},
		"global strategy: burst should set to each distributor": {
			distributors:          2,
			ingestionRateStrategy: validation.GlobalIngestionRateStrategy,
			ingestionRateMB:       10 * (1.0 / float64(bytesInMB)),
			ingestionBurstSizeMB:  20 * (1.0 / float64(bytesInMB)),
			pushes: []testPush{
				{bytes: 15, expectedError: nil},
				{bytes: 6, expectedError: httpgrpc.Errorf(http.StatusTooManyRequests, validation.RateLimitedErrorMsg(5, 1, 6))},
				{bytes: 5, expectedError: nil},
				{bytes: 1, expectedError: httpgrpc.Errorf(http.StatusTooManyRequests, validation.RateLimitedErrorMsg(5, 1, 1))},
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
			kvStore := consul.NewInMemoryClient(ring.GetCodec())

			// Start all expected distributors
			distributors := make([]*Distributor, testData.distributors)
			for i := 0; i < testData.distributors; i++ {
				distributors[i] = prepare(t, limits, kvStore)
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

func prepare(t *testing.T, limits *validation.Limits, kvStore kv.Client) *Distributor {
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
		ingestersRing.ingesters = append(ingestersRing.ingesters, ring.IngesterDesc{
			Addr: addr,
		})
	}

	distributorConfig.DistributorRing.HeartbeatPeriod = 100 * time.Millisecond
	distributorConfig.DistributorRing.InstanceID = strconv.Itoa(rand.Int())
	distributorConfig.DistributorRing.KVStore.Mock = kvStore
	distributorConfig.DistributorRing.InstanceInterfaceNames = []string{"eth0", "en0", "lo0"}
	distributorConfig.factory = func(addr string) (ring_client.PoolClient, error) {
		return ingesters[addr], nil
	}

	d, err := New(distributorConfig, clientConfig, ingestersRing, overrides, nil)
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
			Timestamp: time.Unix(0, 0),
			Line:      line,
		})
	}
	return &req
}

type mockIngester struct {
	grpc_health_v1.HealthClient
	logproto.PusherClient
}

func (i *mockIngester) Push(ctx context.Context, in *logproto.PushRequest, opts ...grpc.CallOption) (*logproto.PushResponse, error) {
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
	ingesters         []ring.IngesterDesc
	replicationFactor uint32
}

func (r mockRing) Get(key uint32, op ring.Operation, buf []ring.IngesterDesc) (ring.ReplicationSet, error) {
	result := ring.ReplicationSet{
		MaxErrors: 1,
		Ingesters: buf[:0],
	}
	for i := uint32(0); i < r.replicationFactor; i++ {
		n := (key + i) % uint32(len(r.ingesters))
		result.Ingesters = append(result.Ingesters, r.ingesters[n])
	}
	return result, nil
}

func (r mockRing) GetAll(op ring.Operation) (ring.ReplicationSet, error) {
	return ring.ReplicationSet{
		Ingesters: r.ingesters,
		MaxErrors: 1,
	}, nil
}

func (r mockRing) ReplicationFactor() int {
	return int(r.replicationFactor)
}

func (r mockRing) IngesterCount() int {
	return len(r.ingesters)
}

func (r mockRing) Subring(key uint32, n int) (ring.ReadRing, error) {
	return r, nil
}
