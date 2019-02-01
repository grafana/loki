package client

import (
	"context"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/wire"
	"github.com/stretchr/testify/require"
)

// TestMarshall is useful to try out various optimisation on the unmarshalling code.
func TestMarshall(t *testing.T) {
	recorder := httptest.NewRecorder()
	{
		req := WriteRequest{}
		for i := 0; i < 10; i++ {
			req.Timeseries = append(req.Timeseries, PreallocTimeseries{
				TimeSeries{
					Labels: []LabelPair{
						{wire.Bytes([]byte("foo")), wire.Bytes([]byte(strconv.Itoa(i)))},
					},
					Samples: []Sample{
						{TimestampMs: int64(i), Value: float64(i)},
					},
				},
			})
		}
		err := util.SerializeProtoResponse(recorder, &req, util.RawSnappy)
		require.NoError(t, err)
	}

	{
		req := WriteRequest{}
		_, err := util.ParseProtoReader(context.Background(), recorder.Body, &req, util.RawSnappy)
		require.NoError(t, err)
	}
}
