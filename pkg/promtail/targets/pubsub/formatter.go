package pubsub

import (
	"encoding/json"
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
	Timestamp time.Time `json:"timestamp"`

	// TODO(kavi): Add other meta data fields as well.

	// payload can be one of the following
	// ignoring protopayload for now.
	TextPayload string `json:"textPayload,omitempty"`

	// TODO(kavi): push it as just string?
	JSONPayload string `json:"jsonPayload,omitempty"`
}

func format(m *pubsub.Message) (api.Entry, error) {
	var ge GCPLogEntry

	if err := json.Unmarshal(m.Data, &ge); err != nil {
		return api.Entry{}, err
	}

	var payload string

	if ge.TextPayload == "" {
		payload = ge.JSONPayload
	}

	return api.Entry{
		Labels: model.LabelSet{"source": "cloudtail"},
		Entry: logproto.Entry{
			Timestamp: ge.Timestamp,
			Line:      payload,
		},
	}, nil
}
