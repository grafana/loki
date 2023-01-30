package sarama

type DescribeGroupsResponse struct {
	// Version defines the protocol version to use for encode and decode
	Version int16
	// ThrottleTimeMs contains the duration in milliseconds for which the
	// request was throttled due to a quota violation, or zero if the request
	// did not violate any quota.
	ThrottleTimeMs int32
	// Groups contains each described group.
	Groups []*GroupDescription
}

func (r *DescribeGroupsResponse) encode(pe packetEncoder) (err error) {
	if r.Version >= 1 {
		pe.putInt32(r.ThrottleTimeMs)
	}
	if err := pe.putArrayLength(len(r.Groups)); err != nil {
		return err
	}

	for _, block := range r.Groups {
		if err := block.encode(pe, r.Version); err != nil {
			return err
		}
	}

	return nil
}

func (r *DescribeGroupsResponse) decode(pd packetDecoder, version int16) (err error) {
	r.Version = version
	if r.Version >= 1 {
		if r.ThrottleTimeMs, err = pd.getInt32(); err != nil {
			return err
		}
	}
	if numGroups, err := pd.getArrayLength(); err != nil {
		return err
	} else if numGroups > 0 {
		r.Groups = make([]*GroupDescription, numGroups)
		for i := 0; i < numGroups; i++ {
			block := &GroupDescription{}
			if err := block.decode(pd, r.Version); err != nil {
				return err
			}
			r.Groups[i] = block
		}
	}

	return nil
}

func (r *DescribeGroupsResponse) key() int16 {
	return 15
}

func (r *DescribeGroupsResponse) version() int16 {
	return r.Version
}

func (r *DescribeGroupsResponse) headerVersion() int16 {
	return 0
}

func (r *DescribeGroupsResponse) requiredVersion() KafkaVersion {
	switch r.Version {
	case 1, 2, 3, 4:
		return V2_3_0_0
	}
	return V0_9_0_0
}

// GroupDescription contains each described group.
type GroupDescription struct {
	// Version defines the protocol version to use for encode and decode
	Version int16
	// Err contains the describe error as the KError type.
	Err KError
	// ErrorCode contains the describe error, or 0 if there was no error.
	ErrorCode int16
	// GroupId contains the group ID string.
	GroupId string
	// State contains the group state string, or the empty string.
	State string
	// ProtocolType contains the group protocol type, or the empty string.
	ProtocolType string
	// Protocol contains the group protocol data, or the empty string.
	Protocol string
	// Members contains the group members.
	Members map[string]*GroupMemberDescription
	// AuthorizedOperations contains a 32-bit bitfield to represent authorized
	// operations for this group.
	AuthorizedOperations int32
}

func (gd *GroupDescription) encode(pe packetEncoder, version int16) (err error) {
	gd.Version = version
	pe.putInt16(gd.ErrorCode)

	if err := pe.putString(gd.GroupId); err != nil {
		return err
	}
	if err := pe.putString(gd.State); err != nil {
		return err
	}
	if err := pe.putString(gd.ProtocolType); err != nil {
		return err
	}
	if err := pe.putString(gd.Protocol); err != nil {
		return err
	}

	if err := pe.putArrayLength(len(gd.Members)); err != nil {
		return err
	}

	for _, block := range gd.Members {
		if err := block.encode(pe, gd.Version); err != nil {
			return err
		}
	}

	if gd.Version >= 3 {
		pe.putInt32(gd.AuthorizedOperations)
	}

	return nil
}

func (gd *GroupDescription) decode(pd packetDecoder, version int16) (err error) {
	gd.Version = version
	if gd.ErrorCode, err = pd.getInt16(); err != nil {
		return err
	}

	gd.Err = KError(gd.ErrorCode)

	if gd.GroupId, err = pd.getString(); err != nil {
		return err
	}
	if gd.State, err = pd.getString(); err != nil {
		return err
	}
	if gd.ProtocolType, err = pd.getString(); err != nil {
		return err
	}
	if gd.Protocol, err = pd.getString(); err != nil {
		return err
	}

	if numMembers, err := pd.getArrayLength(); err != nil {
		return err
	} else if numMembers > 0 {
		gd.Members = make(map[string]*GroupMemberDescription, numMembers)
		for i := 0; i < numMembers; i++ {
			block := &GroupMemberDescription{}
			if err := block.decode(pd, gd.Version); err != nil {
				return err
			}
			gd.Members[block.MemberId] = block
		}
	}

	if gd.Version >= 3 {
		if gd.AuthorizedOperations, err = pd.getInt32(); err != nil {
			return err
		}
	}

	return nil
}

// GroupMemberDescription contains the group members.
type GroupMemberDescription struct {
	// Version defines the protocol version to use for encode and decode
	Version int16
	// MemberId contains the member ID assigned by the group coordinator.
	MemberId string
	// GroupInstanceId contains the unique identifier of the consumer instance
	// provided by end user.
	GroupInstanceId *string
	// ClientId contains the client ID used in the member's latest join group
	// request.
	ClientId string
	// ClientHost contains the client host.
	ClientHost string
	// MemberMetadata contains the metadata corresponding to the current group
	// protocol in use.
	MemberMetadata []byte
	// MemberAssignment contains the current assignment provided by the group
	// leader.
	MemberAssignment []byte
}

func (gmd *GroupMemberDescription) encode(pe packetEncoder, version int16) (err error) {
	gmd.Version = version
	if err := pe.putString(gmd.MemberId); err != nil {
		return err
	}
	if gmd.Version >= 4 {
		if err := pe.putNullableString(gmd.GroupInstanceId); err != nil {
			return err
		}
	}
	if err := pe.putString(gmd.ClientId); err != nil {
		return err
	}
	if err := pe.putString(gmd.ClientHost); err != nil {
		return err
	}
	if err := pe.putBytes(gmd.MemberMetadata); err != nil {
		return err
	}
	if err := pe.putBytes(gmd.MemberAssignment); err != nil {
		return err
	}

	return nil
}

func (gmd *GroupMemberDescription) decode(pd packetDecoder, version int16) (err error) {
	gmd.Version = version
	if gmd.MemberId, err = pd.getString(); err != nil {
		return err
	}
	if gmd.Version >= 4 {
		if gmd.GroupInstanceId, err = pd.getNullableString(); err != nil {
			return err
		}
	}
	if gmd.ClientId, err = pd.getString(); err != nil {
		return err
	}
	if gmd.ClientHost, err = pd.getString(); err != nil {
		return err
	}
	if gmd.MemberMetadata, err = pd.getBytes(); err != nil {
		return err
	}
	if gmd.MemberAssignment, err = pd.getBytes(); err != nil {
		return err
	}

	return nil
}

func (gmd *GroupMemberDescription) GetMemberAssignment() (*ConsumerGroupMemberAssignment, error) {
	if len(gmd.MemberAssignment) == 0 {
		return nil, nil
	}
	assignment := new(ConsumerGroupMemberAssignment)
	err := decode(gmd.MemberAssignment, assignment, nil)
	return assignment, err
}

func (gmd *GroupMemberDescription) GetMemberMetadata() (*ConsumerGroupMemberMetadata, error) {
	if len(gmd.MemberMetadata) == 0 {
		return nil, nil
	}
	metadata := new(ConsumerGroupMemberMetadata)
	err := decode(gmd.MemberMetadata, metadata, nil)
	return metadata, err
}
