package gcplog

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/model/relabel"

	"github.com/grafana/loki/clients/pkg/promtail/api"
	lokiClient "github.com/grafana/loki/clients/pkg/promtail/client"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/util"
)

// Configured as a global in this file to avoid recompiling this regex everywhere.
var labelToLokiCompatible *regexp.Regexp

func init() {
	// TODO: Maybe use a regexp negative filter and grab everything non-alphanumeric non-underscore?
	labelToLokiCompatible = regexp.MustCompile("[.-/]")
}

// PushMessage is the POST body format sent by GCP PubSub push subscriptions.
// See https://cloud.google.com/pubsub/docs/push for details.
type PushMessage struct {
	Message struct {
		Attributes       map[string]string `json:"attributes"`
		Data             string            `json:"data"`
		ID               string            `json:"message_id"`
		PublishTimestamp string            `json:"publish_time"`
	} `json:"message"`
	Subscription string `json:"subscription"`
}

func (pm *PushMessage) Validate() error {
	if pm.Message.Data == "" {
		return fmt.Errorf("push message has no data")
	}
	if pm.Message.ID == "" {
		return fmt.Errorf("push message has no ID")
	}
	if pm.Subscription == "" {
		return fmt.Errorf("push message has no subscription")
	}
	return nil
}

// translate converts a GCP PushMessage into a loki api.Entry. It is responsible for decoding the log line (contained in the Message.Data)
// attribute, using the incoming timestamp if necessary, and formatting and passing down the incoming Message.Attributes
// if relabel is configured.
func translate(m PushMessage, other model.LabelSet, useIncomingTimestamp bool, relabelConfigs []*relabel.Config, xScopeOrgID string) (api.Entry, error) {
	// mandatory label for gcplog
	lbs := labels.NewBuilder(nil)
	lbs.Set("__gcp_message_id", m.Message.ID)
	lbs.Set("__gcp_subscription_name", m.Subscription)

	// labels from gcp log entry. Add it as internal labels
	for k, v := range m.Message.Attributes {
		lbs.Set(fmt.Sprintf("__gcp_attributes_%s", convertToLokiCompatibleLabel(k)), v)
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

	// If the incoming request carries the tenant id, inject it as the reserved label, so it's used by the
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

// convertToLokiCompatibleLabel converts an incoming GCP Push message label to a loki compatible format. There are labels
// such as `logging.googleapis.com/timestamp`, which contain non-loki-compatible characters, which is just alphanumeric
// and _. The approach taken is to translate every non-alphanumeric separator character to an underscore.
func convertToLokiCompatibleLabel(label string) string {
	return util.SnakeCase(convertSeparatorCharacterToUnderscore(label))
}

func convertSeparatorCharacterToUnderscore(s string) string {
	var buf bytes.Buffer
	for _, r := range s {
		// The condition in the if statement matches the following regex [.-/]
		if r == '.' || r == '-' || r == '/' {
			fmt.Fprintf(&buf, "_")
		} else {
			fmt.Fprintf(&buf, "%c", r)
		}
	}
	return buf.String()
}
