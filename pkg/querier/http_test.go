package querier

import (
	"io/ioutil"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/weaveworks/common/user"
)

func TestRangeQueryHandler(t *testing.T) {
	//limits, err := validation.NewOverrides(defaultLimitsTestConfig(), nil)
	q := Querier{}

	req := httptest.NewRequest("GET", "/query_range", nil)
	req = req.WithContext(user.InjectOrgID(req.Context(), "foobar"))
	rq := req.URL.Query()
	rq.Add("query", "{job=\"foo\"}")
	req.URL.RawQuery = rq.Encode()
	err := req.ParseForm()
	require.NoError(t, err)
	w := httptest.NewRecorder()
	q.RangeQueryHandler(w, req)

	resp := w.Result()
	body, err := ioutil.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equalf(t, 200, resp.StatusCode, "request failed: %s", string(body))
}
