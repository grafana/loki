package labelquery

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logcli/client"
	"github.com/grafana/loki/v3/pkg/loghttp"
)

type testLabelClient struct {
	client.Client
	response *loghttp.LabelResponse
	err      error
}

func (c *testLabelClient) ListLabelNames(_ bool, _, _ time.Time) (*loghttp.LabelResponse, error) {
	return c.response, c.err
}

func (c *testLabelClient) ListLabelValues(_ string, _ bool, _, _ time.Time) (*loghttp.LabelResponse, error) {
	return c.response, c.err
}

func TestLabelQueryListLabels(t *testing.T) {
	expected := []string{"app", "cluster"}
	q := &LabelQuery{}

	labels, err := q.ListLabels(&testLabelClient{response: &loghttp.LabelResponse{Data: expected}})
	require.NoError(t, err)
	require.Equal(t, expected, labels)

	expectedErr := errors.New("list labels failed")
	_, err = q.ListLabels(&testLabelClient{err: expectedErr})
	require.ErrorIs(t, err, expectedErr)
}

func TestLabelQueryDoLabels(t *testing.T) {
	q := &LabelQuery{LabelName: "app"}

	require.NoError(t, q.DoLabels(&testLabelClient{response: &loghttp.LabelResponse{Data: []string{"loki"}}}))

	expectedErr := errors.New("list label values failed")
	require.ErrorIs(t, q.DoLabels(&testLabelClient{err: expectedErr}), expectedErr)
}
