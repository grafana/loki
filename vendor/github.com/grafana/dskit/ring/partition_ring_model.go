package ring

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/gogo/protobuf/proto"

	"github.com/grafana/dskit/kv/codec"
	"github.com/grafana/dskit/kv/memberlist"
)

type partitionRingCodec struct {
	codec.Codec
}

// Decode wraps Codec.Decode and ensure PartitionRingDesc maps are not nil.
func (c *partitionRingCodec) Decode(in []byte) (interface{}, error) {
	out, err := c.Codec.Decode(in)
	if err != nil {
		return out, err
	}

	// Ensure maps are initialised. This makes working with PartitionRingDesc more convenient.
	if actual, ok := out.(*PartitionRingDesc); ok {
		if actual.Partitions == nil {
			actual.Partitions = map[int32]PartitionDesc{}
		}
		if actual.Owners == nil {
			actual.Owners = map[string]OwnerDesc{}
		}
	}

	return out, nil
}

func GetPartitionRingCodec() codec.Codec {
	return &partitionRingCodec{
		Codec: codec.NewProtoCodec("partitionRingDesc", PartitionRingDescFactory),
	}
}

// PartitionRingDescFactory makes new PartitionRingDesc.
func PartitionRingDescFactory() proto.Message {
	return NewPartitionRingDesc()
}

func GetOrCreatePartitionRingDesc(in any) *PartitionRingDesc {
	if in == nil {
		return NewPartitionRingDesc()
	}

	desc := in.(*PartitionRingDesc)
	if desc == nil {
		return NewPartitionRingDesc()
	}

	return desc
}

func NewPartitionRingDesc() *PartitionRingDesc {
	return &PartitionRingDesc{
		Partitions: map[int32]PartitionDesc{},
		Owners:     map[string]OwnerDesc{},
	}
}

// ownersByPartition returns a map where the key is the partition ID and the value is a list of owner IDs.
func (m *PartitionRingDesc) ownersByPartition() map[int32][]string {
	out := make(map[int32][]string, len(m.Partitions))
	for id, o := range m.Owners {
		out[o.OwnedPartition] = append(out[o.OwnedPartition], id)
	}

	// Sort owners to have predictable tests.
	for id := range out {
		slices.Sort(out[id])
	}

	return out
}

// countPartitionsByState returns a map containing the number of partitions by state.
func (m *PartitionRingDesc) countPartitionsByState() map[PartitionState]int {
	// Init the map to have to zero values for all states.
	out := make(map[PartitionState]int, len(PartitionState_value)-2)
	for _, state := range PartitionState_value {
		if PartitionState(state) == PartitionUnknown || PartitionState(state) == PartitionDeleted {
			continue
		}

		out[PartitionState(state)] = 0
	}

	for _, partition := range m.Partitions {
		out[partition.State]++
	}

	return out
}

func (m *PartitionRingDesc) activePartitionsCount() int {
	count := 0
	for _, partition := range m.Partitions {
		if partition.IsActive() {
			count++
		}
	}
	return count
}

func (m *PartitionRingDesc) maxPartitionID() int32 {
	var max int32 = -1
	for id := range m.Partitions {
		if id > max {
			max = id
		}
	}
	return max
}

// WithPartitions returns a new PartitionRingDesc with only the specified partitions and their owners included.
func (m *PartitionRingDesc) WithPartitions(partitions map[int32]struct{}) PartitionRingDesc {
	newPartitions := make(map[int32]PartitionDesc, len(partitions))
	newOwners := make(map[string]OwnerDesc, len(partitions)*2) // assuming two owners per partition.

	for pid, p := range m.Partitions {
		if _, ok := partitions[pid]; ok {
			newPartitions[pid] = p
		}
	}

	for oid, o := range m.Owners {
		if _, ok := partitions[o.OwnedPartition]; ok {
			newOwners[oid] = o
		}
	}

	return PartitionRingDesc{
		Partitions: newPartitions,
		Owners:     newOwners,
	}
}

// AddPartition adds a new partition to the ring. Tokens are auto-generated using the spread minimizing strategy
// which generates deterministic unique tokens.
func (m *PartitionRingDesc) AddPartition(id int32, state PartitionState, now time.Time) {
	// Spread-minimizing token generator is deterministic unique-token generator for given id and zone.
	// Partitions don't use zones.
	spreadMinimizing := NewSpreadMinimizingTokenGeneratorForInstanceAndZoneID("", int(id), 0, false)

	m.Partitions[id] = PartitionDesc{
		Id:             id,
		TokenScheme:    PartitionTokensStored,
		Tokens:         spreadMinimizing.GenerateTokens(optimalTokensPerInstance, nil),
		State:          state,
		StateTimestamp: now.Unix(),
	}
}

// AddPartitionWithDerivedTokens adds a partition using PartitionTokensSmt512.
func (m *PartitionRingDesc) AddPartitionWithDerivedTokens(id int32, state PartitionState, now time.Time) {
	m.Partitions[id] = PartitionDesc{
		Id:             id,
		TokenScheme:    PartitionTokensSmt512,
		Tokens:         nil, // Tokens will be derived by readers
		State:          state,
		StateTimestamp: now.Unix(),
	}
}

// UpdatePartitionState changes the state of a partition. Returns true if the state was changed,
// or false if the update was a no-op.
func (m *PartitionRingDesc) UpdatePartitionState(id int32, state PartitionState, now time.Time) (bool, error) {
	d, ok := m.Partitions[id]
	if !ok {
		return false, nil
	}

	if d.State == state {
		return false, nil
	}

	if d.StateChangeLocked {
		return false, ErrPartitionStateChangeLocked
	}

	d.State = state
	d.StateTimestamp = now.Unix()
	m.Partitions[id] = d
	return true, nil
}

// UpdatePartitionStateChangeLock changes the state change lock of a partition. Returns true if the lock was changed,
// or false if the update was a no-op.
func (m *PartitionRingDesc) UpdatePartitionStateChangeLock(id int32, locked bool, now time.Time) bool {
	d, ok := m.Partitions[id]
	if !ok {
		return false
	}

	if d.StateChangeLocked == locked {
		return false
	}

	d.StateChangeLocked = locked
	d.StateChangeLockedTimestamp = now.Unix()
	m.Partitions[id] = d
	return true
}

// RemovePartition removes a partition.
func (m *PartitionRingDesc) RemovePartition(id int32) {
	delete(m.Partitions, id)
}

// HasPartition returns whether a partition exists.
func (m *PartitionRingDesc) HasPartition(id int32) bool {
	_, ok := m.Partitions[id]
	return ok
}

// AddOrUpdateOwner adds or updates a partition owner in the ring. Returns true, if the
// owner was added or updated, false if it was left unchanged.
func (m *PartitionRingDesc) AddOrUpdateOwner(id string, state OwnerState, ownedPartition int32, now time.Time) bool {
	prev, ok := m.Owners[id]
	updated := OwnerDesc{
		State:          state,
		OwnedPartition: ownedPartition,

		// Preserve the previous timestamp so that we'll NOT compare it.
		// Then, if we detect that the OwnerDesc should be updated, we'll
		// also update the UpdateTimestamp.
		UpdatedTimestamp: prev.UpdatedTimestamp,
	}

	if ok && prev.Equal(updated) {
		return false
	}

	updated.UpdatedTimestamp = now.Unix()
	m.Owners[id] = updated

	return true
}

// RemoveOwner removes a partition owner. Returns true if the ring has been changed.
func (m *PartitionRingDesc) RemoveOwner(id string) bool {
	if _, ok := m.Owners[id]; !ok {
		return false
	}

	delete(m.Owners, id)
	return true
}

// HasOwner returns whether a owner exists.
func (m *PartitionRingDesc) HasOwner(id string) bool {
	_, ok := m.Owners[id]
	return ok
}

// PartitionOwnersCount returns the number of owners for a given partition.
func (m *PartitionRingDesc) PartitionOwnersCount(partitionID int32) int {
	count := 0
	for _, o := range m.Owners {
		if o.OwnedPartition == partitionID {
			count++
		}
	}
	return count
}

// PartitionOwnersCountUpdatedBefore returns the number of owners for a given partition,
// including only owners which have been updated the last time before the input timestamp.
func (m *PartitionRingDesc) PartitionOwnersCountUpdatedBefore(partitionID int32, before time.Time) int {
	count := 0
	beforeSeconds := before.Unix()

	for _, o := range m.Owners {
		if o.OwnedPartition == partitionID && o.GetUpdatedTimestamp() < beforeSeconds {
			count++
		}
	}
	return count
}

// Merge implements memberlist.Mergeable.
//
// Timestamps more than maxFutureTimestampSkew ahead of the local clock are handled
// as described in Desc.Merge.
func (m *PartitionRingDesc) Merge(mergeable memberlist.Mergeable, localCAS bool) (memberlist.Mergeable, error) {
	return m.mergeWithTime(mergeable, localCAS, time.Now())
}

func (m *PartitionRingDesc) mergeWithTime(mergeable memberlist.Mergeable, localCAS bool, now time.Time) (memberlist.Mergeable, error) {
	if mergeable == nil {
		return nil, nil
	}

	other, ok := mergeable.(*PartitionRingDesc)
	if !ok {
		return nil, fmt.Errorf("expected *PartitionRingDesc, got %T", mergeable)
	}

	if other == nil {
		return nil, nil
	}

	change := NewPartitionRingDesc()
	timestampLimit := futureTimestampLimit(now)

	// Handle partitions.
	for id, otherPart := range other.Partitions {
		changed := false

		thisPart, exists := m.Partitions[id]
		if !exists {
			if otherPart.StateTimestamp > timestampLimit {
				// Written by a node with a clock too far ahead. Accepting it would block all later updates.
				// A state change lock timestamp too far ahead is kept, because any valid one replaces it.
				continue
			}

			changed = true
			thisPart = otherPart
		} else {
			// We don't merge changes to partition ID, tokens and token scheme because we expect them to be immutable.
			//
			// If in the future we'll change the tokens generation algorithm and we'll have to handle migration to
			// a different set of tokens then we'll add the support. For example, we could add a new token scheme
			// and preserve tokens generated by the latest scheme only, or a timestamp for tokens update too.

			// Timestamps from a node with a clock too far ahead are ignored when incoming,
			// and replaceable by any valid incoming timestamp when local.
			thisStateTimestamp := localMergeTimestamp(thisPart.StateTimestamp, timestampLimit)
			thisStateChangeLockedTimestamp := localMergeTimestamp(thisPart.StateChangeLockedTimestamp, timestampLimit)

			// In case the timestamp is equal we give priority to the deleted state.
			// Reason is that timestamp has second precision, so we cover the case an
			// update and subsequent deletion occur within the same second.
			if otherPart.StateTimestamp <= timestampLimit && (otherPart.StateTimestamp > thisStateTimestamp || (otherPart.StateTimestamp == thisStateTimestamp && otherPart.State == PartitionDeleted && thisPart.State != PartitionDeleted)) {
				changed = true

				thisPart.State = otherPart.State
				thisPart.StateTimestamp = otherPart.StateTimestamp
			}

			if otherPart.StateChangeLockedTimestamp <= timestampLimit && otherPart.StateChangeLockedTimestamp > thisStateChangeLockedTimestamp {
				changed = true

				thisPart.StateChangeLocked = otherPart.StateChangeLocked
				thisPart.StateChangeLockedTimestamp = otherPart.StateChangeLockedTimestamp
			}
		}

		if changed {
			m.Partitions[id] = thisPart
			change.Partitions[id] = thisPart
		}
	}

	if localCAS {
		// Let's mark all missing partitions in incoming change as deleted.
		// This breaks commutativity! But we only do it locally, not when gossiping with others.
		for pid, thisPart := range m.Partitions {
			if _, exists := other.Partitions[pid]; !exists && thisPart.State != PartitionDeleted {
				// Partition was removed from the ring. We need to preserve it locally, but we set state to PartitionDeleted.
				thisPart.State = PartitionDeleted
				thisPart.StateTimestamp = now.Unix()
				m.Partitions[pid] = thisPart
				change.Partitions[pid] = thisPart
			}
		}
	}

	// Now let's handle owners.
	for id, otherOwner := range other.Owners {
		if otherOwner.UpdatedTimestamp > timestampLimit {
			// Written by a node with a clock too far ahead. Accepting it would block all later updates.
			continue
		}

		thisOwner := m.Owners[id]
		thisUpdatedTimestamp := localMergeTimestamp(thisOwner.UpdatedTimestamp, timestampLimit)

		// In case the timestamp is equal we give priority to the deleted state.
		// Reason is that timestamp has second precision, so we cover the case an
		// update and subsequent deletion occur within the same second.
		if otherOwner.UpdatedTimestamp > thisUpdatedTimestamp || (otherOwner.UpdatedTimestamp == thisUpdatedTimestamp && otherOwner.State == OwnerDeleted && thisOwner.State != OwnerDeleted) {
			m.Owners[id] = otherOwner
			change.Owners[id] = otherOwner
		}
	}

	if localCAS {
		// Mark all missing owners as deleted.
		// This breaks commutativity! But we only do it locally, not when gossiping with others.
		for id, thisOwner := range m.Owners {
			if _, exists := other.Owners[id]; !exists && thisOwner.State != OwnerDeleted {
				// Owner was removed from the ring. We need to preserve it locally, but we set state to OwnerDeleted.
				thisOwner.State = OwnerDeleted
				thisOwner.UpdatedTimestamp = now.Unix()
				m.Owners[id] = thisOwner
				change.Owners[id] = thisOwner
			}
		}
	}

	// If nothing changed, report nothing.
	if len(change.Partitions) == 0 && len(change.Owners) == 0 {
		return nil, nil
	}

	return change, nil
}

// MergeContent implements memberlist.Mergeable.
func (m *PartitionRingDesc) MergeContent() []string {
	result := make([]string, 0, len(m.Partitions)+len(m.Owners))

	// We're assuming that partition IDs and instance IDs are not colliding (ie. no instance is called "1").
	for pid := range m.Partitions {
		result = append(result, strconv.Itoa(int(pid)))
	}

	for id := range m.Owners {
		result = append(result, id)
	}
	return result
}

// RemoveTombstones implements memberlist.Mergeable.
func (m *PartitionRingDesc) RemoveTombstones(limit time.Time) (total, removed int) {
	for pid, part := range m.Partitions {
		if part.State == PartitionDeleted {
			if limit.IsZero() || time.Unix(part.StateTimestamp, 0).Before(limit) {
				delete(m.Partitions, pid)
				removed++
			} else {
				total++
			}
		}
	}

	for n, owner := range m.Owners {
		if owner.State == OwnerDeleted {
			if limit.IsZero() || time.Unix(owner.UpdatedTimestamp, 0).Before(limit) {
				delete(m.Owners, n)
				removed++
			} else {
				total++
			}
		}
	}

	return
}

// Clone copies the ring description and its maps. Partition tokens remain shared
// and must be treated as immutable.
func (m *PartitionRingDesc) Clone() memberlist.Mergeable {
	// Non-nullable protobuf map values already share their token slices; typed
	// copies preserve that ownership without reflection on every memberlist read.
	clone := &PartitionRingDesc{}
	// Size by live entries so removed partitions and owners don't retain capacity.
	if m.Partitions != nil {
		clone.Partitions = make(map[int32]PartitionDesc, len(m.Partitions))
		maps.Copy(clone.Partitions, m.Partitions)
	}
	if m.Owners != nil {
		clone.Owners = make(map[string]OwnerDesc, len(m.Owners))
		maps.Copy(clone.Owners, m.Owners)
	}
	return clone
}

func (m *PartitionDesc) IsPending() bool {
	return m.GetState() == PartitionPending
}

func (m *PartitionDesc) IsActive() bool {
	return m.GetState() == PartitionActive
}

func (m *PartitionDesc) IsInactive() bool {
	return m.GetState() == PartitionInactive
}

func (m *PartitionDesc) IsInactiveSince(since time.Time) bool {
	return m.IsInactive() && m.GetStateTimestamp() < since.Unix()
}

func (m *PartitionDesc) GetStateTime() time.Time {
	return time.Unix(m.GetStateTimestamp(), 0)
}

func (m *PartitionDesc) Clone() PartitionDesc {
	return *(proto.Clone(m).(*PartitionDesc))
}

// CleanName returns the PartitionState name without the "Partition" prefix.
func (s PartitionState) CleanName() string {
	return strings.TrimPrefix(s.String(), "Partition")
}

// CleanName returns the PartitionTokenScheme name without the "PartitionTokens" prefix.
func (s PartitionTokenScheme) CleanName() string {
	return strings.TrimPrefix(s.String(), "PartitionTokens")
}
