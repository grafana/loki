package gcppush

import (
	"encoding/base64"
	"fmt"
	lokiClient "github.com/grafana/loki/clients/pkg/promtail/client"
	"strings"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/model/relabel"

	"github.com/grafana/loki/clients/pkg/promtail/api"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/util"
)

// PushMessage is the POST body format sent by GCP PubSub push subscriptions.
type PushMessage struct {
	Message struct {
		Attributes       map[string]string `json:"attributes"`
		Data             string            `json:"data"`
		ID               string            `json:"message_id"`
		PublishTimestamp string            `json:"publish_time"`
	} `json:"message"`
	Subscription string `json:"subscription"`
}

func format(m PushMessage, other model.LabelSet, useIncomingTimestamp bool, relabelConfigs []*relabel.Config, xScopeOrgID string) (api.Entry, error) {
	// mandatory label for gcplog
	lbs := labels.NewBuilder(nil)
	lbs.Set("__gcp_message_id", m.Message.ID)

	// labels from gcp log entry. Add it as internal labels
	for k, v := range m.Message.Attributes {
		lbs.Set(fmt.Sprintf("__gcp_attributes_%s", util.SnakeCase(k)), v)
	}

	var processed labels.Labels

	// apply relabeling
	if len(relabelConfigs) > 0 {
		processed = relabel.Process(lbs.Labels(), relabelConfigs...)
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

	decodedData, err := base64.StdEncoding.DecodeString(m.Message.Data)
	if err != nil {
		return api.Entry{}, fmt.Errorf("failed to decode data: %w", err)
	}
	line := string(decodedData)

	if useIncomingTimestamp {
		var err error
		ts, err = time.Parse(time.RFC3339, m.Message.PublishTimestamp)
		if err != nil {
			return api.Entry{}, fmt.Errorf("invalid publish timestamp format: %w", err)
		}
	}

	// If the incoming request carries the tenant id, inject it as the reserved label so it's used by the
	// remote write client.
	if xScopeOrgID != "" {
		labels[lokiClient.ReservedLabelTenantID] = model.LabelValue(xScopeOrgID)
	}

	return api.Entry{
		Labels: labels,
		Entry: logproto.Entry{
			Timestamp: ts,
			Line:      line,
		},
	}, nil
}
