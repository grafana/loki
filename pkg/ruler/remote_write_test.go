package ruler

import (
	"math/rand"
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/cortexpb"
	"github.com/golang/snappy"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/util"
)

func TestPrepare(t *testing.T) {
	client := RemoteWriteClient{}
	queue, err := util.NewEvictingQueue(1000, func() {})
	require.Nil(t, err)

	lbs := labels.Labels{
		labels.Label{
			Name:  "cluster",
			Value: "us-central1",
		},
	}
	sample := cortexpb.Sample{
		Value:       rand.Float64(),
		TimestampMs: time.Now().Unix(),
	}

	// first start with 10 items
	for i := 0; i < 10; i++ {
		queue.Append(queueEntry{labels: lbs, sample: sample})
	}
	require.Nil(t, client.prepare(queue))

	assert.Equal(t, len(client.labels), queue.Length())
	assert.Equal(t, len(client.samples), queue.Length())
	assert.Equal(t, cap(client.labels), 10)
	assert.Equal(t, cap(client.samples), 10)

	queue.Clear()

	// then resize the internal slices to 100
	for i := 0; i < 100; i++ {
		queue.Append(queueEntry{labels: lbs, sample: sample})
	}
	require.Nil(t, client.prepare(queue))

	assert.Equal(t, len(client.labels), queue.Length())
	assert.Equal(t, len(client.samples), queue.Length())
	assert.Equal(t, cap(client.labels), 100)
	assert.Equal(t, cap(client.samples), 100)

	queue.Clear()

	// then reuse the existing slice (no resize necessary since 5 < 100)
	for i := 0; i < 5; i++ {
		queue.Append(queueEntry{labels: lbs, sample: sample})
	}
	require.Nil(t, client.prepare(queue))

	assert.Equal(t, len(client.labels), queue.Length())
	assert.Equal(t, len(client.samples), queue.Length())
	// cap remains 100 since no resize was necessary
	assert.Equal(t, cap(client.labels), 100)
	assert.Equal(t, cap(client.samples), 100)
}

func TestPrepareRequest(t *testing.T) {
	appender := createBasicAppender(t)

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

func createBasicAppender(t *testing.T) *RemoteWriteAppender {
	ctx := createOriginContext("/rule/file", "rule-group")
	appendable := createBasicAppendable(100)

	appender := appendable.Appender(ctx).(*RemoteWriteAppender)
	client, err := NewRemoteWriter(appendable.cfg, "fake")
	require.Nil(t, err)

	appender.remoteWriter = client
	return appender
}
