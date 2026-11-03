package sarama

import "time"

// ConsumerGroupDescribeResponse contains descriptions of KIP-848 consumer groups.
type ConsumerGroupDescribeResponse struct {
	Version      int16
	ThrottleTime time.Duration
	Groups       []ConsumerGroupDescription
}

// ConsumerGroupDescription contains the state and membership of a consumer group.
type ConsumerGroupDescription struct {
	ErrorCode            KError
	ErrorMessage         *string
	GroupID              string
	GroupState           string
	GroupEpoch           int32
	AssignmentEpoch      int32
	AssignorName         string
	Members              []ConsumerGroupMemberDescription
	AuthorizedOperations int32
}

// ConsumerGroupMemberDescription describes a member of a KIP-848 consumer group.
type ConsumerGroupMemberDescription struct {
	MemberID             string
	InstanceID           *string
	RackID               *string
	MemberEpoch          int32
	ClientID             string
	ClientHost           string
	SubscribedTopicNames []string
	SubscribedTopicRegex *string
	Assignment           ConsumerGroupAssignment
	TargetAssignment     ConsumerGroupAssignment
	MemberType           int8
}

// ConsumerGroupAssignment contains a member's current or target assignment.
type ConsumerGroupAssignment struct {
	TopicPartitions []ConsumerGroupTopicPartitions
}

// ConsumerGroupTopicPartitions contains the assigned partitions of a topic.
type ConsumerGroupTopicPartitions struct {
	TopicID    Uuid
	TopicName  string
	Partitions []int32
}

func (t *ConsumerGroupTopicPartitions) encode(pe packetEncoder) error {
	if err := pe.putUuid(t.TopicID); err != nil {
		return err
	}

	if err := pe.putString(t.TopicName); err != nil {
		return err
	}

	if err := pe.putInt32Array(t.Partitions); err != nil {
		return err
	}

	pe.putEmptyTaggedFieldArray()

	return nil
}

func (t *ConsumerGroupTopicPartitions) decode(pd packetDecoder, version int16) (err error) {
	if t.TopicID, err = pd.getUuid(); err != nil {
		return err
	}

	if t.TopicName, err = pd.getString(); err != nil {
		return err
	}

	if t.Partitions, err = pd.getInt32Array(); err != nil {
		return err
	}

	_, err = pd.getEmptyTaggedFieldArray()

	return err
}

func (a *ConsumerGroupAssignment) encode(pe packetEncoder) error {
	if err := pe.putArrayLength(len(a.TopicPartitions)); err != nil {
		return err
	}

	for i := range a.TopicPartitions {
		if err := a.TopicPartitions[i].encode(pe); err != nil {
			return err
		}
	}

	pe.putEmptyTaggedFieldArray()

	return nil
}

func (a *ConsumerGroupAssignment) decode(pd packetDecoder, version int16) (err error) {
	n, err := pd.getArrayLength()

	if err != nil {
		return err
	}

	if n < 0 {
		return errInvalidArrayLength
	}

	a.TopicPartitions = make([]ConsumerGroupTopicPartitions, n)

	for i := range a.TopicPartitions {
		if err := a.TopicPartitions[i].decode(pd, version); err != nil {
			return err
		}
	}

	_, err = pd.getEmptyTaggedFieldArray()

	return err
}

func (m *ConsumerGroupMemberDescription) encode(pe packetEncoder, version int16) error {
	if err := pe.putString(m.MemberID); err != nil {
		return err
	}

	if err := pe.putNullableString(m.InstanceID); err != nil {
		return err
	}

	if err := pe.putNullableString(m.RackID); err != nil {
		return err
	}

	pe.putInt32(m.MemberEpoch)

	if err := pe.putString(m.ClientID); err != nil {
		return err
	}

	if err := pe.putString(m.ClientHost); err != nil {
		return err
	}

	if err := pe.putStringArray(m.SubscribedTopicNames); err != nil {
		return err
	}

	if err := pe.putNullableString(m.SubscribedTopicRegex); err != nil {
		return err
	}

	if err := m.Assignment.encode(pe); err != nil {
		return err
	}

	if err := m.TargetAssignment.encode(pe); err != nil {
		return err
	}

	if version >= 1 {
		pe.putInt8(m.MemberType)
	}

	pe.putEmptyTaggedFieldArray()

	return nil
}

func (m *ConsumerGroupMemberDescription) decode(pd packetDecoder, version int16) (err error) {
	if m.MemberID, err = pd.getString(); err != nil {
		return err
	}

	if m.InstanceID, err = pd.getNullableString(); err != nil {
		return err
	}

	if m.RackID, err = pd.getNullableString(); err != nil {
		return err
	}

	if m.MemberEpoch, err = pd.getInt32(); err != nil {
		return err
	}

	if m.ClientID, err = pd.getString(); err != nil {
		return err
	}

	if m.ClientHost, err = pd.getString(); err != nil {
		return err
	}

	if m.SubscribedTopicNames, err = pd.getStringArray(); err != nil {
		return err
	}

	if m.SubscribedTopicRegex, err = pd.getNullableString(); err != nil {
		return err
	}

	if err := m.Assignment.decode(pd, version); err != nil {
		return err
	}

	if err := m.TargetAssignment.decode(pd, version); err != nil {
		return err
	}

	m.MemberType = -1

	if version >= 1 {
		if m.MemberType, err = pd.getInt8(); err != nil {
			return err
		}
	}

	_, err = pd.getEmptyTaggedFieldArray()

	return err
}

func (g *ConsumerGroupDescription) encode(pe packetEncoder, version int16) error {
	pe.putKError(g.ErrorCode)

	if err := pe.putNullableString(g.ErrorMessage); err != nil {
		return err
	}

	if err := pe.putString(g.GroupID); err != nil {
		return err
	}

	if err := pe.putString(g.GroupState); err != nil {
		return err
	}

	pe.putInt32(g.GroupEpoch)

	pe.putInt32(g.AssignmentEpoch)

	if err := pe.putString(g.AssignorName); err != nil {
		return err
	}

	if err := pe.putArrayLength(len(g.Members)); err != nil {
		return err
	}

	for i := range g.Members {
		if err := g.Members[i].encode(pe, version); err != nil {
			return err
		}
	}

	pe.putInt32(g.AuthorizedOperations)

	pe.putEmptyTaggedFieldArray()

	return nil
}

func (g *ConsumerGroupDescription) decode(pd packetDecoder, version int16) (err error) {
	if g.ErrorCode, err = pd.getKError(); err != nil {
		return err
	}

	if g.ErrorMessage, err = pd.getNullableString(); err != nil {
		return err
	}

	if g.GroupID, err = pd.getString(); err != nil {
		return err
	}

	if g.GroupState, err = pd.getString(); err != nil {
		return err
	}

	if g.GroupEpoch, err = pd.getInt32(); err != nil {
		return err
	}

	if g.AssignmentEpoch, err = pd.getInt32(); err != nil {
		return err
	}

	if g.AssignorName, err = pd.getString(); err != nil {
		return err
	}

	n, err := pd.getArrayLength()

	if err != nil {
		return err
	}

	if n < 0 {
		return errInvalidArrayLength
	}

	g.Members = make([]ConsumerGroupMemberDescription, n)

	for i := range g.Members {
		if err := g.Members[i].decode(pd, version); err != nil {
			return err
		}
	}

	if g.AuthorizedOperations, err = pd.getInt32(); err != nil {
		return err
	}

	_, err = pd.getEmptyTaggedFieldArray()

	return err
}

func (r *ConsumerGroupDescribeResponse) encode(pe packetEncoder) error {
	if !r.isValidVersion() {
		return PacketEncodingError{"invalid or unsupported ConsumerGroupDescribeResponse version"}
	}

	pe.putDurationMs(r.ThrottleTime)

	if err := pe.putArrayLength(len(r.Groups)); err != nil {
		return err
	}

	for i := range r.Groups {
		if err := r.Groups[i].encode(pe, r.Version); err != nil {
			return err
		}
	}

	pe.putEmptyTaggedFieldArray()

	return nil
}

func (r *ConsumerGroupDescribeResponse) decode(pd packetDecoder, version int16) (err error) {
	r.Version = version

	if !r.isValidVersion() {
		return PacketDecodingError{"invalid or unsupported ConsumerGroupDescribeResponse version"}
	}

	if r.ThrottleTime, err = pd.getDurationMs(); err != nil {
		return err
	}

	n, err := pd.getArrayLength()

	if err != nil {
		return err
	}

	if n < 0 {
		return errInvalidArrayLength
	}

	r.Groups = make([]ConsumerGroupDescription, n)

	for i := range r.Groups {
		if err := r.Groups[i].decode(pd, version); err != nil {
			return err
		}
	}

	_, err = pd.getEmptyTaggedFieldArray()

	return err
}

func (r *ConsumerGroupDescribeResponse) key() int16 { return apiKeyConsumerGroupDescribe }

func (r *ConsumerGroupDescribeResponse) version() int16 { return r.Version }

func (r *ConsumerGroupDescribeResponse) setVersion(v int16) { r.Version = v }

func (r *ConsumerGroupDescribeResponse) headerVersion() int16 { return 1 }

func (r *ConsumerGroupDescribeResponse) isValidVersion() bool {
	return r.Version >= 0 && r.Version <= 1
}

func (r *ConsumerGroupDescribeResponse) isFlexible() bool { return true }

func (r *ConsumerGroupDescribeResponse) isFlexibleVersion(version int16) bool { return version >= 0 }

func (r *ConsumerGroupDescribeResponse) requiredVersion() KafkaVersion {
	switch r.Version {
	case 1:
		return V4_0_0_0
	default:
		return V3_7_0_0
	}
}

func (r *ConsumerGroupDescribeResponse) throttleTime() time.Duration { return r.ThrottleTime }
