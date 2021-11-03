package gcplog

import (
	"fmt"
	"strings"
	"time"

	"cloud.google.com/go/pubsub"
	json "github.com/json-iterator/go"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/pkg/relabel"

	"github.com/grafana/loki/clients/pkg/promtail/api"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/util"
)

// LogEntry that will be written to the pubsub topic.
// According to the following spec.
// https://cloud.google.com/logging/docs/reference/v2/rest/v2/LogEntry
// nolint:revive
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

	TextPayload string `json:"textPayload"`

	// NOTE(kavi): There are other fields on GCPLogEntry. but we need only need above fields for now
	// anyway we will be sending the entire entry to Loki.
}

func format(
	m *pubsub.Message,
	other model.LabelSet,
	useIncomingTimestamp bool,
	relabelConfig []*relabel.Config,
) (api.Entry, error) {
	var ge GCPLogEntry

	if err := json.Unmarshal(m.Data, &ge); err != nil {
		return api.Entry{}, err
	}

	// mandatory label for gcplog
	lbs := labels.NewBuilder(nil)
	lbs.Set("resource_type", ge.Resource.Type)

	// labels from gcp log entry. Add it as internal labels
	for k, v := range ge.Resource.Labels {
		lbs.Set("__"+util.SnakeCase(k), v)
	}

	var processed labels.Labels

	// apply relabeling
	if len(relabelConfig) > 0 {
		processed = relabel.Process(lbs.Labels(), relabelConfig...)
	} else {
		processed = lbs.Labels()
	}

	// final labelset that will be sent to loki
	labels := make(model.LabelSet)
	for _, lbl := range processed {
		// ignore internal labels
		if strings.HasPrefix(lbl.Name, "__") {
			continue
		}
		// ignore invalid labels
		if !model.LabelName(lbl.Name).IsValid() || !model.LabelValue(lbl.Value).IsValid() {
			continue
		}
		labels[model.LabelName(lbl.Name)] = model.LabelValue(lbl.Value)
	}

	// add labels coming from scrapeconfig
	labels = labels.Merge(other)

	ts := time.Now()
	line := string(m.Data)

	if useIncomingTimestamp {
		tt := ge.Timestamp
		if tt == "" {
			tt = ge.ReceiveTimestamp
		}
		var err error
		ts, err = time.Parse(time.RFC3339, tt)
		if err != nil {
			return api.Entry{}, fmt.Errorf("invalid timestamp format: %w", err)
		}

		if ts.IsZero() {
			return api.Entry{}, fmt.Errorf("no timestamp found in the log entry")
		}
	}

	// Send only `ge.textPaylload` as log line if its present.
	if strings.TrimSpace(ge.TextPayload) != "" {
		line = ge.TextPayload
	}

	return api.Entry{
		Labels: labels,
		Entry: logproto.Entry{
			Timestamp: ts,
			Line:      line,
		},
	}, nil
}
