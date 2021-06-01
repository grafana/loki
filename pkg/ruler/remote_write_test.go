package ruler

import (
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/cortexpb"
	"github.com/golang/snappy"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/stretchr/testify/require"
)

func TestPrepareRequest(t *testing.T) {
	ctx := createOriginContext("/rule/file", "rule-group")
	appendable := createBasicAppendable(10)

	appender := appendable.Appender(ctx).(*RemoteWriteAppender)
	client, err := newRemoteWriter(appendable.cfg, "fake")
	require.Nil(t, err)

	appender.remoteWriter = client

	lbs := labels.Labels{
		labels.Label{
			Name:  "cluster",
			Value: "us-central1",
		},
	}
	sample := cortexpb.Sample{
		Value:       70,
		TimestampMs: time.Now().Unix(),
	}

	appender.Append(0, lbs, sample.TimestampMs, sample.Value) //nolint:errcheck

	bytes, err := appender.remoteWriter.PrepareRequest(appender.queue)
	require.Nil(t, err)

	var req cortexpb.WriteRequest

	reqBytes, err := snappy.Decode(nil, bytes)
	require.Nil(t, err)

	require.Nil(t, req.Unmarshal(reqBytes))

	require.Equal(t, req.Timeseries[0].Labels[0].Name, lbs[0].Name)
	require.Equal(t, req.Timeseries[0].Labels[0].Value, lbs[0].Value)
	require.Equal(t, req.Timeseries[0].Samples[0], sample)
}
