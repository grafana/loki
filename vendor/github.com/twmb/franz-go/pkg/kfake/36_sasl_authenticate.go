package kfake

import (
	"errors"

	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// SASLAuthenticate: v0-2
//
// Supported mechanisms:
// * PLAIN
// * SCRAM-SHA-256
// * SCRAM-SHA-512
//
// Version notes:
// * v1: SessionLifetimeMs in response
// * v2: Flexible versions

func init() { regKey(36, 0, 2) }

func (c *Cluster) handleSASLAuthenticate(creq *clientReq) (kmsg.Response, error) {
	req := creq.kreq.(*kmsg.SASLAuthenticateRequest)
	resp := req.ResponseKind().(*kmsg.SASLAuthenticateResponse)

	if err := c.checkReqVersion(req.Key(), req.Version); err != nil {
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
		// On re-authentication, the principal must not change
		// (SaslServerAuthenticator ensurePrincipalUnchanged).
		if creq.cc.user != "" && creq.cc.user != u {
			return nil, errors.New("sasl reauthentication changed the authenticated user")
		}
		creq.cc.saslStage = saslStageComplete
		creq.cc.user = u
		c.maybeFillSessionLifetime(creq.cc, resp)

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
		if creq.cc.user != "" && creq.cc.user != c0.user {
			return nil, errors.New("sasl reauthentication changed the authenticated user")
		}
		s0, serverFirst := scramServerFirst(c0, a)
		resp.SASLAuthBytes = serverFirst
		creq.cc.saslStage = saslStageAuthScram1
		creq.cc.s0 = &s0
		creq.cc.user = c0.user // store user for later; cleared if auth fails

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
		if creq.cc.user != "" && creq.cc.user != c0.user {
			return nil, errors.New("sasl reauthentication changed the authenticated user")
		}
		s0, serverFirst := scramServerFirst(c0, a)
		resp.SASLAuthBytes = serverFirst
		creq.cc.saslStage = saslStageAuthScram1
		creq.cc.s0 = &s0
		creq.cc.user = c0.user // store user for later; cleared if auth fails

	case saslStageAuthScram1:
		serverFinal, err := creq.cc.s0.serverFinal(req.SASLAuthBytes)
		if err != nil {
			return nil, err
		}
		resp.SASLAuthBytes = serverFinal
		creq.cc.saslStage = saslStageComplete
		creq.cc.s0 = nil
		c.maybeFillSessionLifetime(creq.cc, resp)
	}

	return resp, nil
}

// maybeFillSessionLifetime sets SessionLifetimeMillis on the final (successful)
// SaslAuthenticate response when re-authentication is enabled, KIP-368, and
// records on the connection whether this session carries an expiration (which
// is what gates a future re-handshake; see 17_sasl_handshake.go). Real
// brokers compute min(connections.max.reauth.ms, credential expiry); kfake
// credentials do not expire, so the config value is the lifetime. The field
// exists from v1 on; kmsg only serializes it for v1+, so setting it
// unconditionally on the response is version safe.
func (c *Cluster) maybeFillSessionLifetime(cc *clientConn, resp *kmsg.SASLAuthenticateResponse) {
	reauthMs := c.connectionsMaxReauthMs()
	if reauthMs > 0 {
		resp.SessionLifetimeMillis = reauthMs
	}
	cc.hasSessionExpiry = reauthMs > 0
}
