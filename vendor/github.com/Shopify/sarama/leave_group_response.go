package sarama

type MemberResponse struct {
	MemberId        string
	GroupInstanceId *string
	Err             KError
}
type LeaveGroupResponse struct {
	Version      int16
	ThrottleTime int32
	Err          KError
	Members      []MemberResponse
}

func (r *LeaveGroupResponse) encode(pe packetEncoder) error {
	if r.Version >= 1 {
		pe.putInt32(r.ThrottleTime)
	}
	pe.putInt16(int16(r.Err))
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
			pe.putInt16(int16(member.Err))
		}
	}
	return nil
}

func (r *LeaveGroupResponse) decode(pd packetDecoder, version int16) (err error) {
	r.Version = version
	if r.Version >= 1 {
		if r.ThrottleTime, err = pd.getInt32(); err != nil {
			return err
		}
	}
	kerr, err := pd.getInt16()
	if err != nil {
		return err
	}
	r.Err = KError(kerr)

	if r.Version >= 3 {
		membersLen, err := pd.getArrayLength()
		if err != nil {
			return err
		}
		r.Members = make([]MemberResponse, membersLen)
		for i := 0; i < len(r.Members); i++ {
			if r.Members[i].MemberId, err = pd.getString(); err != nil {
				return err
			}
			if r.Members[i].GroupInstanceId, err = pd.getNullableString(); err != nil {
				return err
			}
			if memberErr, err := pd.getInt16(); err != nil {
				return err
			} else {
				r.Members[i].Err = KError(memberErr)
			}
		}
	}

	return nil
}

func (r *LeaveGroupResponse) key() int16 {
	return 13
}

func (r *LeaveGroupResponse) version() int16 {
	return r.Version
}

func (r *LeaveGroupResponse) headerVersion() int16 {
	return 0
}

func (r *LeaveGroupResponse) requiredVersion() KafkaVersion {
	switch r.Version {
	case 1, 2, 3:
		return V2_3_0_0
	}
	return V0_9_0_0
}
