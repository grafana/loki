package kfake

import (
	"net"
	"strconv"

	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
)

func init() { regKey(10, 0, 4) }

func (c *Cluster) handleFindCoordinator(kreq kmsg.Request) (kmsg.Response, error) {
	req := kreq.(*kmsg.FindCoordinatorRequest)
	resp := req.ResponseKind().(*kmsg.FindCoordinatorResponse)

	if err := checkReqVersion(req.Key(), req.Version); err != nil {
		return nil, err
	}

	var unknown bool
	if req.CoordinatorType != 0 && req.CoordinatorType != 1 {
		unknown = true
	}

	if req.Version <= 3 {
		req.CoordinatorKeys = append(req.CoordinatorKeys, req.CoordinatorKey)
		defer func() {
			resp.ErrorCode = resp.Coordinators[0].ErrorCode
			resp.ErrorMessage = resp.Coordinators[0].ErrorMessage
			resp.NodeID = resp.Coordinators[0].NodeID
			resp.Host = resp.Coordinators[0].Host
			resp.Port = resp.Coordinators[0].Port
		}()
	}

	addc := func(key string) *kmsg.FindCoordinatorResponseCoordinator {
		sc := kmsg.NewFindCoordinatorResponseCoordinator()
		sc.Key = key
		resp.Coordinators = append(resp.Coordinators, sc)
		return &resp.Coordinators[len(resp.Coordinators)-1]
	}

	for _, key := range req.CoordinatorKeys {
		sc := addc(key)
		if unknown {
			sc.ErrorCode = kerr.InvalidRequest.Code
			continue
		}

		b := c.coordinator(key)
		host, port, _ := net.SplitHostPort(b.ln.Addr().String())
		iport, _ := strconv.Atoi(port)

		sc.NodeID = b.node
		sc.Host = host
		sc.Port = int32(iport)
	}

	return resp, nil
}
