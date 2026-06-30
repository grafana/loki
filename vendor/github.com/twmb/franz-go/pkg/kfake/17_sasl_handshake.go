package kfake

import (
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// SASLHandshake: v1
//
// Supported mechanisms:
// * PLAIN
// * SCRAM-SHA-256
// * SCRAM-SHA-512
//
// Note: v0 is not supported (v0 uses implicit auth after handshake)

func init() { regKey(17, 1, 1) }

func (c *Cluster) handleSASLHandshake(creq *clientReq) (kmsg.Response, error) {
	req := creq.kreq.(*kmsg.SASLHandshakeRequest)
	resp := req.ResponseKind().(*kmsg.SASLHandshakeResponse)

	if err := c.checkReqVersion(req.Key(), req.Version); err != nil {
		return nil, err
	}

	if creq.cc.saslStage != saslStageBegin {
		resp.ErrorCode = kerr.IllegalSaslState.Code
		return resp, nil
	}

	switch req.Mechanism {
	case saslPlain:
		creq.cc.saslStage = saslStageAuthPlain
	case saslScram256:
		creq.cc.saslStage = saslStageAuthScram0_256
	case saslScram512:
		creq.cc.saslStage = saslStageAuthScram0_512
	default:
		resp.ErrorCode = kerr.UnsupportedSaslMechanism.Code
		resp.SupportedMechanisms = []string{saslPlain, saslScram256, saslScram512}
	}
	return resp, nil
}
