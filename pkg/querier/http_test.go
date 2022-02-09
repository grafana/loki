package querier

import (
	"io"
	"io/ioutil"
	"net/http/httptest"
	"testing"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/validation"
	jsoniter "github.com/json-iterator/go"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/weaveworks/common/user"
)

// TestRangeQueryHandlerLinks verifies that the response includes the proper slef and next links for batching.
// The querier always returns one entry per seconds starting at Unix(100, 0). The limit is 10.
func TestRangeQueryHandlerLinks(t *testing.T) {
	limits, _ := validation.NewOverrides(defaultLimitsTestConfig(), nil)
	store := newStoreMock()
	store.On("SelectLogs", mock.Anything, mock.Anything).Return(mockStreamIterator(100, 20), nil)

	queryClient := newQueryClientMock()
	ingesterClient := newQuerierClientMock()
	ingesterClient.On("Query", mock.Anything, mock.Anything, mock.Anything).Return(queryClient, nil)

	q, err := newQuerier(
		mockQuerierConfig(),
		mockIngesterClientConfig(),
		newIngesterClientMockFactory(ingesterClient),
		mockReadRingWithOneActiveIngester(),
		store, limits)
	require.NoError(t, err)

	for _, tc := range []struct {
		direction string
		self      string
		next      string
	}{
		{
			"forward",
			"/loki/api/v1/query_range?direction=FORWARD&end=120000000000&interval=0&limit=10&query=%7Btype%3D%22test%22%7D&start=100000000000&step=1000000000",
			"/loki/api/v1/query_range?direction=FORWARD&end=120000000000&interval=0&limit=10&query=%7Btype%3D%22test%22%7D&start=109000000001&step=1000000000",
		},
		{
			"backward",
			"/loki/api/v1/query_range?direction=BACKWARD&end=120000000000&interval=0&limit=10&query=%7Btype%3D%22test%22%7D&start=100000000000&step=1000000000",
			"/loki/api/v1/query_range?direction=BACKWARD&end=100000000000&interval=0&limit=10&query=%7Btype%3D%22test%22%7D&start=100000000000&step=1000000000",
		},
	} {
		t.Run(tc.direction, func(t *testing.T) {
			queryClient.On("Recv").Return(mockQueryResponse([]logproto.Stream{mockStream(100, 1)}), nil).Once()
			queryClient.On("Recv").Return(nil, io.EOF).Once()

			req := httptest.NewRequest("GET", "/query_range", nil)
			req = req.WithContext(user.InjectOrgID(req.Context(), "foobar"))
			rq := req.URL.Query()
			rq.Add("query", "{type=\"test\"}")
			rq.Add("direction", tc.direction)
			rq.Add("limit", "10")
			rq.Add("start", "100000000000")
			rq.Add("end", "120000000000")
			req.URL.RawQuery = rq.Encode()
			err = req.ParseForm()
			require.NoError(t, err)
			w := httptest.NewRecorder()
			q.RangeQueryHandler(w, req)

			resp := w.Result()
			body, err := ioutil.ReadAll(resp.Body)
			require.NoError(t, err)
			require.Equalf(t, 200, resp.StatusCode, "request failed: %s", string(body))

			self := jsoniter.Get(body, "links", 0, "href").ToString()
			require.Equal(t, tc.self, self)

			next := jsoniter.Get(body, "links", 1, "href").ToString()
			require.Equal(t, tc.next, next)
		})
	}
}
