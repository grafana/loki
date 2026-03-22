package sarama

import "time"

type SyncGroupResponse struct {
	// Version defines the protocol version to use for encode and decode
	Version int16
	// ThrottleTime contains the duration in milliseconds for which the
	// request was throttled due to a quota violation, or zero if the request
	// did not violate any quota.
	ThrottleTime int32
	// Err contains the error code, or 0 if there was no error.
	Err KError
	// MemberAssignment contains the member assignment.
	MemberAssignment []byte
}

func (r *SyncGroupResponse) setVersion(v int16) {
	r.Version = v
}

func (r *SyncGroupResponse) GetMemberAssignment() (*ConsumerGroupMemberAssignment, error) {
	assignment := new(ConsumerGroupMemberAssignment)
	err := decode(r.MemberAssignment, assignment, nil)
	return assignment, err
}

func (r *SyncGroupResponse) encode(pe packetEncoder) error {
	if r.Version >= 1 {
		pe.putInt32(r.ThrottleTime)
	}
	pe.putKError(r.Err)
	if err := pe.putBytes(r.MemberAssignment); err != nil {
		return err
	}

	pe.putEmptyTaggedFieldArray()
	return nil
}

func (r *SyncGroupResponse) decode(pd packetDecoder, version int16) (err error) {
	r.Version = version
	if r.Version >= 1 {
		if r.ThrottleTime, err = pd.getInt32(); err != nil {
			return err
		}
	}
	r.Err, err = pd.getKError()
	if err != nil {
		return err
	}

	r.MemberAssignment, err = pd.getBytes()
	if err != nil {
		return err
	}

	_, err = pd.getEmptyTaggedFieldArray()
	return err
}

func (r *SyncGroupResponse) key() int16 {
	return apiKeySyncGroup
}

func (r *SyncGroupResponse) version() int16 {
	return r.Version
}

func (r *SyncGroupResponse) headerVersion() int16 {
	if r.Version >= 4 {
		return 1
	}
	return 0
}

func (r *SyncGroupResponse) isValidVersion() bool {
	return r.Version >= 0 && r.Version <= 4
}

func (r *SyncGroupResponse) isFlexible() bool {
	return r.isFlexibleVersion(r.Version)
}

func (r *SyncGroupResponse) isFlexibleVersion(version int16) bool {
	return version >= 4
}

func (r *SyncGroupResponse) requiredVersion() KafkaVersion {
	switch r.Version {
	case 4:
		return V2_4_0_0
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

func (r *SyncGroupResponse) throttleTime() time.Duration {
	return time.Duration(r.ThrottleTime) * time.Millisecond
}
