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

func format(m *pubsub.Message) (api.Entry, error) {
	var ge GCPLogEntry

	if err := json.Unmarshal(m.Data, &ge); err != nil {
		return api.Entry{}, err
	}

	// may this labels combination can lead to unique stream without timestamp conflic or out-of-order.
	// TODO(kavi): Should I add logname as label?
	labels := model.LabelSet{
		"logName":      model.LabelValue(ge.LogName),
		"resourceType": model.LabelValue(ge.Resource.Type),
	}
	for k, v := range ge.Resource.Labels {
		labels[model.LabelName(k)] = model.LabelValue(v)
	}

	t := ge.Timestamp
	if t == "" {
		t = ge.ReceiveTimestamp
	}

	ts, err := time.Parse(time.RFC3339, t)
	if err != nil {
		return api.Entry{}, fmt.Errorf("invalid timestamp format: %w", err)
	}

	if ts.IsZero() {
		return api.Entry{}, fmt.Errorf("no timestamp found in the log")
	}

	return api.Entry{
		Labels: labels,
		Entry: logproto.Entry{
			Timestamp: ts,
			Line:      string(m.Data),
		},
	}, nil
}
