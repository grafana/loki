package sarama

type ListGroupsResponse struct {
	Version      int16
	ThrottleTime int32
	Err          KError
	Groups       map[string]string
	GroupsData   map[string]GroupData // version 4 or later
}

type GroupData struct {
	GroupState string // version 4 or later
}

func (r *ListGroupsResponse) encode(pe packetEncoder) error {
	if r.Version >= 1 {
		pe.putInt32(r.ThrottleTime)
	}

	pe.putInt16(int16(r.Err))

	if r.Version <= 2 {
		if err := pe.putArrayLength(len(r.Groups)); err != nil {
			return err
		}
		for groupId, protocolType := range r.Groups {
			if err := pe.putString(groupId); err != nil {
				return err
			}
			if err := pe.putString(protocolType); err != nil {
				return err
			}
		}
	} else {
		pe.putCompactArrayLength(len(r.Groups))
		for groupId, protocolType := range r.Groups {
			if err := pe.putCompactString(groupId); err != nil {
				return err
			}
			if err := pe.putCompactString(protocolType); err != nil {
				return err
			}

			if r.Version >= 4 {
				groupData := r.GroupsData[groupId]
				if err := pe.putCompactString(groupData.GroupState); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func (r *ListGroupsResponse) decode(pd packetDecoder, version int16) error {
	r.Version = version
	if r.Version >= 1 {
		var err error
		if r.ThrottleTime, err = pd.getInt32(); err != nil {
			return err
		}
	}

	kerr, err := pd.getInt16()
	if err != nil {
		return err
	}

	r.Err = KError(kerr)

	var n int
	if r.Version <= 2 {
		n, err = pd.getArrayLength()
	} else {
		n, err = pd.getCompactArrayLength()
	}
	if err != nil {
		return err
	}

	for i := 0; i < n; i++ {
		if i == 0 {
			r.Groups = make(map[string]string)
			if r.Version >= 4 {
				r.GroupsData = make(map[string]GroupData)
			}
		}

		var groupId, protocolType string
		if r.Version <= 2 {
			groupId, err = pd.getString()
			if err != nil {
				return err
			}
			protocolType, err = pd.getString()
			if err != nil {
				return err
			}
		} else {
			groupId, err = pd.getCompactString()
			if err != nil {
				return err
			}
			protocolType, err = pd.getCompactString()
			if err != nil {
				return err
			}
		}

		r.Groups[groupId] = protocolType

		if r.Version >= 4 {
			groupState, err := pd.getCompactString()
			if err != nil {
				return err
			}
			r.GroupsData[groupId] = GroupData{
				GroupState: groupState,
			}
		}

		if r.Version >= 3 {
			if _, err = pd.getEmptyTaggedFieldArray(); err != nil {
				return err
			}
		}
	}

	if r.Version >= 3 {
		if _, err = pd.getEmptyTaggedFieldArray(); err != nil {
			return err
		}
	}

	return nil
}

func (r *ListGroupsResponse) key() int16 {
	return 16
}

func (r *ListGroupsResponse) version() int16 {
	return r.Version
}

func (r *ListGroupsResponse) headerVersion() int16 {
	if r.Version >= 3 {
		return 1
	}
	return 0
}

func (r *ListGroupsResponse) isValidVersion() bool {
	return r.Version >= 0 && r.Version <= 4
}

func (r *ListGroupsResponse) requiredVersion() KafkaVersion {
	switch r.Version {
	case 4:
		return V2_6_0_0
	case 3:
		return V2_4_0_0
	case 2:
		return V2_0_0_0
	case 1:
		return V0_11_0_0
	case 0:
		return V0_9_0_0
	default:
		return V2_6_0_0
	}
}
