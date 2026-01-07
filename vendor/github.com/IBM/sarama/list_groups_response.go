package sarama

type ListGroupsResponse struct {
	Version      int16
	ThrottleTime int32
	Err          KError
	Groups       map[string]string
	GroupsData   map[string]GroupData // version 4 or later
}

func (r *ListGroupsResponse) setVersion(v int16) {
	r.Version = v
}

type GroupData struct {
	GroupState string // version 4 or later
	GroupType  string // version 5 or later
}

func (r *ListGroupsResponse) encode(pe packetEncoder) error {
	if r.Version >= 1 {
		pe.putInt32(r.ThrottleTime)
	}

	pe.putKError(r.Err)

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
		if r.Version >= 4 {
			groupData := r.GroupsData[groupId]
			if err := pe.putString(groupData.GroupState); err != nil {
				return err
			}
		}

		if r.Version >= 5 {
			groupData := r.GroupsData[groupId]
			if err := pe.putString(groupData.GroupType); err != nil {
				return err
			}
		}
		pe.putEmptyTaggedFieldArray()
	}

	pe.putEmptyTaggedFieldArray()
	return nil
}

func (r *ListGroupsResponse) decode(pd packetDecoder, version int16) (err error) {
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

	n, err := pd.getArrayLength()
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
		groupId, err = pd.getString()
		if err != nil {
			return err
		}
		protocolType, err = pd.getString()
		if err != nil {
			return err
		}

		r.Groups[groupId] = protocolType

		if r.Version >= 4 {
			var groupData GroupData
			groupState, err := pd.getString()
			if err != nil {
				return err
			}
			groupData.GroupState = groupState
			if r.Version >= 5 {
				groupType, err := pd.getString()
				if err != nil {
					return err
				}
				groupData.GroupType = groupType
			}
			r.GroupsData[groupId] = groupData
		}

		if _, err = pd.getEmptyTaggedFieldArray(); err != nil {
			return err
		}
	}

	_, err = pd.getEmptyTaggedFieldArray()
	return err
}

func (r *ListGroupsResponse) key() int16 {
	return apiKeyListGroups
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
	return r.Version >= 0 && r.Version <= 5
}

func (r *ListGroupsResponse) isFlexible() bool {
	return r.isFlexibleVersion(r.Version)
}

func (r *ListGroupsResponse) isFlexibleVersion(version int16) bool {
	return version >= 3
}

func (r *ListGroupsResponse) requiredVersion() KafkaVersion {
	switch r.Version {
	case 5:
		return V3_8_0_0
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
