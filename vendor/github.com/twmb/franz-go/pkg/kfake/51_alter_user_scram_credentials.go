package kfake

import (
	"bytes"

	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// AlterUserSCRAMCredentials: v0
//
// Behavior:
// * Must be sent to the controller
// * Creates, updates, or deletes SCRAM credentials for users
// * Supports SCRAM-SHA-256 (mechanism 1) and SCRAM-SHA-512 (mechanism 2)
// * Iterations must be between 4096 and 16384

func init() { regKey(51, 0, 0) }

func (c *Cluster) handleAlterUserSCRAMCredentials(creq *clientReq) (kmsg.Response, error) {
	var (
		b    = creq.cc.b
		req  = creq.kreq.(*kmsg.AlterUserSCRAMCredentialsRequest)
		resp = req.ResponseKind().(*kmsg.AlterUserSCRAMCredentialsResponse)
	)

	if err := c.checkReqVersion(req.Key(), req.Version); err != nil {
		return nil, err
	}

	if e := c.denyCluster(creq, kmsg.ACLOperationAlter); e != nil && creq.skipsWork(e) { // a timed-out change still changes the credentials
		for _, d := range req.Deletions {
			sr := kmsg.NewAlterUserSCRAMCredentialsResponseResult()
			sr.User = d.Name
			sr.ErrorCode = e.Code
			resp.Results = append(resp.Results, sr)
		}
		for _, u := range req.Upsertions {
			sr := kmsg.NewAlterUserSCRAMCredentialsResponseResult()
			sr.User = u.Name
			sr.ErrorCode = e.Code
			resp.Results = append(resp.Results, sr)
		}
		return resp, nil
	}

	addr := func(u string) *kmsg.AlterUserSCRAMCredentialsResponseResult {
		sr := kmsg.NewAlterUserSCRAMCredentialsResponseResult()
		sr.User = u
		resp.Results = append(resp.Results, sr)
		return &resp.Results[len(resp.Results)-1]
	}
	// A fault can answer a user before the work runs. The work's own answer
	// for that user must not add an entry or replace the code.
	answered := make(map[string]bool)
	doneu := func(u string, code int16) {
		if answered[u] {
			return
		}
		sr := addr(u)
		sr.ErrorCode = code
	}

	users := make(map[string]int16)
	faulted := func(u string) bool {
		e := creq.faults.check(faultKey{resource: u})
		if e == nil {
			return false
		}
		doneu(u, e.Code)
		answered[u] = true
		if creq.skipsWork(e) { // a timed-out change still changes the credential
			users[u] = e.Code
			return true
		}
		return false
	}

	// Validate everything up front, keeping track of all (and duplicate)
	// users. If we are not controller, we fail with our users map.
	for _, d := range req.Deletions {
		if faulted(d.Name) {
			continue
		}
		if d.Name == "" {
			users[d.Name] = kerr.UnacceptableCredential.Code
			continue
		}
		if d.Mechanism != 1 && d.Mechanism != 2 {
			users[d.Name] = kerr.UnsupportedSaslMechanism.Code
			continue
		}
		users[d.Name] = 0
	}
	for _, u := range req.Upsertions {
		if faulted(u.Name) {
			continue
		}
		if u.Name == "" || u.Iterations < 4096 || u.Iterations > 16384 { // Kafka min/max
			users[u.Name] = kerr.UnacceptableCredential.Code
			continue
		}
		if u.Mechanism != 1 && u.Mechanism != 2 {
			users[u.Name] = kerr.UnsupportedSaslMechanism.Code
			continue
		}
		if code, deleting := users[u.Name]; deleting && code == 0 {
			users[u.Name] = kerr.DuplicateResource.Code
			continue
		}
		users[u.Name] = 0
	}

	if b != c.controller {
		for u := range users {
			doneu(u, kerr.NotController.Code)
		}
		return resp, nil
	}

	// Add anything that failed validation.
	for u, code := range users {
		if code != 0 {
			doneu(u, code)
		}
	}

	// Process all deletions, adding ResourceNotFound as necessary.
	for _, d := range req.Deletions {
		if users[d.Name] != 0 {
			continue
		}
		m := c.sasls.scram256
		if d.Mechanism == 2 {
			m = c.sasls.scram512
		}
		if m == nil {
			doneu(d.Name, kerr.ResourceNotFound.Code)
			continue
		}
		if _, ok := m[d.Name]; !ok {
			doneu(d.Name, kerr.ResourceNotFound.Code)
			continue
		}
		delete(m, d.Name)
		doneu(d.Name, 0)
	}

	// Process all upsertions.
	for _, u := range req.Upsertions {
		if users[u.Name] != 0 {
			continue
		}
		m := &c.sasls.scram256
		mech := saslScram256
		if u.Mechanism == 2 {
			m = &c.sasls.scram512
			mech = saslScram512
		}
		if *m == nil {
			*m = make(map[string]scramAuth)
		}
		(*m)[u.Name] = scramAuth{
			mechanism:  mech,
			iterations: int(u.Iterations),
			saltedPass: bytes.Clone(u.SaltedPassword),
			salt:       bytes.Clone(u.Salt),
		}
		doneu(u.Name, 0)
	}

	c.persistSASLState()
	return resp, nil
}
