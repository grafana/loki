package pubsub

import (
	"encoding/json"
	"fmt"
	"time"

	"cloud.google.com/go/pubsub"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/promtail/api"
	"github.com/prometheus/common/model"
)

// LogEntry that will be written to the pubsub topic.
// According to the following spec.
// https://cloud.google.com/logging/docs/reference/v2/rest/v2/LogEntry
type GCPLogEntry struct {
	LogName  string `json:"logName"`
	Resource struct {
		Type   string            `json:"type"`
		Labels map[string]string `json:"labels"`
	} `json:"resource"`
	Timestamp string `json:"timestamp"`

	// The time the log entry was received by Logging.
	// Its important that `Timestamp` is optional in GCE log entry.
	ReceiveTimestamp string `json:"receiveTimestamp"`

	// TODO(kavi): Add other meta data fields as well if needed.
}

func (t *PubsubTarget) format(m *pubsub.Message) (api.Entry, error) {
	var ge GCPLogEntry

	if err := json.Unmarshal(m.Data, &ge); err != nil {
		return api.Entry{}, err
	}

	labels := model.LabelSet{
		"logName":      model.LabelValue(ge.LogName),
		"resourceType": model.LabelValue(ge.Resource.Type),
	}
	for k, v := range ge.Resource.Labels {
		labels[model.LabelName(k)] = model.LabelValue(v)
	}

	// add labes from config as well.
	labels = labels.Merge(t.config.Labels)

	ts := ge.Timestamp
	if ts == "" {
		ts = ge.ReceiveTimestamp
	}

	tt, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return api.Entry{}, fmt.Errorf("invalid timestamp format: %w", err)
	}

	if tt.IsZero() {
		return api.Entry{}, fmt.Errorf("no timestamp found in the log")
	}

	return api.Entry{
		Labels: labels,
		Entry: logproto.Entry{
			Timestamp: time.Now(), // rewrite timestamp to avoid out-of-order
			Line:      string(m.Data),
		},
	}, nil
}
