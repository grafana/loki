package seriesquery

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logcli/client"
	"github.com/grafana/loki/v3/pkg/loghttp"
)

type testSeriesClient struct {
	client.Client
	response *loghttp.SeriesResponse
	err      error
}

func (c *testSeriesClient) Series(_ []string, _, _ time.Time, _ bool) (*loghttp.SeriesResponse, error) {
	return c.response, c.err
}

func TestSeriesQueryGetSeries(t *testing.T) {
	expected := []loghttp.LabelSet{{"app": "loki"}}
	q := &SeriesQuery{Matcher: `{app="loki"}`}

	series, err := q.GetSeries(&testSeriesClient{response: &loghttp.SeriesResponse{Data: expected}})
	require.NoError(t, err)
	require.Equal(t, expected, series)

	expectedErr := errors.New("get series failed")
	_, err = q.GetSeries(&testSeriesClient{err: expectedErr})
	require.ErrorIs(t, err, expectedErr)
}

func TestSeriesQueryDoSeries(t *testing.T) {
	q := &SeriesQuery{Matcher: `{app="loki"}`}

	require.NoError(t, q.DoSeries(&testSeriesClient{response: &loghttp.SeriesResponse{Data: []loghttp.LabelSet{{"app": "loki"}}}}))

	expectedErr := errors.New("get series failed")
	require.ErrorIs(t, q.DoSeries(&testSeriesClient{err: expectedErr}), expectedErr)
}
