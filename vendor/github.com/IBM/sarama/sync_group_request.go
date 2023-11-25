package sarama

type SyncGroupRequestAssignment struct {
	// MemberId contains the ID of the member to assign.
	MemberId string
	// Assignment contains the member assignment.
	Assignment []byte
}

func (a *SyncGroupRequestAssignment) encode(pe packetEncoder, version int16) (err error) {
	if err := pe.putString(a.MemberId); err != nil {
		return err
	}

	if err := pe.putBytes(a.Assignment); err != nil {
		return err
	}

	return nil
}

func (a *SyncGroupRequestAssignment) decode(pd packetDecoder, version int16) (err error) {
	if a.MemberId, err = pd.getString(); err != nil {
		return err
	}

	if a.Assignment, err = pd.getBytes(); err != nil {
		return err
	}

	return nil
}

type SyncGroupRequest struct {
	// Version defines the protocol version to use for encode and decode
	Version int16
	// GroupId contains the unique group identifier.
	GroupId string
	// GenerationId contains the generation of the group.
	GenerationId int32
	// MemberId contains the member ID assigned by the group.
	MemberId string
	// GroupInstanceId contains the unique identifier of the consumer instance provided by end user.
	GroupInstanceId *string
	// GroupAssignments contains each assignment.
	GroupAssignments []SyncGroupRequestAssignment
}

func (s *SyncGroupRequest) encode(pe packetEncoder) (err error) {
	if err := pe.putString(s.GroupId); err != nil {
		return err
	}

	pe.putInt32(s.GenerationId)

	if err := pe.putString(s.MemberId); err != nil {
		return err
	}

	if s.Version >= 3 {
		if err := pe.putNullableString(s.GroupInstanceId); err != nil {
			return err
		}
	}

	if err := pe.putArrayLength(len(s.GroupAssignments)); err != nil {
		return err
	}
	for _, block := range s.GroupAssignments {
		if err := block.encode(pe, s.Version); err != nil {
			return err
		}
	}

	return nil
}

func (s *SyncGroupRequest) decode(pd packetDecoder, version int16) (err error) {
	s.Version = version
	if s.GroupId, err = pd.getString(); err != nil {
		return err
	}

	if s.GenerationId, err = pd.getInt32(); err != nil {
		return err
	}

	if s.MemberId, err = pd.getString(); err != nil {
		return err
	}

	if s.Version >= 3 {
		if s.GroupInstanceId, err = pd.getNullableString(); err != nil {
			return err
		}
	}

	if numAssignments, err := pd.getArrayLength(); err != nil {
		return err
	} else if numAssignments > 0 {
		s.GroupAssignments = make([]SyncGroupRequestAssignment, numAssignments)
		for i := 0; i < numAssignments; i++ {
			var block SyncGroupRequestAssignment
			if err := block.decode(pd, s.Version); err != nil {
				return err
			}
			s.GroupAssignments[i] = block
		}
	}

	return nil
}

func (r *SyncGroupRequest) key() int16 {
	return 14
}

func (r *SyncGroupRequest) version() int16 {
	return r.Version
}

func (r *SyncGroupRequest) headerVersion() int16 {
	return 1
}

func (r *SyncGroupRequest) isValidVersion() bool {
	return r.Version >= 0 && r.Version <= 3
}

func (r *SyncGroupRequest) requiredVersion() KafkaVersion {
	switch r.Version {
	case 3:
		return V2_3_0_0
	case 2:
		return V2_0_0_0
	case 1:
		return V0_11_0_0
	case 0:
		return V0_9_0_0
	default:
		return V2_3_0_0
	}
}

func (r *SyncGroupRequest) AddGroupAssignment(memberId string, memberAssignment []byte) {
	r.GroupAssignments = append(r.GroupAssignments, SyncGroupRequestAssignment{
		MemberId:   memberId,
		Assignment: memberAssignment,
	})
}

func (r *SyncGroupRequest) AddGroupAssignmentMember(
	memberId string,
	memberAssignment *ConsumerGroupMemberAssignment,
) error {
	bin, err := encode(memberAssignment, nil)
	if err != nil {
		return err
	}

	r.AddGroupAssignment(memberId, bin)
	return nil
}
