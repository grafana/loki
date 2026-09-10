package index

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logcli/client"
	"github.com/grafana/loki/v3/pkg/logcli/output"
	"github.com/grafana/loki/v3/pkg/logcli/volume"
	"github.com/grafana/loki/v3/pkg/loghttp"
	"github.com/grafana/loki/v3/pkg/logproto"
)

type testIndexClient struct {
	client.Client
	statsResponse  *logproto.IndexStatsResponse
	volumeResponse *loghttp.QueryResponse
	err            error
}

func (c *testIndexClient) GetStats(_ string, _, _ time.Time, _ bool) (*logproto.IndexStatsResponse, error) {
	return c.statsResponse, c.err
}

func (c *testIndexClient) GetVolume(_ *volume.Query) (*loghttp.QueryResponse, error) {
	return c.volumeResponse, c.err
}

func (c *testIndexClient) GetVolumeRange(_ *volume.Query) (*loghttp.QueryResponse, error) {
	return c.volumeResponse, c.err
}

func TestStatsQueryStats(t *testing.T) {
	expected := &logproto.IndexStatsResponse{Streams: 1, Chunks: 2, Entries: 3, Bytes: 4}
	q := &StatsQuery{QueryString: `{app="loki"}`}

	stats, err := q.Stats(&testIndexClient{statsResponse: expected})
	require.NoError(t, err)
	require.Equal(t, expected, stats)

	expectedErr := errors.New("get stats failed")
	_, err = q.Stats(&testIndexClient{err: expectedErr})
	require.ErrorIs(t, err, expectedErr)
}

func TestStatsQueryDoStats(t *testing.T) {
	q := &StatsQuery{QueryString: `{app="loki"}`}

	require.NoError(t, q.DoStats(&testIndexClient{statsResponse: &logproto.IndexStatsResponse{Streams: 1}}))

	expectedErr := errors.New("get stats failed")
	require.ErrorIs(t, q.DoStats(&testIndexClient{err: expectedErr}), expectedErr)
}

func TestGetVolume(t *testing.T) {
	testVolumeQuery(t, false)
}

func TestGetVolumeRange(t *testing.T) {
	testVolumeQuery(t, true)
}

func testVolumeQuery(t *testing.T, rangeQuery bool) {
	t.Helper()

	q := &volume.Query{QueryString: `{app="loki"}`}
	out := output.NewRaw(&bytes.Buffer{}, &output.LogOutputOptions{})
	response := &loghttp.QueryResponse{Data: loghttp.QueryResponseData{Result: loghttp.Streams{}}}
	call := GetVolume
	if rangeQuery {
		call = GetVolumeRange
	}

	require.NoError(t, call(q, &testIndexClient{volumeResponse: response}, out, false))

	expectedErr := errors.New("get volume failed")
	require.ErrorIs(t, call(q, &testIndexClient{err: expectedErr}, out, false), expectedErr)
}
