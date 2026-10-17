package distributor

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/httpgrpc"
	ring_client "github.com/grafana/dskit/ring/client"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/validation"
)

type rejectingBatchIngester struct {
	mockIngester
	err error
}

func (i *rejectingBatchIngester) Push(ctx context.Context, req *logproto.PushRequest, options ...grpc.CallOption) (*logproto.PushResponse, error) {
	response, err := i.mockIngester.Push(ctx, req, options...)
	if err != nil {
		return response, err
	}
	return response, i.err
}

func TestDistributorGroupedPushRateLimitQuorum(t *testing.T) {
	for _, failures := range []int{0, 1, 2, 3} {
		t.Run(strconv.Itoa(failures)+" rejecting replicas", func(t *testing.T) {
			limits := &validation.Limits{}
			flagext.DefaultValues(limits)
			clients := make([]*rejectingBatchIngester, 3)
			for i := range clients {
				clients[i] = &rejectingBatchIngester{}
				if i < failures {
					clients[i].err = httpgrpc.Errorf(http.StatusTooManyRequests, "one stream in the batch was rate limited")
				}
			}
			distributors, _ := prepare(t, 1, 3, limits, func(addr string) (ring_client.PoolClient, error) {
				i, err := strconv.Atoi(strings.TrimPrefix(addr, "ingester-"))
				if err != nil {
					return nil, err
				}
				return clients[i], nil
			})
			_, err := distributors[0].Push(ctx, makeWriteRequestWithLabels(2, 64, []string{`{kind="limited"}`, `{kind="valid"}`}, false, false, false))
			if failures < 2 {
				require.NoError(t, err)
			} else {
				response, ok := httpgrpc.HTTPResponseFromError(err)
				require.True(t, ok)
				require.Equal(t, int32(http.StatusTooManyRequests), response.Code)
			}
		})
	}
}
