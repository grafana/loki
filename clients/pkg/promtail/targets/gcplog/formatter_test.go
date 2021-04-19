package gcplog

import (
	"testing"
	"time"

	"cloud.google.com/go/pubsub"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/relabel"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/clients/pkg/promtail/api"

	"github.com/grafana/loki/pkg/logproto"
)

func TestFormat(t *testing.T) {
	cases := []struct {
		name                 string
		msg                  *pubsub.Message
		labels               model.LabelSet
		relabel              []*relabel.Config
		useIncomingTimestamp bool
		expected             api.Entry
	}{
		{
			name: "relabelling",
			msg: &pubsub.Message{
				Data: []byte(withAllFields),
			},
			labels: model.LabelSet{
				"jobname": "pubsub-test",
			},
			relabel: []*relabel.Config{
				{
					SourceLabels: model.LabelNames{"__backend_service_name"},
					Separator:    ";",
					Regex:        relabel.MustNewRegexp("(.*)"),
					TargetLabel:  "backend_service_name",
					Action:       "replace",
					Replacement:  "$1",
				},
				{
					SourceLabels: model.LabelNames{"__bucket_name"},
					Separator:    ";",
					Regex:        relabel.MustNewRegexp("(.*)"),
					TargetLabel:  "bucket_name",
					Action:       "replace",
					Replacement:  "$1",
				},
			},
			useIncomingTimestamp: true,
			expected: api.Entry{
				Labels: model.LabelSet{
					"jobname":              "pubsub-test",
					"resource_type":        "gcs",
					"backend_service_name": "http-loki",
					"bucket_name":          "loki-bucket",
					"promtail_instance":    model.LabelValue(instanceID.String()),
				},
				Entry: logproto.Entry{
					Timestamp: mustTime(t, "2020-12-22T15:01:23.045123456Z"),
					Line:      withAllFields,
				},
			},
		},
		{
			name: "use-original-timestamp",
			msg: &pubsub.Message{
				Data: []byte(withAllFields),
			},
			labels: model.LabelSet{
				"jobname": "pubsub-test",
			},
			useIncomingTimestamp: true,
			expected: api.Entry{
				Labels: model.LabelSet{
					"jobname":           "pubsub-test",
					"resource_type":     "gcs",
					"promtail_instance": model.LabelValue(instanceID.String()),
				},
				Entry: logproto.Entry{
					Timestamp: mustTime(t, "2020-12-22T15:01:23.045123456Z"),
					Line:      withAllFields,
				},
			},
		},
		{
			name: "rewrite-timestamp",
			msg: &pubsub.Message{
				Data: []byte(withAllFields),
			},
			labels: model.LabelSet{
				"jobname": "pubsub-test",
			},
			expected: api.Entry{
				Labels: model.LabelSet{
					"jobname":           "pubsub-test",
					"resource_type":     "gcs",
					"promtail_instance": model.LabelValue(instanceID.String()),
				},
				Entry: logproto.Entry{
					Timestamp: time.Now(),
					Line:      withAllFields,
				},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := format(c.msg, c.labels, c.useIncomingTimestamp, c.relabel)

			require.NoError(t, err)

			assert.Equal(t, c.expected.Labels, got.Labels)
			assert.Equal(t, c.expected.Line, got.Line)
			if c.useIncomingTimestamp {
				assert.Equal(t, c.expected.Entry.Timestamp, got.Timestamp)
			} else {
				if got.Timestamp.Sub(c.expected.Timestamp).Seconds() > 1 {
					assert.Fail(t, "timestamp shouldn't differ much when rewriting log entry timestamp.")
				}
			}
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
	withAllFields = `{"logName": "https://project/gcs", "resource": {"type": "gcs", "labels": {"backendServiceName": "http-loki", "bucketName": "loki-bucket", "instanceId": "344555"}}, "timestamp": "2020-12-22T15:01:23.045123456Z"}`
)
