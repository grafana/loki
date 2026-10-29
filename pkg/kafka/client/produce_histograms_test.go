package client

import (
	"errors"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// gatherHistograms returns the gathered histograms by metric name.
func gatherHistograms(t *testing.T, reg *prometheus.Registry) map[string][]*dto.Metric {
	t.Helper()
	families, err := reg.Gather()
	require.NoError(t, err)
	result := map[string][]*dto.Metric{}
	for _, f := range families {
		if f.GetType() == dto.MetricType_HISTOGRAM {
			result[f.GetName()] = f.GetMetric()
		}
	}
	return result
}

func labelsOf(m *dto.Metric) map[string]string {
	labels := map[string]string{}
	for _, l := range m.GetLabel() {
		labels[l.GetName()] = l.GetValue()
	}
	return labels
}

func TestProduceHistograms_OnBrokerE2E(t *testing.T) {
	meta := kgo.BrokerMetadata{NodeID: 3, Host: "broker-3.example", Port: 9092}
	e2e := kgo.BrokerE2E{
		WriteWait:   1 * time.Millisecond,
		TimeToWrite: 2 * time.Millisecond,
		ReadWait:    3 * time.Millisecond,
		TimeToRead:  4 * time.Millisecond,
	}

	tests := []struct {
		name          string
		key           int16
		writeErr      error
		readErr       error
		expectedCount map[string]uint64
	}{
		{
			name: "successful produce observes every stage",
			key:  int16(kmsg.Produce),
			expectedCount: map[string]uint64{
				"loki_kafka_client_produce_write_wait_seconds": 1,
				"loki_kafka_client_produce_write_time_seconds": 1,
				"loki_kafka_client_produce_read_wait_seconds":  1,
				"loki_kafka_client_produce_read_time_seconds":  1,
			},
		},
		{
			name:          "write error observes nothing",
			key:           int16(kmsg.Produce),
			writeErr:      errors.New("write failed"),
			expectedCount: map[string]uint64{},
		},
		{
			name:    "read error observes only the write stages",
			key:     int16(kmsg.Produce),
			readErr: errors.New("read failed"),
			expectedCount: map[string]uint64{
				"loki_kafka_client_produce_write_wait_seconds": 1,
				"loki_kafka_client_produce_write_time_seconds": 1,
			},
		},
		{
			name:          "other requests are ignored",
			key:           int16(kmsg.Metadata),
			expectedCount: map[string]uint64{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg := prometheus.NewRegistry()
			h := newProduceHistograms(WrapPrometheusRegisterer("test", reg))
			h.OnNewClient(nil)

			e2e := e2e
			e2e.WriteErr = tt.writeErr
			e2e.ReadErr = tt.readErr
			h.OnBrokerE2E(meta, tt.key, e2e)

			histograms := gatherHistograms(t, reg)
			require.Len(t, histograms, len(tt.expectedCount))
			for name, count := range tt.expectedCount {
				require.Len(t, histograms[name], 1, name)
				m := histograms[name][0]
				require.Equal(t, map[string]string{"component": "test", "node_id": "3", "host": "broker-3.example"}, labelsOf(m))
				require.Equal(t, count, m.GetHistogram().GetSampleCount(), name)
				require.NotNil(t, m.GetHistogram().Schema, name)
				require.NotEmpty(t, m.GetHistogram().GetPositiveSpan(), name)
				require.Empty(t, m.GetHistogram().GetBucket(), name) // not a classic histogram
			}
		})
	}
}
