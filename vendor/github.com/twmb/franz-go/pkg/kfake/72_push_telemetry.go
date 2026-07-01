package kfake

import (
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// PushTelemetry: v0
//
// KIP-714: Client metrics and observability.
//
// Behavior:
// * No ACL checks (telemetry uses subscription-based validation)
// * Validates ClientInstanceID is not zero/reserved
// * Validates SubscriptionID matches the assigned subscription
// * Accepts and discards metrics (no actual processing)
// * Removes client instance if Terminating flag is set
//
// Version notes:
// * v0: Initial version

func init() { regKey(72, 0, 0) }

func (c *Cluster) handlePushTelemetry(creq *clientReq) (kmsg.Response, error) {
	req := creq.kreq.(*kmsg.PushTelemetryRequest)
	resp := req.ResponseKind().(*kmsg.PushTelemetryResponse)

	if err := c.checkReqVersion(req.Key(), req.Version); err != nil {
		return nil, err
	}

	if req.ClientInstanceID == [16]byte{} {
		resp.ErrorCode = kerr.InvalidRequest.Code
		return resp, nil
	}

	subID, ok := c.telem[req.ClientInstanceID]
	if !ok || subID != req.SubscriptionID {
		resp.ErrorCode = kerr.UnknownSubscriptionID.Code
		return resp, nil
	}

	if req.Terminating {
		delete(c.telem, req.ClientInstanceID)
	}

	return resp, nil
}
