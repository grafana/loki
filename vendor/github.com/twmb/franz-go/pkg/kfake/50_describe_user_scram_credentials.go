package kfake

import (
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
)

func init() { regKey(50, 0, 0) }

func (c *Cluster) handleDescribeUserSCRAMCredentials(kreq kmsg.Request) (kmsg.Response, error) {
	var (
		req  = kreq.(*kmsg.DescribeUserSCRAMCredentialsRequest)
		resp = req.ResponseKind().(*kmsg.DescribeUserSCRAMCredentialsResponse)
	)

	if err := checkReqVersion(req.Key(), req.Version); err != nil {
		return nil, err
	}

	describe := make(map[string]bool) // if false, user was duplicated
	for _, u := range req.Users {
		if _, ok := describe[u.Name]; ok {
			describe[u.Name] = true
		} else {
			describe[u.Name] = false
		}
	}
	if req.Users == nil { // null returns all
		for u := range c.sasls.scram256 {
			describe[u] = false
		}
		for u := range c.sasls.scram512 {
			describe[u] = false
		}
	}

	addr := func(u string) *kmsg.DescribeUserSCRAMCredentialsResponseResult {
		sr := kmsg.NewDescribeUserSCRAMCredentialsResponseResult()
		sr.User = u
		resp.Results = append(resp.Results, sr)
		return &resp.Results[len(resp.Results)-1]
	}

	for u, duplicated := range describe {
		sr := addr(u)
		if duplicated {
			sr.ErrorCode = kerr.DuplicateResource.Code
			continue
		}
		if a, ok := c.sasls.scram256[u]; ok {
			ci := kmsg.NewDescribeUserSCRAMCredentialsResponseResultCredentialInfo()
			ci.Mechanism = 1
			ci.Iterations = int32(a.iterations)
			sr.CredentialInfos = append(sr.CredentialInfos, ci)
		}
		if a, ok := c.sasls.scram512[u]; ok {
			ci := kmsg.NewDescribeUserSCRAMCredentialsResponseResultCredentialInfo()
			ci.Mechanism = 2
			ci.Iterations = int32(a.iterations)
			sr.CredentialInfos = append(sr.CredentialInfos, ci)
		}
		if len(sr.CredentialInfos) == 0 {
			sr.ErrorCode = kerr.ResourceNotFound.Code
		}
	}

	return resp, nil
}
