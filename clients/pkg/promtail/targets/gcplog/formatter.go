package gcplog

import (
	"fmt"
	"strings"
	"time"

	json "github.com/json-iterator/go"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/model/relabel"

	"github.com/grafana/loki/v3/clients/pkg/promtail/api"

	"github.com/grafana/loki/v3/pkg/logproto"
)

// GCPLogEntry that will be written to the pubsub topic.
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

	// Optional. The severity of the log entry. The default value is DEFAULT.
	// DEFAULT, DEBUG, INFO, NOTICE, WARNING, ERROR, CRITICAL, ALERT, EMERGENCY
	// https://cloud.google.com/logging/docs/reference/v2/rest/v2/LogEntry#LogSeverity
	Severity string `json:"severity"`

	// Optional. A map of key, value pairs that provides additional information about the log entry.
	// The labels can be user-defined or system-defined.
	Labels map[string]string `json:"labels"`

	TextPayload string `json:"textPayload"`

	// NOTE(kavi): There are other fields on GCPLogEntry. but we need only need above fields for now
	// anyway we will be sending the entire entry to Loki.
}

func parseGCPLogsEntry(data []byte, other model.LabelSet, otherInternal labels.Labels, useIncomingTimestamp, useFullLine bool, relabelConfig []*relabel.Config) (api.Entry, error) {
	var ge GCPLogEntry

	if err := json.Unmarshal(data, &ge); err != nil {
		return api.Entry{}, err
	}

	// Mixin with otherInternal labels coming from upstream that need processing
	// Adding mandatory labels for gcplog
	lbs := labels.NewBuilder(otherInternal)
	lbs.Set("__gcp_logname", ge.LogName)
	lbs.Set("__gcp_resource_type", ge.Resource.Type)
	lbs.Set("__gcp_severity", ge.Severity)

	// resource labels from gcp log entry. Add it as internal labels
	for k, v := range ge.Resource.Labels {
		lbs.Set("__gcp_resource_labels_"+convertToLokiCompatibleLabel(k), v)
	}

	// labels from gcp log entry. Add it as internal labels
	for k, v := range ge.Labels {
		lbs.Set("__gcp_labels_"+convertToLokiCompatibleLabel(k), v)
	}

	var processed labels.Labels

	// apply relabeling
	if len(relabelConfig) > 0 {
		processed, _ = relabel.Process(lbs.Labels(), relabelConfig...)
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
	line := string(data)

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

	// Send only `ge.textPayload` as log line if its present and user don't explicitly ask for the whole log.
	if !useFullLine && strings.TrimSpace(ge.TextPayload) != "" {
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
