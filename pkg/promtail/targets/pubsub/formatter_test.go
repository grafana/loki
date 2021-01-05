package pubsub

import (
	"testing"
	"time"

	"cloud.google.com/go/pubsub"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/promtail/api"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormat(t *testing.T) {
	cases := []struct {
		name     string
		msg      *pubsub.Message
		labels   model.LabelSet
		expected api.Entry
	}{
		{
			name: "with all fields",
			msg: &pubsub.Message{
				Data: []byte(withAllFields),
			},
			labels: model.LabelSet{
				"jobname": "pubsub-test",
			},
			expected: api.Entry{
				Labels: model.LabelSet{
					"jobname":      "pubsub-test",
					"logName":      "https://project/gcs",
					"resourceType": "gcs",
					"instanceId":   "344555",
				},
				Entry: logproto.Entry{
					Timestamp: mustTime(t, "2020-12-22T15:01:23.045123456Z"),
					Line:      withAllFields,
				},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := format(c.msg, c.labels)

			require.NoError(t, err)

			assert.Equal(t, c.expected.Labels, got.Labels)
			assert.Equal(t, c.expected.Line, got.Line)

		})
	}
}

func mustTime(t *testing.T, v string) time.Time {
	t.Helper()

	ts, err := time.Parse(time.RFC3339, v)
	require.NoError(t, err)
	return ts
}

const (
	withAllFields = `{"logName": "https://project/gcs", "resource": {"type": "gcs", "labels": {"instanceId": "344555"}}, "timestamp": "2020-12-22T15:01:23.045123456Z"}`
)
