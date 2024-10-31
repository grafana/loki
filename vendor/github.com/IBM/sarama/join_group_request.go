package sarama

type GroupProtocol struct {
	// Name contains the protocol name.
	Name string
	// Metadata contains the protocol metadata.
	Metadata []byte
}

func (p *GroupProtocol) decode(pd packetDecoder) (err error) {
	p.Name, err = pd.getString()
	if err != nil {
		return err
	}
	p.Metadata, err = pd.getBytes()
	return err
}

func (p *GroupProtocol) encode(pe packetEncoder) (err error) {
	if err := pe.putString(p.Name); err != nil {
		return err
	}
	if err := pe.putBytes(p.Metadata); err != nil {
		return err
	}
	return nil
}

type JoinGroupRequest struct {
	// Version defines the protocol version to use for encode and decode
	Version int16
	// GroupId contains the group identifier.
	GroupId string
	// SessionTimeout specifies that the coordinator should consider the consumer
	// dead if it receives no heartbeat after this timeout in milliseconds.
	SessionTimeout int32
	// RebalanceTimeout contains the maximum time in milliseconds that the
	// coordinator will wait for each member to rejoin when rebalancing the
	// group.
	RebalanceTimeout int32
	// MemberId contains the member id assigned by the group coordinator.
	MemberId string
	// GroupInstanceId contains the unique identifier of the consumer instance
	// provided by end user.
	GroupInstanceId *string
	// ProtocolType contains the unique name the for class of protocols
	// implemented by the group we want to join.
	ProtocolType string
	// GroupProtocols contains the list of protocols that the member supports.
	// deprecated; use OrderedGroupProtocols
	GroupProtocols map[string][]byte
	// OrderedGroupProtocols contains an ordered list of protocols that the member
	// supports.
	OrderedGroupProtocols []*GroupProtocol
}

func (r *JoinGroupRequest) encode(pe packetEncoder) error {
	if err := pe.putString(r.GroupId); err != nil {
		return err
	}
	pe.putInt32(r.SessionTimeout)
	if r.Version >= 1 {
		pe.putInt32(r.RebalanceTimeout)
	}
	if err := pe.putString(r.MemberId); err != nil {
		return err
	}
	if r.Version >= 5 {
		if err := pe.putNullableString(r.GroupInstanceId); err != nil {
			return err
		}
	}
	if err := pe.putString(r.ProtocolType); err != nil {
		return err
	}

	if len(r.GroupProtocols) > 0 {
		if len(r.OrderedGroupProtocols) > 0 {
			return PacketDecodingError{"cannot specify both GroupProtocols and OrderedGroupProtocols on JoinGroupRequest"}
		}

		if err := pe.putArrayLength(len(r.GroupProtocols)); err != nil {
			return err
		}
		for name, metadata := range r.GroupProtocols {
			if err := pe.putString(name); err != nil {
				return err
			}
			if err := pe.putBytes(metadata); err != nil {
				return err
			}
		}
	} else {
		if err := pe.putArrayLength(len(r.OrderedGroupProtocols)); err != nil {
			return err
		}
		for _, protocol := range r.OrderedGroupProtocols {
			if err := protocol.encode(pe); err != nil {
				return err
			}
		}
	}

	return nil
}

func (r *JoinGroupRequest) decode(pd packetDecoder, version int16) (err error) {
	r.Version = version

	if r.GroupId, err = pd.getString(); err != nil {
		return
	}

	if r.SessionTimeout, err = pd.getInt32(); err != nil {
		return
	}

	if version >= 1 {
		if r.RebalanceTimeout, err = pd.getInt32(); err != nil {
			return err
		}
	}

	if r.MemberId, err = pd.getString(); err != nil {
		return
	}

	if version >= 5 {
		if r.GroupInstanceId, err = pd.getNullableString(); err != nil {
			return
		}
	}

	if r.ProtocolType, err = pd.getString(); err != nil {
		return
	}

	n, err := pd.getArrayLength()
	if err != nil {
		return err
	}
	if n == 0 {
		return nil
	}

	r.GroupProtocols = make(map[string][]byte)
	for i := 0; i < n; i++ {
		protocol := &GroupProtocol{}
		if err := protocol.decode(pd); err != nil {
			return err
		}
		r.GroupProtocols[protocol.Name] = protocol.Metadata
		r.OrderedGroupProtocols = append(r.OrderedGroupProtocols, protocol)
	}

	return nil
}

func (r *JoinGroupRequest) key() int16 {
	return 11
}

func (r *JoinGroupRequest) version() int16 {
	return r.Version
}

func (r *JoinGroupRequest) headerVersion() int16 {
	return 1
}

func (r *JoinGroupRequest) isValidVersion() bool {
	return r.Version >= 0 && r.Version <= 5
}

func (r *JoinGroupRequest) requiredVersion() KafkaVersion {
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

func (r *JoinGroupRequest) AddGroupProtocol(name string, metadata []byte) {
	r.OrderedGroupProtocols = append(r.OrderedGroupProtocols, &GroupProtocol{
		Name:     name,
		Metadata: metadata,
	})
}

func (r *JoinGroupRequest) AddGroupProtocolMetadata(name string, metadata *ConsumerGroupMemberMetadata) error {
	bin, err := encode(metadata, nil)
	if err != nil {
		return err
	}

	r.AddGroupProtocol(name, bin)
	return nil
}
