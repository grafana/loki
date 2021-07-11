package client

import (
	"bytes"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/grafana/loki/pkg/loghttp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFileClient_QueryRange(t *testing.T) {

}

func TestFileClient_Query(t *testing.T) {

}

func TestFileClient_ListLabelNames(t *testing.T) {
	c := newEmptyClient(t)
	values, err := c.ListLabelNames(true, time.Now(), time.Now())
	require.NoError(t, err)
	assert.Equal(t, &loghttp.LabelResponse{
		Data:   []string{defaultLabelKey},
		Status: loghttp.QueryStatusSuccess,
	}, values)
}

func TestFileClient_ListLabelValues(t *testing.T) {
	c := newEmptyClient(t)
	values, err := c.ListLabelValues(defaultLabelKey, true, time.Now(), time.Now())
	require.NoError(t, err)
	assert.Equal(t, &loghttp.LabelResponse{
		Data:   []string{defaultLabelValue},
		Status: loghttp.QueryStatusSuccess,
	}, values)

}

func TestFileClient_Series(t *testing.T) {
	c := newEmptyClient(t)
	got, err := c.Series(nil, time.Now(), time.Now(), true)
	require.NoError(t, err)

	exp := &loghttp.SeriesResponse{
		Data: []loghttp.LabelSet{
			{defaultLabelKey: defaultLabelValue},
		},
		Status: loghttp.QueryStatusSuccess,
	}

	assert.Equal(t, exp, got)
}

func TestFileClient_LiveTail(t *testing.T) {
	c := newEmptyClient(t)
	x, err := c.LiveTailQueryConn("", time.Second, 0, time.Now(), true)
	require.Error(t, err)
	require.Nil(t, x)
	assert.True(t, errors.Is(err, ErrNotSupported))
}

func TestFileClient_GetOrgID(t *testing.T) {
	c := newEmptyClient(t)
	assert.Equal(t, defaultOrgID, c.GetOrgID())
}

func newEmptyClient(t *testing.T) *FileClient {
	t.Helper()
	return NewFileClient(io.NopCloser(&bytes.Buffer{}))
}
