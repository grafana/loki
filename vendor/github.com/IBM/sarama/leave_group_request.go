package sarama

type MemberIdentity struct {
	MemberId        string
	GroupInstanceId *string
}

type LeaveGroupRequest struct {
	Version  int16
	GroupId  string
	MemberId string           // Removed in Version 3
	Members  []MemberIdentity // Added in Version 3
}

func (r *LeaveGroupRequest) encode(pe packetEncoder) error {
	if err := pe.putString(r.GroupId); err != nil {
		return err
	}
	if r.Version < 3 {
		if err := pe.putString(r.MemberId); err != nil {
			return err
		}
	}
	if r.Version >= 3 {
		if err := pe.putArrayLength(len(r.Members)); err != nil {
			return err
		}
		for _, member := range r.Members {
			if err := pe.putString(member.MemberId); err != nil {
				return err
			}
			if err := pe.putNullableString(member.GroupInstanceId); err != nil {
				return err
			}
		}
	}

	return nil
}

func (r *LeaveGroupRequest) decode(pd packetDecoder, version int16) (err error) {
	r.Version = version
	if r.GroupId, err = pd.getString(); err != nil {
		return
	}
	if r.Version < 3 {
		if r.MemberId, err = pd.getString(); err != nil {
			return
		}
	}
	if r.Version >= 3 {
		memberCount, err := pd.getArrayLength()
		if err != nil {
			return err
		}
		r.Members = make([]MemberIdentity, memberCount)
		for i := 0; i < memberCount; i++ {
			memberIdentity := MemberIdentity{}
			if memberIdentity.MemberId, err = pd.getString(); err != nil {
				return err
			}
			if memberIdentity.GroupInstanceId, err = pd.getNullableString(); err != nil {
				return err
			}
			r.Members[i] = memberIdentity
		}
	}

	return nil
}

func (r *LeaveGroupRequest) key() int16 {
	return 13
}

func (r *LeaveGroupRequest) version() int16 {
	return r.Version
}

func (r *LeaveGroupRequest) headerVersion() int16 {
	return 1
}

func (r *LeaveGroupRequest) isValidVersion() bool {
	return r.Version >= 0 && r.Version <= 3
}

func (r *LeaveGroupRequest) requiredVersion() KafkaVersion {
	switch r.Version {
	case 3:
		return V2_4_0_0
	case 2:
		return V2_0_0_0
	case 1:
		return V0_11_0_0
	case 0:
		return V0_9_0_0
	default:
		return V2_4_0_0
	}
}
