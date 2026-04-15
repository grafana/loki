package kfake

import (
	"errors"

	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
)

func init() { regKey(36, 0, 2) }

func (c *Cluster) handleSASLAuthenticate(creq *clientReq) (kmsg.Response, error) {
	req := creq.kreq.(*kmsg.SASLAuthenticateRequest)
	resp := req.ResponseKind().(*kmsg.SASLAuthenticateResponse)

	if err := checkReqVersion(req.Key(), req.Version); err != nil {
		return nil, err
	}

	switch creq.cc.saslStage {
	default:
		resp.ErrorCode = kerr.IllegalSaslState.Code
		return resp, nil

	case saslStageAuthPlain:
		u, p, err := saslSplitPlain(req.SASLAuthBytes)
		if err != nil {
			return nil, err
		}
		if c.sasls.plain == nil {
			return nil, errors.New("invalid sasl")
		}
		if p != c.sasls.plain[u] {
			return nil, errors.New("invalid sasl")
		}
		creq.cc.saslStage = saslStageComplete

	case saslStageAuthScram0_256:
		c0, err := scramParseClient0(req.SASLAuthBytes)
		if err != nil {
			return nil, err
		}
		if c.sasls.scram256 == nil {
			return nil, errors.New("invalid sasl")
		}
		a, ok := c.sasls.scram256[c0.user]
		if !ok {
			return nil, errors.New("invalid sasl")
		}
		s0, serverFirst := scramServerFirst(c0, a)
		resp.SASLAuthBytes = serverFirst
		creq.cc.saslStage = saslStageAuthScram1
		creq.cc.s0 = &s0

	case saslStageAuthScram0_512:
		c0, err := scramParseClient0(req.SASLAuthBytes)
		if err != nil {
			return nil, err
		}
		if c.sasls.scram512 == nil {
			return nil, errors.New("invalid sasl")
		}
		a, ok := c.sasls.scram512[c0.user]
		if !ok {
			return nil, errors.New("invalid sasl")
		}
		s0, serverFirst := scramServerFirst(c0, a)
		resp.SASLAuthBytes = serverFirst
		creq.cc.saslStage = saslStageAuthScram1
		creq.cc.s0 = &s0

	case saslStageAuthScram1:
		serverFinal, err := creq.cc.s0.serverFinal(req.SASLAuthBytes)
		if err != nil {
			return nil, err
		}
		resp.SASLAuthBytes = serverFinal
		creq.cc.saslStage = saslStageComplete
		creq.cc.s0 = nil
	}

	return resp, nil
}
