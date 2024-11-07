package worker

import (
	"context"
	"net/http"
	"testing"

	"github.com/gogo/status"
	"github.com/grafana/dskit/httpgrpc"
	"github.com/grafana/dskit/user"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logqlmodel"
	"github.com/grafana/loki/v3/pkg/querier/queryrange"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/v3/pkg/util/server"
)

type HandlerFunc func(context.Context, queryrangebase.Request) (queryrangebase.Response, error)

func (h HandlerFunc) Do(ctx context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
	return h(ctx, req)
}

func TestHandleQueryRequest(t *testing.T) {
	for name, tc := range map[string]struct {
		err    error
		errMsg string
		code   int
	}{
		"bad-request": {
			err:    httpgrpc.Errorf(http.StatusBadRequest, "some input is malformed"),
			errMsg: "some input is malformed",
			code:   http.StatusBadRequest,
		},
		"parser error": {
			err:    logqlmodel.ErrParse,
			errMsg: "failed to parse",
			code:   http.StatusBadRequest,
		},
		"pipeline error": {
			err:    logqlmodel.ErrPipeline,
			errMsg: "failed execute pipeline",
			code:   http.StatusBadRequest,
		},
		"limit error": {
			err:    logqlmodel.ErrLimit,
			errMsg: "limit reached",
			code:   http.StatusBadRequest,
		},
		"blocked error": {
			err:    logqlmodel.ErrBlocked,
			errMsg: "query blocked by policy",
			code:   http.StatusBadRequest,
		},
		"canceled error": {
			err:    context.Canceled,
			errMsg: "cancelled by the client",
			code:   server.StatusClientClosedRequest,
		},
	} {
		t.Run(name, func(t *testing.T) {
			ctx := user.InjectOrgID(context.Background(), "1")
			request, err := queryrange.DefaultCodec.QueryRequestWrap(ctx, &queryrange.LokiRequest{Query: `{app="foo"}`})
			require.NoError(t, err)

			mockHandler := HandlerFunc(func(context.Context, queryrangebase.Request) (queryrangebase.Response, error) {
				return nil, tc.err
			})

			response := handleQueryRequest(ctx, request, mockHandler, queryrange.DefaultCodec)

			require.Equal(t, int32(tc.code), response.Status.Code)
			require.Contains(t, response.Status.Message, tc.errMsg)

			// errors that are not HTTP errors yet will be mapped by util.server.WriteError.
			if httpResp, ok := httpgrpc.HTTPResponseFromError(status.ErrorProto(response.Status)); ok {
				require.Equal(t, tc.code, int(httpResp.Code))
			}
		})
	}
}
