package ruler

import (
	"math/rand"
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/cortexpb"
	"github.com/golang/snappy"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/config"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/pkg/relabel"
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
		queue.Append(TimeSeriesEntry{Labels: lbs, Sample: sample})
	}
	require.NoError(t, client.prepare(queue))

	assert.Equal(t, len(client.labels), queue.Length())
	assert.Equal(t, len(client.samples), queue.Length())
	assert.Equal(t, cap(client.labels), 10)
	assert.Equal(t, cap(client.samples), 10)

	queue.Clear()

	// then resize the internal slices to 100
	for i := 0; i < 100; i++ {
		queue.Append(TimeSeriesEntry{Labels: lbs, Sample: sample})
	}
	require.NoError(t, client.prepare(queue))

	assert.Equal(t, len(client.labels), queue.Length())
	assert.Equal(t, len(client.samples), queue.Length())
	assert.Equal(t, cap(client.labels), 100)
	assert.Equal(t, cap(client.samples), 100)

	queue.Clear()

	// then reuse the existing slice (no resize necessary since 5 < 100)
	for i := 0; i < 5; i++ {
		queue.Append(TimeSeriesEntry{Labels: lbs, Sample: sample})
	}
	require.NoError(t, client.prepare(queue))

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

func TestRelabelling(t *testing.T) {
	lbs := labels.FromStrings("cluster", "us-central1")

	queue, err := util.NewEvictingQueue(1000, func() {})
	require.Nil(t, err)

	queue.Append(TimeSeriesEntry{Labels: lbs, Sample: cortexpb.Sample{
		Value:       rand.Float64(),
		TimestampMs: time.Now().Unix(),
	}})

	tests := []struct {
		name           string
		relabelConfigs []*relabel.Config
		expectedLabels labels.Labels
	}{
		{
			name: "add a label",
			relabelConfigs: []*relabel.Config{
				{
					Replacement: "bar",
					TargetLabel: "foo",
				},
			},
			expectedLabels: append(lbs, labels.FromStrings("foo", "bar")...),
		},
		{
			name: "remove a label",
			relabelConfigs: []*relabel.Config{
				{
					SourceLabels: model.LabelNames{"cluster"},
					Action:       relabel.LabelDrop,
					Regex:        relabel.MustNewRegexp(".+"),
				},
			},
			expectedLabels: labels.Labels{},
		},
		{
			name:           "no relabel configs defined",
			expectedLabels: lbs,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setting default fields, mimicking the behaviour in Prometheus
			// see https://github.com/prometheus/prometheus/blob/main/pkg/relabel/relabel_test.go#L434
			for _, cfg := range tt.relabelConfigs {
				if cfg.Action == "" {
					cfg.Action = relabel.DefaultRelabelConfig.Action
				}
				if cfg.Separator == "" {
					cfg.Separator = relabel.DefaultRelabelConfig.Separator
				}
				if cfg.Regex.Regexp == nil {
					cfg.Regex = relabel.DefaultRelabelConfig.Regex
				}
				if cfg.Replacement == "" {
					cfg.Replacement = relabel.DefaultRelabelConfig.Replacement
				}
			}

			client, err := NewRemoteWriter(Config{
				RemoteWrite: RemoteWriteConfig{
					Client: config.RemoteWriteConfig{
						WriteRelabelConfigs: tt.relabelConfigs,
					},
					Enabled: true,
				},
			}, "fake")
			assert.NoError(t, err)

			writer := client.(*RemoteWriteClient)
			require.NoError(t, writer.prepare(queue))

			assert.Equal(t, tt.expectedLabels, writer.labels[0])
		})
	}
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
