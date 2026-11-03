package kfake

import (
	"github.com/twmb/franz-go/pkg/kmsg"
)

// GetTelemetrySubscriptions: v0
//
// KIP-714: Client metrics and observability.
//
// Behavior:
// * No ACL checks (telemetry uses subscription-based validation)
// * Assigns ClientInstanceID if not provided
// * Returns subscription configuration for client metrics collection
// * Tracks client instances for PushTelemetry validation
//
// Version notes:
// * v0: Initial version

func init() { regKey(71, 0, 0) }

func (c *Cluster) handleGetTelemetrySubscriptions(creq *clientReq) (kmsg.Response, error) {
	req := creq.kreq.(*kmsg.GetTelemetrySubscriptionsRequest)
	resp := req.ResponseKind().(*kmsg.GetTelemetrySubscriptionsResponse)

	if err := c.checkReqVersion(req.Key(), req.Version); err != nil {
		return nil, err
	}

	clientID := req.ClientInstanceID
	if clientID == [16]byte{} {
		clientID = randUUID()
	}

	subID, ok := c.telem[clientID]
	if !ok {
		c.telemNextID++
		subID = c.telemNextID
		c.telem[clientID] = subID
	}

	resp.ClientInstanceID = clientID
	resp.SubscriptionID = subID
	resp.AcceptedCompressionTypes = []int8{0, 1, 2, 3, 4} // none, gzip, snappy, lz4, zstd
	resp.PushIntervalMillis = 30000                       // 30 seconds
	resp.TelemetryMaxBytes = 1048576                      // 1MB
	resp.DeltaTemporality = true                          // Use delta metrics
	resp.RequestedMetrics = []string{""}                  // Subscribe to all metrics

	return resp, nil
}
