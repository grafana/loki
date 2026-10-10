package tail

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/user"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/querier/plan"
	"github.com/grafana/loki/v3/pkg/validation"
)

func TestTailHandler(t *testing.T) {
	defaultLimits := defaultLimitsTestConfig()
	limits, err := validation.NewOverrides(defaultLimits, nil)
	require.NoError(t, err)

	tailQuerier := NewQuerier(nil, nil, nil, limits, 1*time.Minute, NewMetrics(nil), log.NewNopLogger())

	req, err := http.NewRequest("GET", `/`, nil)
	require.NoError(t, err)
	q := req.URL.Query()
	q.Add("query", `{app="loki"}`)
	req.URL.RawQuery = q.Encode()
	err = req.ParseForm()
	require.NoError(t, err)

	ctx := user.InjectOrgID(req.Context(), "1|2")
	req = req.WithContext(ctx)
	require.NoError(t, err)

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(tailQuerier.TailHandler)

	handler.ServeHTTP(rr, req)
	require.Equal(t, http.StatusBadRequest, rr.Code)
	require.Equal(t, "multiple org IDs present", rr.Body.String())
}

func TestTailHandlerRejectsInvalidPipelineRegexpBeforeUpgrade(t *testing.T) {
	defaultLimits := defaultLimitsTestConfig()
	limits, err := validation.NewOverrides(defaultLimits, nil)
	require.NoError(t, err)

	tailQuerier := NewQuerier(nil, nil, nil, limits, 1*time.Minute, NewMetrics(nil), log.NewNopLogger())

	req, err := http.NewRequest("GET", `/`, nil)
	require.NoError(t, err)
	q := req.URL.Query()
	q.Add("query", `{app="loki"} |~ "GET("`)
	req.URL.RawQuery = q.Encode()
	err = req.ParseForm()
	require.NoError(t, err)

	ctx := user.InjectOrgID(req.Context(), "fake")
	req = req.WithContext(ctx)
	require.NoError(t, err)

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(tailQuerier.TailHandler)

	handler.ServeHTTP(rr, req)
	require.Equal(t, http.StatusBadRequest, rr.Code)
	require.Contains(t, rr.Body.String(), "error parsing regexp")
}

func TestValidateTailQuery(t *testing.T) {
	for _, tc := range []struct {
		name    string
		query   string
		wantErr string
	}{
		{name: "matchers only", query: `{app="loki"}`},
		{name: "valid line filter regexp", query: `{app="loki"} |~ "GET /"`},
		{name: "invalid line filter regexp", query: `{app="loki"} |~ "GET("`, wantErr: "error parsing regexp"},
		{name: "non-selector expression", query: `rate({app="loki"}[5m])`, wantErr: "unsupported query expression"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parsed, err := syntax.ParseExpr(tc.query)
			require.NoError(t, err)

			req := &logproto.TailRequest{Plan: &plan.QueryPlan{AST: parsed}}
			err = validateTailQuery(req)
			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func defaultLimitsTestConfig() validation.Limits {
	limits := validation.Limits{}
	flagext.DefaultValues(&limits)
	return limits
}
