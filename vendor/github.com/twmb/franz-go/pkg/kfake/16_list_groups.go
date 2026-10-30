package kfake

import (
	"strings"

	"github.com/twmb/franz-go/pkg/kmsg"
)

// ListGroups: v0-5
//
// Version notes:
// * v1: ThrottleMillis
// * v3: Flexible versions
// * v4: StatesFilter (KIP-518)
// * v5: TypesFilter for new consumer protocol (KIP-848)

func init() { regKey(16, 0, 5) }

func (c *Cluster) handleListGroups(creq *clientReq) (kmsg.Response, error) {
	req := creq.kreq.(*kmsg.ListGroupsRequest)

	if err := c.checkReqVersion(req.Key(), req.Version); err != nil {
		return nil, err
	}

	resp := c.groups.handleList(creq)

	// Include share groups, which have their own independent
	// group management loop (not a part of the 'groups' struct).
	for _, sg := range c.shareGroups.gs {
		if c.coordinator(sg.name).node != creq.cc.b.node {
			continue
		}
		if !c.allowedACL(creq, sg.name, kmsg.ACLResourceTypeGroup, kmsg.ACLOperationDescribe) {
			continue
		}
		// Determine state. Use waitControl to safely read member count.
		var state string
		var members int
		if !sg.waitControl(func() {
			members = len(sg.members)
		}) {
			state = "Dead"
		} else if members == 0 {
			state = "Empty"
		} else {
			state = "Stable"
		}
		if len(req.StatesFilter) > 0 && !containsFold(req.StatesFilter, state) {
			continue
		}
		if len(req.TypesFilter) > 0 && !containsFold(req.TypesFilter, "share") {
			continue
		}
		lg := kmsg.NewListGroupsResponseGroup()
		lg.Group = sg.name
		lg.ProtocolType = "share"
		lg.GroupState = state
		lg.GroupType = "share"
		resp.Groups = append(resp.Groups, lg)
	}

	return resp, nil
}

// containsFold returns true if any element in ss case-insensitively equals s.
func containsFold(ss []string, s string) bool {
	for _, v := range ss {
		if strings.EqualFold(v, s) {
			return true
		}
	}
	return false
}
