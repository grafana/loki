package distributor

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/user"

	"github.com/grafana/loki/v3/pkg/loghttp/push"
	"github.com/grafana/loki/v3/pkg/logproto"

	"github.com/grafana/dskit/flagext"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/validation"
)

func TestDistributorRingHandler(t *testing.T) {
	limits := &validation.Limits{}
	flagext.DefaultValues(limits)

	runServer := func() *httptest.Server {
		distributors, _ := prepare(t, 1, 3, limits, nil)

		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			distributors[0].ServeHTTP(w, r)
		}))
	}

	t.Run("renders ring status for global rate limiting", func(t *testing.T) {
		limits.IngestionRateStrategy = validation.GlobalIngestionRateStrategy
		svr := runServer()
		defer svr.Close()

		resp, err := svr.Client().Get(svr.URL)
		require.NoError(t, err)

		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.Contains(t, string(body), "<th>Instance ID</th>")
		require.NotContains(t, string(body), "Not running with Global Rating Limit - ring not being used by the Distributor")
	})

	t.Run("doesn't return ring status for local rate limiting", func(t *testing.T) {
		limits.IngestionRateStrategy = validation.LocalIngestionRateStrategy
		svr := runServer()
		defer svr.Close()

		resp, err := svr.Client().Get(svr.URL)
		require.NoError(t, err)

		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.Contains(t, string(body), "Not running with Global Rating Limit - ring not being used by the Distributor")
		require.NotContains(t, string(body), "<th>Instance ID</th>")
	})
}

func TestRequestParserWrapping(t *testing.T) {
	limits := &validation.Limits{}
	flagext.DefaultValues(limits)
	limits.RejectOldSamples = false
	distributors, _ := prepare(t, 1, 3, limits, nil)

	var called bool
	distributors[0].RequestParserWrapper = func(requestParser push.RequestParser) push.RequestParser {
		called = true
		return requestParser
	}

	ctx := user.InjectOrgID(context.Background(), "test-user")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "fake-path", nil)
	require.NoError(t, err)

	distributors[0].pushHandler(httptest.NewRecorder(), req, stubParser, push.HTTPError)

	require.True(t, called)
}

func stubParser(
	_ string,
	_ *http.Request,
	_ push.TenantsRetention,
	_ push.Limits,
	_ push.UsageTracker,
	_ bool,
	_ log.Logger,
) (*logproto.PushRequest, *push.Stats, error) {
	return &logproto.PushRequest{}, &push.Stats{}, nil
}
