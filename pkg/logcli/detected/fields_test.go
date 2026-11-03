package detected

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logcli/client"
	"github.com/grafana/loki/v3/pkg/loghttp"
)

type testFieldsClient struct {
	client.Client
	response *loghttp.DetectedFieldsResponse
	err      error
}

func (c *testFieldsClient) GetDetectedFields(
	_, _ string,
	_, _ int,
	_, _ time.Time,
	_ time.Duration,
	_ bool,
) (*loghttp.DetectedFieldsResponse, error) {
	return c.response, c.err
}

func TestFieldsQueryDo(t *testing.T) {
	q := &FieldsQuery{QueryString: `{app="loki"}`}

	require.NoError(t, q.Do(&testFieldsClient{response: &loghttp.DetectedFieldsResponse{Values: []string{"info"}}}, "default"))

	expectedErr := errors.New("get detected fields failed")
	err := q.Do(&testFieldsClient{err: expectedErr}, "default")
	require.ErrorIs(t, err, expectedErr)
	require.ErrorContains(t, err, "error doing request")
}
