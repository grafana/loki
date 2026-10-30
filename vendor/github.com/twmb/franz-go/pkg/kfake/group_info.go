package kfake

import (
	"cmp"
	"context"
	"slices"
	"time"

	"github.com/twmb/franz-go/pkg/kmsg"
)

// GroupInfo contains snapshot-in-time metadata about a group.
type GroupInfo struct {
	Group   string        // Group is the group this info is for.
	State   string        // State is the group's state: Empty, Stable, ...
	Epoch   int32         // Epoch is the group epoch, or the generation for a classic group.
	Members []GroupMember // Members contains the group's current members, sorted by member ID.

	// Commits contains the group's committed offsets, by topic and
	// partition. A share group has no commits.
	Commits map[string]map[int32]GroupCommit
}

// GroupCommit is one committed offset.
type GroupCommit struct {
	Offset      int64
	LeaderEpoch int32
	Metadata    string    // Metadata is empty if the commit had none.
	At          time.Time // At is when the offset was committed.
}

// GroupMember is one member of a group.
type GroupMember struct {
	MemberID         string
	InstanceID       *string
	ClientID         string
	ClientHost       string
	SubscribedTopics []string
	Assignment       map[string][]int32 // Assignment maps topics to partitions.
}

// NumAssigned returns how many partitions are assigned to this member.
func (m *GroupMember) NumAssigned() int {
	var n int
	for _, ps := range m.Assignment {
		n += len(ps)
	}
	return n
}

// NumAssigned returns how many partitions are assigned across all members.
func (g *GroupInfo) NumAssigned() int {
	var n int
	for i := range g.Members {
		n += g.Members[i].NumAssigned()
	}
	return n
}

// GroupInfo returns information about a group if it exists. This covers
// classic, KIP-848, and share groups.
func (c *Cluster) GroupInfo(group string) *GroupInfo {
	var i *GroupInfo
	c.admin(func() {
		g, sg := c.groups.gs[group], c.shareGroups.get(group)
		switch {
		case g != nil:
			i = g.info(c.data.id2t)
		case sg != nil:
			i = sg.info(c.data.id2t)
		}
	})
	return i
}

func (g *group) info(id2t map[uuid]string) *GroupInfo {
	i := &GroupInfo{Group: g.name}
	i.State = g.state.String()
	i.Epoch = g.generation
	if g.typ == "consumer" {
		i.Epoch = g.groupEpoch
	}
	for _, m := range g.members {
		i.Members = append(i.Members, classicMemberInfo(m))
	}
	for _, m := range g.consumerMembers {
		i.Members = append(i.Members, GroupMember{
			MemberID:         m.memberID,
			InstanceID:       m.instanceID,
			ClientID:         m.clientID,
			ClientHost:       m.clientHost,
			SubscribedTopics: slices.Clone(m.subscribedTopics),
			Assignment:       namedAssignment(m.lastReconciledSent, id2t),
		})
	}
	i.Commits = cloneCommits(g.commits)
	sortMembers(i.Members)
	return i
}

func (g *shareGroup) info(id2t map[uuid]string) *GroupInfo {
	i := &GroupInfo{Group: g.name}
	i.State = groupStable.String()
	if len(g.members) == 0 {
		i.State = groupEmpty.String()
	}
	i.Epoch = g.groupEpoch
	for _, m := range g.members {
		i.Members = append(i.Members, GroupMember{
			MemberID:         m.memberID,
			ClientID:         m.clientID,
			ClientHost:       m.clientHost,
			SubscribedTopics: slices.Clone(m.subscribedTopics),
			Assignment:       namedAssignment(m.assignment, id2t),
		})
	}
	sortMembers(i.Members)
	return i
}

// classicMemberInfo snapshots a classic member. A classic member's topics and
// assignment are opaque bytes on the wire, so we decode both; a member that
// speaks a protocol we cannot decode has neither.
func classicMemberInfo(m *groupMember) GroupMember {
	gm := GroupMember{
		MemberID:   m.memberID,
		InstanceID: m.instanceID,
		ClientID:   m.clientID,
		ClientHost: m.clientHost,
		Assignment: make(map[string][]int32),
	}
	if m.join != nil {
		for _, p := range m.join.Protocols {
			var meta kmsg.ConsumerMemberMetadata
			if err := meta.ReadFrom(p.Metadata); err != nil {
				continue
			}
			for _, topic := range meta.Topics {
				if !slices.Contains(gm.SubscribedTopics, topic) {
					gm.SubscribedTopics = append(gm.SubscribedTopics, topic)
				}
			}
		}
	}
	var a kmsg.ConsumerMemberAssignment
	if err := a.ReadFrom(m.assignment); err == nil {
		for _, t := range a.Topics {
			gm.Assignment[t.Topic] = slices.Clone(t.Partitions)
		}
	}
	return gm
}

// namedAssignment converts a topic ID keyed assignment to a topic name keyed
// one, dropping any topic whose ID no longer resolves.
func namedAssignment(in map[uuid][]int32, id2t map[uuid]string) map[string][]int32 {
	out := make(map[string][]int32, len(in))
	for id, ps := range in {
		if t, ok := id2t[id]; ok {
			out[t] = slices.Clone(ps)
		}
	}
	return out
}

func cloneCommits(in tps[offsetCommit]) map[string]map[int32]GroupCommit {
	out := make(map[string]map[int32]GroupCommit, len(in))
	for topic, ps := range in {
		tout := make(map[int32]GroupCommit, len(ps))
		for p, oc := range ps {
			gc := GroupCommit{
				Offset:      oc.offset,
				LeaderEpoch: oc.leaderEpoch,
				At:          oc.lastCommit,
			}
			if oc.metadata != nil {
				gc.Metadata = *oc.metadata
			}
			tout[p] = gc
		}
		out[topic] = tout
	}
	return out
}

// Members come out of a map, so we sort them: a test that indexes Members
// wants the same member every run.
func sortMembers(ms []GroupMember) {
	slices.SortFunc(ms, func(l, r GroupMember) int { return cmp.Compare(l.MemberID, r.MemberID) })
}

// WaitGroupStable waits for the group to be stable with the given number of
// members and returns it. A members of -1 waits for any member count.
func (c *Cluster) WaitGroupStable(ctx context.Context, group string, members int) (*GroupInfo, error) {
	return c.WaitGroupInfo(ctx, group, func(g *GroupInfo) bool {
		return g != nil && g.State == groupStable.String() && (members < 0 || len(g.Members) == members)
	})
}

// WaitGroupInfo waits until fn returns true for the group and returns it. fn
// is called with nil while the group does not exist, so you can wait for a
// group to appear and you can wait for one to go away.
func (c *Cluster) WaitGroupInfo(ctx context.Context, group string, fn func(*GroupInfo) bool) (*GroupInfo, error) {
	for {
		g := c.GroupInfo(group)
		if fn(g) {
			return g, nil
		}
		select {
		case <-ctx.Done():
			return g, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// SetGroupConfigs sets group level configs, the same way IncrementalAlterConfigs does.
//
// Group configs exist independently from a group (same as real Kafka):
// setting configs does not create the group; the group picks them up if
// it is created later. Setting configs on a live group works, though some
// group configs are applied at group creation time and would be missed.
// Notably, share.auto.offset.reset only applies to partitions that have
// not yet been read.
func (c *Cluster) SetGroupConfigs(group string, configs map[string]string) {
	var rcs []kmsg.IncrementalAlterConfigsRequestResourceConfig
	for k, v := range configs {
		rc := kmsg.NewIncrementalAlterConfigsRequestResourceConfig()
		rc.Name = k
		rc.Op = kmsg.IncrementalAlterConfigOpSet
		rc.Value = kmsg.StringPtr(v)
		rcs = append(rcs, rc)
	}
	c.admin(func() { c.setGroupConfigs(group, rcs) })
}
