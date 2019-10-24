package distributor

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/ring"
	"github.com/cortexproject/cortex/pkg/util/flagext"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/weaveworks/common/httpgrpc"
	"github.com/weaveworks/common/user"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/grafana/loki/pkg/ingester/client"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/util/validation"
)

const (
	numIngesters       = 5
	ingestionRateLimit = 0.000096 // 100 Bytes/s limit
)

var (
	success = &logproto.PushResponse{}
	ctx     = user.InjectOrgID(context.Background(), "test")
)

func TestDistributor(t *testing.T) {
	for i, tc := range []struct {
		samples          int
		expectedResponse *logproto.PushResponse
		expectedError    error
	}{
		{
			samples:          10,
			expectedResponse: success,
		},
		{
			samples:       100,
			expectedError: httpgrpc.Errorf(http.StatusTooManyRequests, "ingestion rate limit (100) exceeded while adding 100 lines"),
		},
	} {
		t.Run(fmt.Sprintf("[%d](samples=%v)", i, tc.samples), func(t *testing.T) {
			d := prepare(t)

			request := makeWriteRequest(tc.samples)
			response, err := d.Push(ctx, request)
			assert.Equal(t, tc.expectedResponse, response)
			assert.Equal(t, tc.expectedError, err)
		})
	}
}

func prepare(t *testing.T) *Distributor {
	var (
		distributorConfig Config
		defaultLimits     validation.Limits
		clientConfig      client.Config
	)
	flagext.DefaultValues(&distributorConfig, &defaultLimits, &clientConfig)
	defaultLimits.EnforceMetricName = false
	defaultLimits.IngestionRate = ingestionRateLimit
	defaultLimits.IngestionBurstSize = ingestionRateLimit

	limits, err := validation.NewOverrides(defaultLimits)
	require.NoError(t, err)

	ingesters := map[string]*mockIngester{}
	for i := 0; i < numIngesters; i++ {
		ingesters[fmt.Sprintf("ingester%d", i)] = &mockIngester{}
	}

	r := &mockRing{
		replicationFactor: 3,
	}
	for addr := range ingesters {
		r.ingesters = append(r.ingesters, ring.IngesterDesc{
			Addr: addr,
		})
	}

	distributorConfig.factory = func(addr string) (grpc_health_v1.HealthClient, error) {
		return ingesters[addr], nil
	}

	d, err := New(distributorConfig, clientConfig, r, limits)
	require.NoError(t, err)

	return d
}

func makeWriteRequest(samples int) *logproto.PushRequest {
	req := logproto.PushRequest{
		Streams: []*logproto.Stream{
			{
				Labels: `{foo="bar"}`,
			},
		},
	}

	for i := 0; i < samples; i++ {
		req.Streams[0].Entries = append(req.Streams[0].Entries, logproto.Entry{
			Timestamp: time.Unix(0, 0),
			Line:      fmt.Sprintf("line %d", i),
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

func (r mockRing) GetAll() (ring.ReplicationSet, error) {
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
