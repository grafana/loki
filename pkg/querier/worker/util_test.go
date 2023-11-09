package worker

import (
	"context"
	"net/http"
	"testing"

	"github.com/gogo/status"
	"github.com/grafana/dskit/httpgrpc"
	"github.com/grafana/dskit/user"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/querier/queryrange"
	"github.com/grafana/loki/pkg/querier/queryrange/queryrangebase"
)

type HandlerFunc func(context.Context, queryrangebase.Request) (queryrangebase.Response, error)

func (h HandlerFunc) Do(ctx context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
	return h(ctx, req)
}

func TestHandleQueryRequest(t *testing.T) {
	ctx := user.InjectOrgID(context.Background(), "1")
	request, err := queryrange.DefaultCodec.QueryRequestWrap(ctx, &queryrange.LokiRequest{})
	require.NoError(t, err)

	mockHandler := HandlerFunc(func(context.Context, queryrangebase.Request) (queryrangebase.Response, error) {
		return nil, httpgrpc.Errorf(http.StatusBadRequest, "some input is malformed")
	})

	response := handleQueryRequest(ctx, request, mockHandler, queryrange.DefaultCodec)

	require.Equal(t, "some input is malformed", response.Status.Message)

	httpResp, ok := httpgrpc.HTTPResponseFromError(status.ErrorProto(response.Status))
	require.True(t, ok)
	require.Equal(t, http.StatusBadRequest, int(httpResp.Code))
}
