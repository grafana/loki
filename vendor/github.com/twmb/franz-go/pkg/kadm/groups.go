package kadm

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// GroupMemberMetadata is the metadata that a client sent in a JoinGroup request.
// This can have one of three types:
//
//	*kmsg.ConsumerMemberMetadata, if the group's ProtocolType is "consumer"
//	*kmsg.ConnectMemberMetadata, if the group's ProtocolType is "connect"
//	[]byte, if the group's ProtocolType is unknown
type GroupMemberMetadata struct{ i any }

// AsConsumer returns the metadata as a ConsumerMemberMetadata if possible.
func (m GroupMemberMetadata) AsConsumer() (*kmsg.ConsumerMemberMetadata, bool) {
	c, ok := m.i.(*kmsg.ConsumerMemberMetadata)
	return c, ok
}

// AsConnect returns the metadata as ConnectMemberMetadata if possible.
func (m GroupMemberMetadata) AsConnect() (*kmsg.ConnectMemberMetadata, bool) {
	c, ok := m.i.(*kmsg.ConnectMemberMetadata)
	return c, ok
}

// Raw returns the metadata as a raw byte slice, if it is neither of consumer
// type nor connect type.
func (m GroupMemberMetadata) Raw() ([]byte, bool) {
	c, ok := m.i.([]byte)
	return c, ok
}

// GroupMemberAssignment is the assignment that a leader sent / a member
// received in a SyncGroup request.  This can have one of three types:
//
//	*kmsg.ConsumerMemberAssignment, if the group's ProtocolType is "consumer"
//	*kmsg.ConnectMemberAssignment, if the group's ProtocolType is "connect"
//	[]byte, if the group's ProtocolType is unknown
type GroupMemberAssignment struct{ i any }

// AsConsumer returns the assignment as a ConsumerMemberAssignment if possible.
func (m GroupMemberAssignment) AsConsumer() (*kmsg.ConsumerMemberAssignment, bool) {
	c, ok := m.i.(*kmsg.ConsumerMemberAssignment)
	return c, ok
}

// AsConnect returns the assignment as ConnectMemberAssignment if possible.
func (m GroupMemberAssignment) AsConnect() (*kmsg.ConnectMemberAssignment, bool) {
	c, ok := m.i.(*kmsg.ConnectMemberAssignment)
	return c, ok
}

// Raw returns the assignment as a raw byte slice, if it is neither of consumer
// type nor connect type.
func (m GroupMemberAssignment) Raw() ([]byte, bool) {
	c, ok := m.i.([]byte)
	return c, ok
}

// DescribedGroupMember is the detail of an individual group member as returned
// by a describe groups response.
type DescribedGroupMember struct {
	MemberID   string  // MemberID is the Kafka assigned member ID of this group member.
	InstanceID *string // InstanceID is a potential user assigned instance ID of this group member (KIP-345).
	ClientID   string  // ClientID is the Kafka client given ClientID of this group member.
	ClientHost string  // ClientHost is the host this member is running on.

	Join     GroupMemberMetadata   // Join is what this member sent in its join group request; what it wants to consume.
	Assigned GroupMemberAssignment // Assigned is what this member was assigned to consume by the leader.
}

// AssignedPartitions returns the set of unique topics and partitions that are
// assigned across all members in this group.
//
// This function is only relevant if the group is of type "consumer".
func (d *DescribedGroup) AssignedPartitions() TopicsSet {
	s := make(TopicsSet)
	for _, m := range d.Members {
		if c, ok := m.Assigned.AsConsumer(); ok {
			for _, t := range c.Topics {
				s.Add(t.Topic, t.Partitions...)
			}
		}
	}
	return s
}

// DescribedGroup contains data from a describe groups response for a single
// group.
type DescribedGroup struct {
	Group string // Group is the name of the described group.

	Coordinator  BrokerDetail           // Coordinator is the coordinator broker for this group.
	State        string                 // State is the state this group is in (Empty, Dead, Stable, etc.).
	ProtocolType string                 // ProtocolType is the type of protocol the group is using, "consumer" for normal consumers, "connect" for Kafka connect.
	Protocol     string                 // Protocol is the partition assignor strategy this group is using.
	Members      []DescribedGroupMember // Members contains the members of this group sorted first by InstanceID, or if nil, by MemberID.

	Err error // Err is non-nil if the group could not be described.
}

// DescribedGroups contains data for multiple groups from a describe groups
// response.
type DescribedGroups map[string]DescribedGroup

// AssignedPartitions returns the set of unique topics and partitions that are
// assigned across all members in all groups. This is the all-group analogue to
// DescribedGroup.AssignedPartitions.
//
// This function is only relevant for groups of type "consumer".
func (ds DescribedGroups) AssignedPartitions() TopicsSet {
	s := make(TopicsSet)
	for _, g := range ds {
		for _, m := range g.Members {
			if c, ok := m.Assigned.AsConsumer(); ok {
				for _, t := range c.Topics {
					s.Add(t.Topic, t.Partitions...)
				}
			}
		}
	}
	return s
}

// Sorted returns all groups sorted by group name.
func (ds DescribedGroups) Sorted() []DescribedGroup {
	s := make([]DescribedGroup, 0, len(ds))
	for _, d := range ds {
		s = append(s, d)
	}
	sort.Slice(s, func(i, j int) bool { return s[i].Group < s[j].Group })
	return s
}

// On calls fn for the group if it exists, returning the group and the error
// returned from fn. If fn is nil, this simply returns the group.
//
// The fn is given a shallow copy of the group. This function returns the copy
// as well; any modifications within fn are modifications on the returned copy.
// Modifications on a described group's inner fields are persisted to the
// original map (because slices are pointers).
//
// If the group does not exist, this returns kerr.GroupIDNotFound.
func (rs DescribedGroups) On(group string, fn func(*DescribedGroup) error) (DescribedGroup, error) {
	if len(rs) > 0 {
		r, ok := rs[group]
		if ok {
			if fn == nil {
				return r, nil
			}
			return r, fn(&r)
		}
	}
	return DescribedGroup{}, kerr.GroupIDNotFound
}

// Error iterates over all groups and returns the first error encountered, if
// any.
func (ds DescribedGroups) Error() error {
	for _, d := range ds {
		if d.Err != nil {
			return d.Err
		}
	}
	return nil
}

// Topics returns a sorted list of all group names.
func (ds DescribedGroups) Names() []string {
	all := make([]string, 0, len(ds))
	for g := range ds {
		all = append(all, g)
	}
	sort.Strings(all)
	return all
}

// ListedGroup contains data from a list groups response for a single group.
type ListedGroup struct {
	Coordinator  int32  // Coordinator is the node ID of the coordinator for this group.
	Group        string // Group is the name of this group.
	ProtocolType string // ProtocolType is the type of protocol the group is using, "consumer" for normal consumers, "connect" for Kafka connect.
	State        string // State is the state this group is in (Empty, Dead, Stable, etc.; only if talking to Kafka 2.6+).
}

// ListedGroups contains information from a list groups response.
type ListedGroups map[string]ListedGroup

// Sorted returns all groups sorted by group name.
func (ls ListedGroups) Sorted() []ListedGroup {
	s := make([]ListedGroup, 0, len(ls))
	for _, l := range ls {
		s = append(s, l)
	}
	sort.Slice(s, func(i, j int) bool { return s[i].Group < s[j].Group })
	return s
}

// Groups returns a sorted list of all group names.
func (ls ListedGroups) Groups() []string {
	all := make([]string, 0, len(ls))
	for g := range ls {
		all = append(all, g)
	}
	sort.Strings(all)
	return all
}

// ListGroups returns all groups in the cluster. If you are talking to Kafka
// 2.6+, filter states can be used to return groups only in the requested
// states. By default, this returns all groups. In almost all cases,
// DescribeGroups is more useful.
//
// This may return *ShardErrors or *AuthError.
func (cl *Client) ListGroups(ctx context.Context, filterStates ...string) (ListedGroups, error) {
	req := kmsg.NewPtrListGroupsRequest()
	req.StatesFilter = append(req.StatesFilter, filterStates...)
	shards := cl.cl.RequestSharded(ctx, req)
	list := make(ListedGroups)
	return list, shardErrEachBroker(req, shards, func(b BrokerDetail, kr kmsg.Response) error {
		resp := kr.(*kmsg.ListGroupsResponse)
		if err := maybeAuthErr(resp.ErrorCode); err != nil {
			return err
		}
		if err := kerr.ErrorForCode(resp.ErrorCode); err != nil {
			return err
		}
		for _, g := range resp.Groups {
			list[g.Group] = ListedGroup{ // group only lives on one broker, no need to exist-check
				Coordinator:  b.NodeID,
				Group:        g.Group,
				ProtocolType: g.ProtocolType,
				State:        g.GroupState,
			}
		}
		return nil
	})
}

// DescribeGroups describes either all groups specified, or all groups in the
// cluster if none are specified.
//
// This may return *ShardErrors or *AuthError.
//
// If no groups are specified and this method first lists groups, and list
// groups returns a *ShardErrors, this function describes all successfully
// listed groups and appends the list shard errors to any describe shard
// errors.
//
// If only one group is described, there will be at most one request issued,
// and there is no need to deeply inspect the error.
func (cl *Client) DescribeGroups(ctx context.Context, groups ...string) (DescribedGroups, error) {
	var seList *ShardErrors
	if len(groups) == 0 {
		listed, err := cl.ListGroups(ctx)
		switch {
		case err == nil:
		case errors.As(err, &seList):
		default:
			return nil, err
		}
		groups = listed.Groups()
		if len(groups) == 0 {
			return nil, err
		}
	}

	req := kmsg.NewPtrDescribeGroupsRequest()
	req.Groups = groups

	shards := cl.cl.RequestSharded(ctx, req)
	described := make(DescribedGroups)
	err := shardErrEachBroker(req, shards, func(b BrokerDetail, kr kmsg.Response) error {
		resp := kr.(*kmsg.DescribeGroupsResponse)
		for _, rg := range resp.Groups {
			if err := maybeAuthErr(rg.ErrorCode); err != nil {
				return err
			}
			g := DescribedGroup{
				Group:        rg.Group,
				Coordinator:  b,
				State:        rg.State,
				ProtocolType: rg.ProtocolType,
				Protocol:     rg.Protocol,
				Err:          kerr.ErrorForCode(rg.ErrorCode),
			}
			for _, rm := range rg.Members {
				gm := DescribedGroupMember{
					MemberID:   rm.MemberID,
					InstanceID: rm.InstanceID,
					ClientID:   rm.ClientID,
					ClientHost: rm.ClientHost,
				}

				var mi, ai any
				switch g.ProtocolType {
				case "consumer":
					m := new(kmsg.ConsumerMemberMetadata)
					a := new(kmsg.ConsumerMemberAssignment)

					m.ReadFrom(rm.ProtocolMetadata)
					a.ReadFrom(rm.MemberAssignment)

					mi, ai = m, a
				case "connect":
					m := new(kmsg.ConnectMemberMetadata)
					a := new(kmsg.ConnectMemberAssignment)

					m.ReadFrom(rm.ProtocolMetadata)
					a.ReadFrom(rm.MemberAssignment)

					mi, ai = m, a
				default:
					mi, ai = rm.ProtocolMetadata, rm.MemberAssignment
				}

				gm.Join = GroupMemberMetadata{mi}
				gm.Assigned = GroupMemberAssignment{ai}
				g.Members = append(g.Members, gm)
			}
			sort.Slice(g.Members, func(i, j int) bool {
				if g.Members[i].InstanceID != nil {
					if g.Members[j].InstanceID == nil {
						return true
					}
					return *g.Members[i].InstanceID < *g.Members[j].InstanceID
				}
				if g.Members[j].InstanceID != nil {
					return false
				}
				return g.Members[i].MemberID < g.Members[j].MemberID
			})
			described[g.Group] = g // group only lives on one broker, no need to exist-check
		}
		return nil
	})

	var seDesc *ShardErrors
	switch {
	case err == nil:
		return described, seList.into()
	case errors.As(err, &seDesc):
		if seList != nil {
			seDesc.Errs = append(seList.Errs, seDesc.Errs...)
		}
		return described, seDesc.into()
	default:
		return nil, err
	}
}

// DeleteGroupResponse contains the response for an individual deleted group.
type DeleteGroupResponse struct {
	Group string // Group is the group this response is for.
	Err   error  // Err is non-nil if the group failed to be deleted.
}

// DeleteGroupResponses contains per-group responses to deleted groups.
type DeleteGroupResponses map[string]DeleteGroupResponse

// Sorted returns all deleted group responses sorted by group name.
func (ds DeleteGroupResponses) Sorted() []DeleteGroupResponse {
	s := make([]DeleteGroupResponse, 0, len(ds))
	for _, d := range ds {
		s = append(s, d)
	}
	sort.Slice(s, func(i, j int) bool { return s[i].Group < s[j].Group })
	return s
}

// On calls fn for the response group if it exists, returning the response and
// the error returned from fn. If fn is nil, this simply returns the group.
//
// The fn is given a copy of the response. This function returns the copy as
// well; any modifications within fn are modifications on the returned copy.
//
// If the group does not exist, this returns kerr.GroupIDNotFound.
func (rs DeleteGroupResponses) On(group string, fn func(*DeleteGroupResponse) error) (DeleteGroupResponse, error) {
	if len(rs) > 0 {
		r, ok := rs[group]
		if ok {
			if fn == nil {
				return r, nil
			}
			return r, fn(&r)
		}
	}
	return DeleteGroupResponse{}, kerr.GroupIDNotFound
}

// Error iterates over all groups and returns the first error encountered, if
// any.
func (rs DeleteGroupResponses) Error() error {
	for _, r := range rs {
		if r.Err != nil {
			return r.Err
		}
	}
	return nil
}

// DeleteGroup deletes the specified group. This is similar to DeleteGroups,
// but returns the kerr.ErrorForCode(response.ErrorCode) if the request/response
// is successful.
func (cl *Client) DeleteGroup(ctx context.Context, group string) (DeleteGroupResponse, error) {
	rs, err := cl.DeleteGroups(ctx, group)
	if err != nil {
		return DeleteGroupResponse{}, err
	}
	g, exists := rs[group]
	if !exists {
		return DeleteGroupResponse{}, errors.New("requested group was not part of the delete group response")
	}
	return g, g.Err
}

// DeleteGroups deletes all groups specified.
//
// The purpose of this request is to allow operators a way to delete groups
// after Kafka 1.1, which removed RetentionTimeMillis from offset commits. See
// KIP-229 for more details.
//
// This may return *ShardErrors. This does not return on authorization
// failures, instead, authorization failures are included in the responses.
func (cl *Client) DeleteGroups(ctx context.Context, groups ...string) (DeleteGroupResponses, error) {
	if len(groups) == 0 {
		return nil, nil
	}
	req := kmsg.NewPtrDeleteGroupsRequest()
	req.Groups = append(req.Groups, groups...)
	shards := cl.cl.RequestSharded(ctx, req)

	rs := make(map[string]DeleteGroupResponse)
	return rs, shardErrEach(req, shards, func(kr kmsg.Response) error {
		resp := kr.(*kmsg.DeleteGroupsResponse)
		for _, g := range resp.Groups {
			rs[g.Group] = DeleteGroupResponse{ // group is always on one broker, no need to exist-check
				Group: g.Group,
				Err:   kerr.ErrorForCode(g.ErrorCode),
			}
		}
		return nil
	})
}

// LeaveGroupBuilder helps build a leave group request, rather than having
// a function signature (string, string, ...string).
//
// All functions on this type accept and return the same pointer, allowing
// for easy build-and-use usage.
type LeaveGroupBuilder struct {
	group       string
	reason      *string
	instanceIDs []*string
}

// LeaveGroup returns a LeaveGroupBuilder for the input group.
func LeaveGroup(group string) *LeaveGroupBuilder {
	return &LeaveGroupBuilder{
		group: group,
	}
}

// Reason attaches a reason to all members in the leave group request.
// This requires Kafka 3.2+.
func (b *LeaveGroupBuilder) Reason(reason string) *LeaveGroupBuilder {
	b.reason = StringPtr(reason)
	return b
}

// InstanceIDs are members to remove from a group.
func (b *LeaveGroupBuilder) InstanceIDs(ids ...string) *LeaveGroupBuilder {
	for _, id := range ids {
		if id != "" {
			b.instanceIDs = append(b.instanceIDs, StringPtr(id))
		}
	}
	return b
}

// LeaveGroupResponse contains the response for an individual instance ID that
// left a group.
type LeaveGroupResponse struct {
	Group      string // Group is the group that was left.
	InstanceID string // InstanceID is the instance ID that left the group.
	MemberID   string // MemberID is the member ID that left the group.
	Err        error  // Err is non-nil if this member did not exist.
}

// LeaveGroupResponses contains responses for each member of a leave group
// request. The map key is the instance ID that was removed from the group.
type LeaveGroupResponses map[string]LeaveGroupResponse

// Sorted returns all removed group members by instance ID.
func (ls LeaveGroupResponses) Sorted() []LeaveGroupResponse {
	s := make([]LeaveGroupResponse, 0, len(ls))
	for _, l := range ls {
		s = append(s, l)
	}
	sort.Slice(s, func(i, j int) bool { return s[i].InstanceID < s[j].InstanceID })
	return s
}

// EachError calls fn for every removed member that has a non-nil error.
func (ls LeaveGroupResponses) EachError(fn func(l LeaveGroupResponse)) {
	for _, l := range ls {
		if l.Err != nil {
			fn(l)
		}
	}
}

// Each calls fn for every removed member.
func (ls LeaveGroupResponses) Each(fn func(l LeaveGroupResponse)) {
	for _, l := range ls {
		fn(l)
	}
}

// Error iterates over all removed members and returns the first error
// encountered, if any.
func (ls LeaveGroupResponses) Error() error {
	for _, l := range ls {
		if l.Err != nil {
			return l.Err
		}
	}
	return nil
}

// Ok returns true if there are no errors. This is a shortcut for ls.Error() ==
// nil.
func (ls LeaveGroupResponses) Ok() bool {
	return ls.Error() == nil
}

// LeaveGroup causes instance IDs to leave a group.
//
// This function allows manually removing members using instance IDs from a
// group, which allows for fast scale down / host replacement (see KIP-345 for
// more detail). This returns an *AuthErr if the use is not authorized to
// remove members from groups.
func (cl *Client) LeaveGroup(ctx context.Context, b *LeaveGroupBuilder) (LeaveGroupResponses, error) {
	if b == nil || len(b.instanceIDs) == 0 {
		return nil, nil
	}
	req := kmsg.NewPtrLeaveGroupRequest()
	req.Group = b.group
	for _, id := range b.instanceIDs {
		m := kmsg.NewLeaveGroupRequestMember()
		id := id
		m.InstanceID = id
		m.Reason = b.reason
		req.Members = append(req.Members, m)
	}

	resp, err := req.RequestWith(ctx, cl.cl)
	if err != nil {
		return nil, err
	}
	if err := maybeAuthErr(resp.ErrorCode); err != nil {
		return nil, err
	}
	if err := kerr.ErrorForCode(resp.ErrorCode); err != nil {
		return nil, err
	}

	resps := make(LeaveGroupResponses)
	for _, m := range resp.Members {
		if m.InstanceID == nil {
			continue // highly unexpected, buggy kafka
		}
		resps[*m.InstanceID] = LeaveGroupResponse{
			Group:      b.group,
			MemberID:   m.MemberID,
			InstanceID: *m.InstanceID,
			Err:        kerr.ErrorForCode(resp.ErrorCode),
		}
	}
	return resps, err
}

// OffsetResponse contains the response for an individual offset for offset
// methods.
type OffsetResponse struct {
	Offset
	Err error // Err is non-nil if the offset operation failed.
}

// OffsetResponses contains per-partition responses to offset methods.
type OffsetResponses map[string]map[int32]OffsetResponse

// Lookup returns the offset at t and p and whether it exists.
func (os OffsetResponses) Lookup(t string, p int32) (OffsetResponse, bool) {
	if len(os) == 0 {
		return OffsetResponse{}, false
	}
	ps := os[t]
	if len(ps) == 0 {
		return OffsetResponse{}, false
	}
	o, exists := ps[p]
	return o, exists
}

// Keep filters the responses to only keep the input offsets.
func (os OffsetResponses) Keep(o Offsets) {
	os.DeleteFunc(func(r OffsetResponse) bool {
		if len(o) == 0 {
			return true // keep nothing, delete
		}
		ot := o[r.Topic]
		if ot == nil {
			return true // topic missing, delete
		}
		_, ok := ot[r.Partition]
		return !ok // does not exist, delete
	})
}

// Offsets returns these offset responses as offsets.
func (os OffsetResponses) Offsets() Offsets {
	i := make(Offsets)
	os.Each(func(o OffsetResponse) {
		i.Add(o.Offset)
	})
	return i
}

// KOffsets returns these offset responses as a kgo offset map.
func (os OffsetResponses) KOffsets() map[string]map[int32]kgo.Offset {
	return os.Offsets().KOffsets()
}

// DeleteFunc keeps only the offsets for which fn returns true.
func (os OffsetResponses) KeepFunc(fn func(OffsetResponse) bool) {
	for t, ps := range os {
		for p, o := range ps {
			if !fn(o) {
				delete(ps, p)
			}
		}
		if len(ps) == 0 {
			delete(os, t)
		}
	}
}

// DeleteFunc deletes any offset for which fn returns true.
func (os OffsetResponses) DeleteFunc(fn func(OffsetResponse) bool) {
	os.KeepFunc(func(o OffsetResponse) bool { return !fn(o) })
}

// Add adds an offset for a given topic/partition to this OffsetResponses map
// (even if it exists).
func (os *OffsetResponses) Add(o OffsetResponse) {
	if *os == nil {
		*os = make(map[string]map[int32]OffsetResponse)
	}
	ot := (*os)[o.Topic]
	if ot == nil {
		ot = make(map[int32]OffsetResponse)
		(*os)[o.Topic] = ot
	}
	ot[o.Partition] = o
}

// EachError calls fn for every offset that as a non-nil error.
func (os OffsetResponses) EachError(fn func(o OffsetResponse)) {
	for _, ps := range os {
		for _, o := range ps {
			if o.Err != nil {
				fn(o)
			}
		}
	}
}

// Sorted returns the responses sorted by topic and partition.
func (os OffsetResponses) Sorted() []OffsetResponse {
	var s []OffsetResponse
	os.Each(func(o OffsetResponse) { s = append(s, o) })
	sort.Slice(s, func(i, j int) bool {
		return s[i].Topic < s[j].Topic ||
			s[i].Topic == s[j].Topic && s[i].Partition < s[j].Partition
	})
	return s
}

// Each calls fn for every offset.
func (os OffsetResponses) Each(fn func(OffsetResponse)) {
	for _, ps := range os {
		for _, o := range ps {
			fn(o)
		}
	}
}

// Partitions returns the set of unique topics and partitions in these offsets.
func (os OffsetResponses) Partitions() TopicsSet {
	s := make(TopicsSet)
	os.Each(func(o OffsetResponse) {
		s.Add(o.Topic, o.Partition)
	})
	return s
}

// Error iterates over all offsets and returns the first error encountered, if
// any. This can be used to check if an operation was entirely successful or
// not.
//
// Note that offset operations can be partially successful. For example, some
// offsets could succeed in an offset commit while others fail (maybe one topic
// does not exist for some reason, or you are not authorized for one topic). If
// this is something you need to worry about, you may need to check all offsets
// manually.
func (os OffsetResponses) Error() error {
	for _, ps := range os {
		for _, o := range ps {
			if o.Err != nil {
				return o.Err
			}
		}
	}
	return nil
}

// Ok returns true if there are no errors. This is a shortcut for os.Error() ==
// nil.
func (os OffsetResponses) Ok() bool {
	return os.Error() == nil
}

// CommitOffsets issues an offset commit request for the input offsets.
//
// This function can be used to manually commit offsets when directly consuming
// partitions outside of an actual consumer group. For example, if you assign
// partitions manually, but want still use Kafka to checkpoint what you have
// consumed, you can manually issue an offset commit request with this method.
//
// This does not return on authorization failures, instead, authorization
// failures are included in the responses.
func (cl *Client) CommitOffsets(ctx context.Context, group string, os Offsets) (OffsetResponses, error) {
	req := kmsg.NewPtrOffsetCommitRequest()
	req.Group = group
	for t, ps := range os {
		rt := kmsg.NewOffsetCommitRequestTopic()
		rt.Topic = t
		for p, o := range ps {
			rp := kmsg.NewOffsetCommitRequestTopicPartition()
			rp.Partition = p
			rp.Offset = o.At
			rp.LeaderEpoch = o.LeaderEpoch
			if len(o.Metadata) > 0 {
				rp.Metadata = kmsg.StringPtr(o.Metadata)
			}
			rt.Partitions = append(rt.Partitions, rp)
		}
		req.Topics = append(req.Topics, rt)
	}

	resp, err := req.RequestWith(ctx, cl.cl)
	if err != nil {
		return nil, err
	}

	rs := make(OffsetResponses)
	for _, t := range resp.Topics {
		rt := make(map[int32]OffsetResponse)
		rs[t.Topic] = rt
		for _, p := range t.Partitions {
			rt[p.Partition] = OffsetResponse{
				Offset: os[t.Topic][p.Partition],
				Err:    kerr.ErrorForCode(p.ErrorCode),
			}
		}
	}

	for t, ps := range os {
		respt := rs[t]
		if respt == nil {
			respt = make(map[int32]OffsetResponse)
			rs[t] = respt
		}
		for p, o := range ps {
			if _, exists := respt[p]; exists {
				continue
			}
			respt[p] = OffsetResponse{
				Offset: o,
				Err:    errOffsetCommitMissing,
			}
		}
	}

	return rs, nil
}

var errOffsetCommitMissing = errors.New("partition missing in commit response")

// CommitAllOffsets is identical to CommitOffsets, but returns an error if the
// offset commit was successful, but some offset within the commit failed to be
// committed.
//
// This is a shortcut function provided to avoid checking two errors, but you
// must be careful with this if partially successful commits can be a problem
// for you.
func (cl *Client) CommitAllOffsets(ctx context.Context, group string, os Offsets) error {
	commits, err := cl.CommitOffsets(ctx, group, os)
	if err != nil {
		return err
	}
	return commits.Error()
}

// FetchOffsets issues an offset fetch requests for all topics and partitions
// in the group. Because Kafka returns only partitions you are authorized to
// fetch, this only returns an auth error if you are not authorized to describe
// the group at all.
//
// This method requires talking to Kafka v0.11+.
func (cl *Client) FetchOffsets(ctx context.Context, group string) (OffsetResponses, error) {
	req := kmsg.NewPtrOffsetFetchRequest()
	req.Group = group
	resp, err := req.RequestWith(ctx, cl.cl)
	if err != nil {
		return nil, err
	}
	if err := maybeAuthErr(resp.ErrorCode); err != nil {
		return nil, err
	}
	if err := kerr.ErrorForCode(resp.ErrorCode); err != nil {
		return nil, err
	}
	rs := make(OffsetResponses)
	for _, t := range resp.Topics {
		rt := make(map[int32]OffsetResponse)
		rs[t.Topic] = rt
		for _, p := range t.Partitions {
			if err := maybeAuthErr(p.ErrorCode); err != nil {
				return nil, err
			}
			var meta string
			if p.Metadata != nil {
				meta = *p.Metadata
			}
			rt[p.Partition] = OffsetResponse{
				Offset: Offset{
					Topic:       t.Topic,
					Partition:   p.Partition,
					At:          p.Offset,
					LeaderEpoch: p.LeaderEpoch,
					Metadata:    meta,
				},
				Err: kerr.ErrorForCode(p.ErrorCode),
			}
		}
	}
	return rs, nil
}

// FetchAllGroupTopics is a kadm "internal" topic name that can be used in
// [FetchOffsetsForTopics]. By default, [FetchOffsetsForTopics] only returns
// topics that are explicitly requested. Other topics that may be committed to
// in the group are not returned. Using FetchAllRequestedTopics switches the
// behavior to return the union of all committed topics and all requested
// topics.
const FetchAllGroupTopics = "|fetch-all-group-topics|"

// FetchOffsetsForTopics is a helper function that returns the currently
// committed offsets for the given group, as well as default -1 offsets for any
// topic/partition that does not yet have a commit.
//
// If any partition fetched or listed has an error, this function returns an
// error. The returned offset responses are ready to be used or converted
// directly to pure offsets with `Into`, and again into kgo offsets with
// another `Into`.
//
// By default, this function returns offsets for only the requested topics. You
// can use the special "topic" [FetchAllGroupTopics] to return all committed-to
// topics in addition to all requested topics.
func (cl *Client) FetchOffsetsForTopics(ctx context.Context, group string, topics ...string) (OffsetResponses, error) {
	os := make(Offsets)

	var all bool
	keept := topics[:0]
	for _, topic := range topics {
		if topic == FetchAllGroupTopics {
			all = true
			continue
		}
		keept = append(keept, topic)
	}
	topics = keept

	if !all && len(topics) == 0 {
		return make(OffsetResponses), nil
	}

	// We have to request metadata to learn all partitions in all the
	// topics. The default returned offset for all partitions is filled in
	// to be -1.
	if len(topics) > 0 {
		listed, err := cl.ListTopics(ctx, topics...)
		if err != nil {
			return nil, fmt.Errorf("unable to list topics: %w", err)
		}

		for _, topic := range topics {
			t := listed[topic]
			if t.Err != nil {
				return nil, fmt.Errorf("unable to describe topics, topic err: %w", t.Err)
			}
			for _, p := range t.Partitions {
				os.AddOffset(topic, p.Partition, -1, -1)
			}
		}
	}

	resps, err := cl.FetchOffsets(ctx, group)
	if err != nil {
		return nil, fmt.Errorf("unable to fetch offsets: %w", err)
	}
	if err := resps.Error(); err != nil {
		return nil, fmt.Errorf("offset fetches had a load error, first error: %w", err)
	}

	// For any topic (and any partition) we explicitly asked for, if the
	// partition does not exist in the response, we fill the default -1
	// from above.
	os.Each(func(o Offset) {
		if _, ok := resps.Lookup(o.Topic, o.Partition); !ok {
			resps.Add(OffsetResponse{Offset: o})
		}
	})

	// If we are not requesting all group offsets, then we strip any topic
	// that was not explicitly requested.
	if !all {
		tset := make(map[string]struct{})
		for _, t := range topics {
			tset[t] = struct{}{}
		}
		for t := range resps {
			if _, ok := tset[t]; !ok {
				delete(resps, t)
			}
		}
	}
	return resps, nil
}

// FetchOffsetsResponse contains a fetch offsets response for a single group.
type FetchOffsetsResponse struct {
	Group   string          // Group is the offsets these fetches correspond to.
	Fetched OffsetResponses // Fetched contains offsets fetched for this group, if any.
	Err     error           // Err contains any error preventing offsets from being fetched.
}

// CommittedPartitions returns the set of unique topics and partitions that
// have been committed to in this group.
func (r FetchOffsetsResponse) CommittedPartitions() TopicsSet {
	return r.Fetched.Partitions()
}

// FetchOFfsetsResponses contains responses for many fetch offsets requests.
type FetchOffsetsResponses map[string]FetchOffsetsResponse

// EachError calls fn for every response that as a non-nil error.
func (rs FetchOffsetsResponses) EachError(fn func(FetchOffsetsResponse)) {
	for _, r := range rs {
		if r.Err != nil {
			fn(r)
		}
	}
}

// AllFailed returns whether all fetch offsets requests failed.
func (rs FetchOffsetsResponses) AllFailed() bool {
	var n int
	rs.EachError(func(FetchOffsetsResponse) { n++ })
	return len(rs) > 0 && n == len(rs)
}

// CommittedPartitions returns the set of unique topics and partitions that
// have been committed to across all members in all responses. This is the
// all-group analogue to FetchOffsetsResponse.CommittedPartitions.
func (rs FetchOffsetsResponses) CommittedPartitions() TopicsSet {
	s := make(TopicsSet)
	for _, r := range rs {
		s.Merge(r.CommittedPartitions())
	}
	return s
}

// On calls fn for the response group if it exists, returning the response and
// the error returned from fn. If fn is nil, this simply returns the group.
//
// The fn is given a copy of the response. This function returns the copy as
// well; any modifications within fn are modifications on the returned copy.
//
// If the group does not exist, this returns kerr.GroupIDNotFound.
func (rs FetchOffsetsResponses) On(group string, fn func(*FetchOffsetsResponse) error) (FetchOffsetsResponse, error) {
	if len(rs) > 0 {
		r, ok := rs[group]
		if ok {
			if fn == nil {
				return r, nil
			}
			return r, fn(&r)
		}
	}
	return FetchOffsetsResponse{}, kerr.GroupIDNotFound
}

// Error iterates over all responses and returns the first error encountered,
// if any.
func (rs FetchOffsetsResponses) Error() error {
	for _, r := range rs {
		if r.Err != nil {
			return r.Err
		}
	}
	return nil
}

// FetchManyOffsets issues a fetch offsets requests for each group specified.
//
// This function is a batch version of FetchOffsets. FetchOffsets and
// CommitOffsets are important to provide as simple APIs for users that manage
// group offsets outside of a consumer group. Each individual group may have an
// auth error.
func (cl *Client) FetchManyOffsets(ctx context.Context, groups ...string) FetchOffsetsResponses {
	fetched := make(FetchOffsetsResponses)
	if len(groups) == 0 {
		return fetched
	}

	req := kmsg.NewPtrOffsetFetchRequest()
	for _, group := range groups {
		rg := kmsg.NewOffsetFetchRequestGroup()
		rg.Group = group
		req.Groups = append(req.Groups, rg)
	}

	groupErr := func(g string, err error) {
		fetched[g] = FetchOffsetsResponse{
			Group: g,
			Err:   err,
		}
	}
	allGroupsErr := func(req *kmsg.OffsetFetchRequest, err error) {
		for _, g := range req.Groups {
			groupErr(g.Group, err)
		}
	}

	shards := cl.cl.RequestSharded(ctx, req)
	for _, shard := range shards {
		req := shard.Req.(*kmsg.OffsetFetchRequest)
		if shard.Err != nil {
			allGroupsErr(req, shard.Err)
			continue
		}
		resp := shard.Resp.(*kmsg.OffsetFetchResponse)
		if err := maybeAuthErr(resp.ErrorCode); err != nil {
			allGroupsErr(req, err)
			continue
		}
		for _, g := range resp.Groups {
			if err := maybeAuthErr(g.ErrorCode); err != nil {
				groupErr(g.Group, err)
				continue
			}
			rs := make(OffsetResponses)
			fg := FetchOffsetsResponse{
				Group:   g.Group,
				Fetched: rs,
				Err:     kerr.ErrorForCode(g.ErrorCode),
			}
			fetched[g.Group] = fg // group coordinator owns all of a group, no need to check existence
			for _, t := range g.Topics {
				rt := make(map[int32]OffsetResponse)
				rs[t.Topic] = rt
				for _, p := range t.Partitions {
					var meta string
					if p.Metadata != nil {
						meta = *p.Metadata
					}
					rt[p.Partition] = OffsetResponse{
						Offset: Offset{
							Topic:       t.Topic,
							Partition:   p.Partition,
							At:          p.Offset,
							LeaderEpoch: p.LeaderEpoch,
							Metadata:    meta,
						},
						Err: kerr.ErrorForCode(p.ErrorCode),
					}
				}
			}
		}
	}
	return fetched
}

// DeleteOffsetsResponses contains the per topic, per partition errors. If an
// offset deletion for a partition was successful, the error will be nil.
type DeleteOffsetsResponses map[string]map[int32]error

// Lookup returns the response at t and p and whether it exists.
func (ds DeleteOffsetsResponses) Lookup(t string, p int32) (error, bool) {
	if len(ds) == 0 {
		return nil, false
	}
	ps := ds[t]
	if len(ps) == 0 {
		return nil, false
	}
	r, exists := ps[p]
	return r, exists
}

// EachError calls fn for every partition that as a non-nil deletion error.
func (ds DeleteOffsetsResponses) EachError(fn func(string, int32, error)) {
	for t, ps := range ds {
		for p, err := range ps {
			if err != nil {
				fn(t, p, err)
			}
		}
	}
}

// Error iterates over all responses and returns the first error encountered,
// if any.
func (ds DeleteOffsetsResponses) Error() error {
	for _, ps := range ds {
		for _, err := range ps {
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// DeleteOffsets deletes offsets for the given group.
//
// Originally, offset commits were persisted in Kafka for some retention time.
// This posed problematic for infrequently committing consumers, so the
// retention time concept was removed in Kafka v2.1 in favor of deleting
// offsets for a group only when the group became empty. However, if a group
// stops consuming from a topic, then the offsets will persist and lag
// monitoring for the group will notice an ever increasing amount of lag for
// these no-longer-consumed topics. Thus, Kafka v2.4 introduced an OffsetDelete
// request to allow admins to manually delete offsets for no longer consumed
// topics.
//
// This method requires talking to Kafka v2.4+. This returns an *AuthErr if the
// user is not authorized to delete offsets in the group at all. This does not
// return on per-topic authorization failures, instead, per-topic authorization
// failures are included in the responses.
func (cl *Client) DeleteOffsets(ctx context.Context, group string, s TopicsSet) (DeleteOffsetsResponses, error) {
	if len(s) == 0 {
		return nil, nil
	}

	req := kmsg.NewPtrOffsetDeleteRequest()
	req.Group = group
	for t, ps := range s {
		rt := kmsg.NewOffsetDeleteRequestTopic()
		rt.Topic = t
		for p := range ps {
			rp := kmsg.NewOffsetDeleteRequestTopicPartition()
			rp.Partition = p
			rt.Partitions = append(rt.Partitions, rp)
		}
		req.Topics = append(req.Topics, rt)
	}

	resp, err := req.RequestWith(ctx, cl.cl)
	if err != nil {
		return nil, err
	}
	if err := maybeAuthErr(resp.ErrorCode); err != nil {
		return nil, err
	}
	if err := kerr.ErrorForCode(resp.ErrorCode); err != nil {
		return nil, err
	}

	r := make(DeleteOffsetsResponses)
	for _, t := range resp.Topics {
		rt := make(map[int32]error)
		r[t.Topic] = rt
		for _, p := range t.Partitions {
			rt[p.Partition] = kerr.ErrorForCode(p.ErrorCode)
		}
	}
	return r, nil
}

// GroupMemberLag is the lag between a group member's current offset commit and
// the current end offset.
//
// If either the offset commits have load errors, or the listed end offsets
// have load errors, the Lag field will be -1 and the Err field will be set (to
// the first of either the commit error, or else the list error).
//
// If the group is in the Empty state, lag is calculated for all partitions in
// a topic, but the member is nil. The calculate function assumes that any
// assigned topic is meant to be entirely consumed. If the group is Empty and
// topics could not be listed, some partitions may be missing.
type GroupMemberLag struct {
	// Member is a reference to the group member consuming this partition.
	// If the group is in state Empty, the member will be nil.
	Member    *DescribedGroupMember
	Topic     string // Topic is the topic this lag is for.
	Partition int32  // Partition is the partition this lag is for.

	Commit Offset       // Commit is this member's current offset commit.
	Start  ListedOffset // Start is a reference to the start of this partition, if provided. Start offsets are optional; if not provided, Start.Err is a non-nil error saying this partition is missing from list offsets. This is always present if lag is calculated via Client.Lag.
	End    ListedOffset // End is a reference to the end offset of this partition.
	Lag    int64        // Lag is how far behind this member is, or -1 if there is a commit error or list offset error.

	Err error // Err is either the commit error, or the list end offsets error, or nil.
}

// IsEmpty returns if the this lag is for a group in the Empty state.
func (g *GroupMemberLag) IsEmpty() bool { return g.Member == nil }

// GroupLag is the per-topic, per-partition lag of members in a group.
type GroupLag map[string]map[int32]GroupMemberLag

// Lookup returns the lag at t and p and whether it exists.
func (l GroupLag) Lookup(t string, p int32) (GroupMemberLag, bool) {
	if len(l) == 0 {
		return GroupMemberLag{}, false
	}
	ps := l[t]
	if len(ps) == 0 {
		return GroupMemberLag{}, false
	}
	m, exists := ps[p]
	return m, exists
}

// Sorted returns the per-topic, per-partition lag by member sorted in order by
// topic then partition.
func (l GroupLag) Sorted() []GroupMemberLag {
	var all []GroupMemberLag
	for _, ps := range l {
		for _, l := range ps {
			all = append(all, l)
		}
	}
	sort.Slice(all, func(i, j int) bool {
		l, r := all[i], all[j]
		if l.Topic < r.Topic {
			return true
		}
		if l.Topic > r.Topic {
			return false
		}
		return l.Partition < r.Partition
	})
	return all
}

// IsEmpty returns if the group is empty.
func (l GroupLag) IsEmpty() bool {
	for _, ps := range l {
		for _, m := range ps {
			return m.IsEmpty()
		}
	}
	return false
}

// Total returns the total lag across all topics.
func (l GroupLag) Total() int64 {
	var tot int64
	for _, tl := range l.TotalByTopic() {
		tot += tl.Lag
	}
	return tot
}

// TotalByTopic returns the total lag for each topic.
func (l GroupLag) TotalByTopic() GroupTopicsLag {
	m := make(map[string]TopicLag)
	for t, ps := range l {
		mt := TopicLag{
			Topic: t,
		}
		for _, l := range ps {
			if l.Lag > 0 {
				mt.Lag += l.Lag
			}
		}
		m[t] = mt
	}
	return m
}

// GroupTopicsLag is the total lag per topic within a group.
type GroupTopicsLag map[string]TopicLag

// TopicLag is the lag for an individual topic within a group.
type TopicLag struct {
	Topic string
	Lag   int64
}

// Sorted returns the per-topic lag, sorted by topic.
func (l GroupTopicsLag) Sorted() []TopicLag {
	var all []TopicLag
	for _, tl := range l {
		all = append(all, tl)
	}
	sort.Slice(all, func(i, j int) bool {
		return all[i].Topic < all[j].Topic
	})
	return all
}

// DescribedGroupLag contains a described group and its lag, or the errors that
// prevent the lag from being calculated.
type DescribedGroupLag struct {
	Group string // Group is the group name.

	Coordinator  BrokerDetail           // Coordinator is the coordinator broker for this group.
	State        string                 // State is the state this group is in (Empty, Dead, Stable, etc.).
	ProtocolType string                 // ProtocolType is the type of protocol the group is using, "consumer" for normal consumers, "connect" for Kafka connect.
	Protocol     string                 // Protocol is the partition assignor strategy this group is using.
	Members      []DescribedGroupMember // Members contains the members of this group sorted first by InstanceID, or if nil, by MemberID.
	Lag          GroupLag               // Lag is the lag for the group.

	DescribeErr error // DescribeErr is the error returned from describing the group, if any.
	FetchErr    error // FetchErr is the error returned from fetching offsets, if any.
}

// Err returns the first of DescribeErr or FetchErr that is non-nil.
func (l *DescribedGroupLag) Error() error {
	if l.DescribeErr != nil {
		return l.DescribeErr
	}
	return l.FetchErr
}

// DescribedGroupLags is a map of group names to the described group with its
// lag, or error for those groups.
type DescribedGroupLags map[string]DescribedGroupLag

// Sorted returns all lags sorted by group name.
func (ls DescribedGroupLags) Sorted() []DescribedGroupLag {
	s := make([]DescribedGroupLag, 0, len(ls))
	for _, l := range ls {
		s = append(s, l)
	}
	sort.Slice(s, func(i, j int) bool { return s[i].Group < s[j].Group })
	return s
}

// EachError calls fn for every group that has a non-nil error.
func (ls DescribedGroupLags) EachError(fn func(l DescribedGroupLag)) {
	for _, l := range ls {
		if l.Error() != nil {
			fn(l)
		}
	}
}

// Each calls fn for every group.
func (ls DescribedGroupLags) Each(fn func(l DescribedGroupLag)) {
	for _, l := range ls {
		fn(l)
	}
}

// Error iterates over all groups and returns the first error encountered, if
// any.
func (ls DescribedGroupLags) Error() error {
	for _, l := range ls {
		if l.Error() != nil {
			return l.Error()
		}
	}
	return nil
}

// Ok returns true if there are no errors. This is a shortcut for ls.Error() ==
// nil.
func (ls DescribedGroupLags) Ok() bool {
	return ls.Error() == nil
}

// Lag returns the lag for all input groups. This function is a shortcut for
// the steps required to use CalculateGroupLagWithStartOffsets properly, with
// some opinionated choices for error handling since calculating lag is
// multi-request process. If a group cannot be described or the offsets cannot
// be fetched, an error is returned for the group. If any topic cannot have its
// end offsets listed, the lag for the partition has a corresponding error. If
// any request fails with an auth error, this returns *AuthError.
func (cl *Client) Lag(ctx context.Context, groups ...string) (DescribedGroupLags, error) {
	set := make(map[string]struct{}, len(groups))
	for _, g := range groups {
		set[g] = struct{}{}
	}
	rem := func() []string {
		groups = groups[:0]
		for g := range set {
			groups = append(groups, g)
		}
		return groups
	}
	lags := make(DescribedGroupLags)

	described, err := cl.DescribeGroups(ctx, rem()...)
	// For auth err: always return.
	// For shard errors, if we had some partial success, then we continue
	// to the rest of the logic in this function.
	// If every shard failed, or on all other errors, we return.
	var ae *AuthError
	var se *ShardErrors
	switch {
	case errors.As(err, &ae):
		return nil, err
	case errors.As(err, &se) && !se.AllFailed:
		for _, se := range se.Errs {
			for _, g := range se.Req.(*kmsg.DescribeGroupsRequest).Groups {
				lags[g] = DescribedGroupLag{
					Group:       g,
					Coordinator: se.Broker,
					DescribeErr: se.Err,
				}
				delete(set, g)
			}
		}
	case err != nil:
		return nil, err
	}
	for _, g := range described {
		lags[g.Group] = DescribedGroupLag{
			Group:        g.Group,
			Coordinator:  g.Coordinator,
			State:        g.State,
			ProtocolType: g.ProtocolType,
			Protocol:     g.Protocol,
			Members:      g.Members,
			DescribeErr:  g.Err,
		}
		if g.Err != nil {
			delete(set, g.Group)
			continue
		}

		// If the input set of groups is empty, DescribeGroups returns all groups.
		// We add to `set` here so that the Lag function itself can calculate
		// lag for all groups.
		set[g.Group] = struct{}{}
	}
	if len(set) == 0 {
		return lags, nil
	}

	// Same thought here. For auth errors, we always return.
	// If a group offset fetch failed, we delete it from described
	// because we cannot calculate lag for it.
	fetched := cl.FetchManyOffsets(ctx, rem()...)
	for _, r := range fetched {
		switch {
		case errors.As(r.Err, &ae):
			return nil, err
		case r.Err != nil:
			l := lags[r.Group]
			l.FetchErr = r.Err
			lags[r.Group] = l
			delete(set, r.Group)
			delete(described, r.Group)
		}
	}
	if len(set) == 0 {
		return lags, nil
	}

	// We have to list the start & end offset for all assigned and
	// committed partitions.
	var startOffsets, endOffsets ListedOffsets
	listPartitions := described.AssignedPartitions()
	listPartitions.Merge(fetched.CommittedPartitions())
	if topics := listPartitions.Topics(); len(topics) > 0 {
		for _, list := range []struct {
			fn  func(context.Context, ...string) (ListedOffsets, error)
			dst *ListedOffsets
		}{
			{cl.ListStartOffsets, &startOffsets},
			{cl.ListEndOffsets, &endOffsets},
		} {
			listed, err := list.fn(ctx, topics...)
			*list.dst = listed
			// As above: return on auth error. If there are shard errors,
			// the topics will be missing in the response and then
			// CalculateGroupLag will return UnknownTopicOrPartition.
			switch {
			case errors.As(err, &ae):
				return nil, err
			case errors.As(err, &se):
				// do nothing: these show up as errListMissing
			case err != nil:
				return nil, err
			}
			// For anything that lists with a single -1 partition, the
			// topic does not exist. We add an UnknownTopicOrPartition
			// error for all partitions that were committed to, so that
			// this shows up in the lag output as UnknownTopicOrPartition
			// rather than errListMissing.
			for t, ps := range listed {
				if len(ps) != 1 {
					continue
				}
				if _, ok := ps[-1]; !ok {
					continue
				}
				delete(ps, -1)
				for p := range listPartitions[t] {
					ps[p] = ListedOffset{
						Topic:     t,
						Partition: p,
						Err:       kerr.UnknownTopicOrPartition,
					}
				}
			}
		}
	}

	for _, g := range described {
		l := lags[g.Group]
		l.Lag = CalculateGroupLagWithStartOffsets(g, fetched[g.Group].Fetched, startOffsets, endOffsets)
		lags[g.Group] = l
	}
	return lags, nil
}

var noOffsets = make(ListedOffsets)

// CalculateGroupLagWithStartOffsets returns the per-partition lag of all
// members in a group. This function slightly expands on [CalculateGroupLag] to
// handle calculating lag for partitions that (1) have no commits AND (2) have
// some segments deleted (cleanup.policy=delete) such that the log start offset
// is non-zero.
//
// As an example, if a group is consuming a partition with log end offset 30
// and log start offset 10 and has not yet committed to the group, this
// function can correctly tell you that the lag is 20, whereas
// CalculateGroupLag would tell you the lag is 30.
//
// This function accepts 'nil' for startOffsets, which will result in the same
// behavior as CalculateGroupLag. This function is useful if you have
// infrequently committing groups against topics that have segments being
// deleted.
func CalculateGroupLagWithStartOffsets(
	group DescribedGroup,
	commit OffsetResponses,
	startOffsets ListedOffsets,
	endOffsets ListedOffsets,
) GroupLag {
	if commit == nil { // avoid panics below
		commit = make(OffsetResponses)
	}
	if startOffsets == nil {
		startOffsets = noOffsets
	}
	if endOffsets == nil {
		endOffsets = noOffsets
	}
	if group.State == "Empty" {
		return calculateEmptyLag(commit, startOffsets, endOffsets)
	}

	l := make(map[string]map[int32]GroupMemberLag)
	for mi, m := range group.Members {
		c, ok := m.Assigned.AsConsumer()
		if !ok {
			continue
		}
		for _, t := range c.Topics {
			lt := l[t.Topic]
			if lt == nil {
				lt = make(map[int32]GroupMemberLag)
				l[t.Topic] = lt
			}

			tcommit := commit[t.Topic]
			tstart := startOffsets[t.Topic]
			tend := endOffsets[t.Topic]
			for _, p := range t.Partitions {
				var (
					pcommit = OffsetResponse{Offset: Offset{
						Topic:     t.Topic,
						Partition: p,
						At:        -1,
					}}
					pend = ListedOffset{
						Topic:     t.Topic,
						Partition: p,
						Err:       errListMissing,
					}
					pstart = pend
					perr   error
				)

				if tcommit != nil {
					if pcommitActual, ok := tcommit[p]; ok {
						pcommit = pcommitActual
					}
				}
				perr = errListMissing
				if tend != nil {
					if pendActual, ok := tend[p]; ok {
						pend = pendActual
						perr = nil
					}
				}
				if perr == nil {
					if perr = pcommit.Err; perr == nil {
						perr = pend.Err
					}
				}
				if tstart != nil {
					if pstartActual, ok := tstart[p]; ok {
						pstart = pstartActual
					}
				}

				lag := int64(-1)
				if perr == nil {
					lag = pend.Offset
					if pstart.Err == nil {
						lag = pend.Offset - pstart.Offset
					}
					if pcommit.At >= 0 {
						lag = pend.Offset - pcommit.At
					}
					// It is possible for a commit to be after the
					// end, in which case we will round to 0. We do
					// this check here to also handle a potential non-commit
					// weird pend < pstart scenario where a segment
					// was deleted between listing offsets.
					if lag < 0 {
						lag = 0
					}
				}

				lt[p] = GroupMemberLag{
					Member:    &group.Members[mi],
					Topic:     t.Topic,
					Partition: p,
					Commit:    pcommit.Offset,
					Start:     pstart,
					End:       pend,
					Lag:       lag,
					Err:       perr,
				}

			}
		}
	}

	return l
}

// CalculateGroupLag returns the per-partition lag of all members in a group.
// The input to this method is the returns from the following methods (make
// sure to check shard errors):
//
//	// Note that FetchOffsets exists to fetch only one group's offsets,
//	// but some of the code below slightly changes.
//	groups := DescribeGroups(ctx, group)
//	commits := FetchManyOffsets(ctx, group)
//	var endOffsets ListedOffsets
//	listPartitions := described.AssignedPartitions()
//	listPartitions.Merge(commits.CommittedPartitions()
//	if topics := listPartitions.Topics(); len(topics) > 0 {
//		endOffsets = ListEndOffsets(ctx, listPartitions.Topics())
//	}
//	for _, group := range groups {
//		lag := CalculateGroupLag(group, commits[group.Group].Fetched, endOffsets)
//	}
//
// If assigned partitions are missing in the listed end offsets, the partition
// will have an error indicating it is missing. A missing topic or partition in
// the commits is assumed to be nothing committing yet.
func CalculateGroupLag(
	group DescribedGroup,
	commit OffsetResponses,
	endOffsets ListedOffsets,
) GroupLag {
	return CalculateGroupLagWithStartOffsets(group, commit, nil, endOffsets)
}

func calculateEmptyLag(commit OffsetResponses, startOffsets, endOffsets ListedOffsets) GroupLag {
	l := make(map[string]map[int32]GroupMemberLag)
	for t, ps := range commit {
		lt := l[t]
		if lt == nil {
			lt = make(map[int32]GroupMemberLag)
			l[t] = lt
		}
		tstart := startOffsets[t]
		tend := endOffsets[t]
		for p, pcommit := range ps {
			var (
				pend = ListedOffset{
					Topic:     t,
					Partition: p,
					Err:       errListMissing,
				}
				pstart = pend
				perr   error
			)

			// In order of priority, perr (the error on the Lag
			// calculation) is non-nil if:
			//
			//  * The topic is missing from end ListOffsets
			//  * The partition is missing from end ListOffsets
			//  * OffsetFetch has an error on the partition
			//  * ListOffsets has an error on the partition
			//
			// If we have no error, then we can calculate lag.
			// We *do* allow an error on start ListedOffsets;
			// if there are no start offsets or the start offset
			// has an error, it is not used for lag calculation.
			perr = errListMissing
			if tend != nil {
				if pendActual, ok := tend[p]; ok {
					pend = pendActual
					perr = nil
				}
			}
			if perr == nil {
				if perr = pcommit.Err; perr == nil {
					perr = pend.Err
				}
			}
			if tstart != nil {
				if pstartActual, ok := tstart[p]; ok {
					pstart = pstartActual
				}
			}

			lag := int64(-1)
			if perr == nil {
				lag = pend.Offset
				if pstart.Err == nil {
					lag = pend.Offset - pstart.Offset
				}
				if pcommit.At >= 0 {
					lag = pend.Offset - pcommit.At
				}
				if lag < 0 {
					lag = 0
				}
			}

			lt[p] = GroupMemberLag{
				Topic:     t,
				Partition: p,
				Commit:    pcommit.Offset,
				Start:     pstart,
				End:       pend,
				Lag:       lag,
				Err:       perr,
			}
		}
	}

	// Now we look at all topics that we calculated lag for, and check out
	// the partitions we listed. If those partitions are missing from the
	// lag calculations above, the partitions were not committed to and we
	// count that as entirely lagging.
	for t, lt := range l {
		tstart := startOffsets[t]
		tend := endOffsets[t]
		for p, pend := range tend {
			if _, ok := lt[p]; ok {
				continue
			}
			pcommit := Offset{
				Topic:       t,
				Partition:   p,
				At:          -1,
				LeaderEpoch: -1,
			}
			perr := pend.Err
			lag := int64(-1)
			if perr == nil {
				lag = pend.Offset
			}
			pstart := ListedOffset{
				Topic:     t,
				Partition: p,
				Err:       errListMissing,
			}
			if tstart != nil {
				if pstartActual, ok := tstart[p]; ok {
					pstart = pstartActual
					if pstart.Err == nil {
						lag = pend.Offset - pstart.Offset
						if lag < 0 {
							lag = 0
						}
					}
				}
			}
			lt[p] = GroupMemberLag{
				Topic:     t,
				Partition: p,
				Commit:    pcommit,
				Start:     pstart,
				End:       pend,
				Lag:       lag,
				Err:       perr,
			}
		}
	}

	return l
}

var errListMissing = errors.New("missing from list offsets")
