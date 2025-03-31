package sarama

import "time"

type JoinGroupResponse struct {
	// Version defines the protocol version to use for encode and decode
	Version int16
	// ThrottleTime contains the duration for which the request was throttled due
	// to a quota violation, or zero if the request did not violate any quota.
	ThrottleTime int32
	// Err contains the error code, or 0 if there was no error.
	Err KError
	// GenerationId contains the generation ID of the group.
	GenerationId int32
	// GroupProtocol contains the group protocol selected by the coordinator.
	GroupProtocol string
	// LeaderId contains the leader of the group.
	LeaderId string
	// MemberId contains the member ID assigned by the group coordinator.
	MemberId string
	// Members contains the per-group-member information.
	Members []GroupMember
}

type GroupMember struct {
	// MemberId contains the group member ID.
	MemberId string
	// GroupInstanceId contains the unique identifier of the consumer instance
	// provided by end user.
	GroupInstanceId *string
	// Metadata contains the group member metadata.
	Metadata []byte
}

func (r *JoinGroupResponse) GetMembers() (map[string]ConsumerGroupMemberMetadata, error) {
	members := make(map[string]ConsumerGroupMemberMetadata, len(r.Members))
	for _, member := range r.Members {
		meta := new(ConsumerGroupMemberMetadata)
		if err := decode(member.Metadata, meta, nil); err != nil {
			return nil, err
		}
		members[member.MemberId] = *meta
	}
	return members, nil
}

func (r *JoinGroupResponse) encode(pe packetEncoder) error {
	if r.Version >= 2 {
		pe.putInt32(r.ThrottleTime)
	}
	pe.putInt16(int16(r.Err))
	pe.putInt32(r.GenerationId)

	if err := pe.putString(r.GroupProtocol); err != nil {
		return err
	}
	if err := pe.putString(r.LeaderId); err != nil {
		return err
	}
	if err := pe.putString(r.MemberId); err != nil {
		return err
	}

	if err := pe.putArrayLength(len(r.Members)); err != nil {
		return err
	}

	for _, member := range r.Members {
		if err := pe.putString(member.MemberId); err != nil {
			return err
		}
		if r.Version >= 5 {
			if err := pe.putNullableString(member.GroupInstanceId); err != nil {
				return err
			}
		}
		if err := pe.putBytes(member.Metadata); err != nil {
			return err
		}
	}

	return nil
}

func (r *JoinGroupResponse) decode(pd packetDecoder, version int16) (err error) {
	r.Version = version

	if version >= 2 {
		if r.ThrottleTime, err = pd.getInt32(); err != nil {
			return
		}
	}

	kerr, err := pd.getInt16()
	if err != nil {
		return err
	}

	r.Err = KError(kerr)

	if r.GenerationId, err = pd.getInt32(); err != nil {
		return
	}

	if r.GroupProtocol, err = pd.getString(); err != nil {
		return
	}

	if r.LeaderId, err = pd.getString(); err != nil {
		return
	}

	if r.MemberId, err = pd.getString(); err != nil {
		return
	}

	n, err := pd.getArrayLength()
	if err != nil {
		return err
	}
	if n == 0 {
		return nil
	}

	r.Members = make([]GroupMember, n)
	for i := 0; i < n; i++ {
		memberId, err := pd.getString()
		if err != nil {
			return err
		}

		var groupInstanceId *string = nil
		if r.Version >= 5 {
			groupInstanceId, err = pd.getNullableString()
			if err != nil {
				return err
			}
		}

		memberMetadata, err := pd.getBytes()
		if err != nil {
			return err
		}

		r.Members[i] = GroupMember{MemberId: memberId, GroupInstanceId: groupInstanceId, Metadata: memberMetadata}
	}

	return nil
}

func (r *JoinGroupResponse) key() int16 {
	return 11
}

func (r *JoinGroupResponse) version() int16 {
	return r.Version
}

func (r *JoinGroupResponse) headerVersion() int16 {
	return 0
}

func (r *JoinGroupResponse) isValidVersion() bool {
	return r.Version >= 0 && r.Version <= 5
}

func (r *JoinGroupResponse) requiredVersion() KafkaVersion {
	switch r.Version {
	case 5:
		return V2_3_0_0
	case 4:
		return V2_2_0_0
	case 3:
		return V2_0_0_0
	case 2:
		return V0_11_0_0
	case 1:
		return V0_10_1_0
	case 0:
		return V0_10_0_0
	default:
		return V2_3_0_0
	}
}

func (r *JoinGroupResponse) throttleTime() time.Duration {
	return time.Duration(r.ThrottleTime) * time.Millisecond
}
