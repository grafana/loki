package gcplog

import (
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/model/relabel"

	"github.com/grafana/loki/v3/clients/pkg/promtail/api"
	lokiClient "github.com/grafana/loki/v3/clients/pkg/promtail/client"

	"github.com/grafana/loki/v3/pkg/util"
)

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

func (pm PushMessage) Validate() error {
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

// translate converts a GCP PushMessage into a loki api.Entry. It parses the push-specific labels, and delegates the rest to parseGCPLogsEntry.
func translate(m PushMessage, other model.LabelSet, useIncomingTimestamp, useFullLine bool, relabelConfigs []*relabel.Config, xScopeOrgID string) (api.Entry, error) {
	// Collect all push-specific labels. Every one of them is first configured as optional, and the user
	// can relabel it if needed. The relabeling and internal drop is handled in parseGCPLogsEntry.
	lbs := labels.NewBuilder(nil)
	lbs.Set("__gcp_message_id", m.Message.ID)
	lbs.Set("__gcp_subscription_name", m.Subscription)
	for k, v := range m.Message.Attributes {
		lbs.Set(fmt.Sprintf("__gcp_attributes_%s", convertToLokiCompatibleLabel(k)), v)
	}

	// Add fixed labels coming from the target configuration
	fixedLabels := other.Clone()

	// If the incoming request carries the tenant id, inject it as the reserved label, so it's used by the
	// remote write client.
	if xScopeOrgID != "" {
		// Expose tenant ID through relabel to use as logs or metrics label
		lbs.Set(lokiClient.ReservedLabelTenantID, xScopeOrgID)
		fixedLabels[lokiClient.ReservedLabelTenantID] = model.LabelValue(xScopeOrgID)
	}

	decodedData, err := base64.StdEncoding.DecodeString(m.Message.Data)
	if err != nil {
		return api.Entry{}, fmt.Errorf("failed to decode data: %w", err)
	}

	entry, err := parseGCPLogsEntry(decodedData, fixedLabels, lbs.Labels(), useIncomingTimestamp, useFullLine, relabelConfigs)
	if err != nil {
		return api.Entry{}, fmt.Errorf("failed to parse logs entry: %w", err)
	}

	return entry, nil
}

var separatorCharacterReplacer = strings.NewReplacer(".", "_", "-", "_", "/", "_")

// convertToLokiCompatibleLabel converts an incoming GCP Push message label to a loki compatible format. There are labels
// such as `logging.googleapis.com/timestamp`, which contain non-loki-compatible characters, which is just alphanumeric
// and _. The approach taken is to translate every non-alphanumeric separator character to an underscore.
func convertToLokiCompatibleLabel(label string) string {
	return util.SnakeCase(separatorCharacterReplacer.Replace(label))
}
