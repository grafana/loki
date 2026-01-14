package sarama

type ListGroupsRequest struct {
	Version      int16
	StatesFilter []string // version 4 or later
	TypesFilter  []string // version 5 or later
}

func (r *ListGroupsRequest) setVersion(v int16) {
	r.Version = v
}

func (r *ListGroupsRequest) encode(pe packetEncoder) error {
	if r.Version >= 4 {
		if err := pe.putArrayLength(len(r.StatesFilter)); err != nil {
			return err
		}
		for _, filter := range r.StatesFilter {
			err := pe.putString(filter)
			if err != nil {
				return err
			}
		}
		if r.Version >= 5 {
			if err := pe.putArrayLength(len(r.TypesFilter)); err != nil {
				return err
			}
			for _, filter := range r.TypesFilter {
				err := pe.putString(filter)
				if err != nil {
					return err
				}
			}
		}
	}
	pe.putEmptyTaggedFieldArray()
	return nil
}

func (r *ListGroupsRequest) decode(pd packetDecoder, version int16) (err error) {
	r.Version = version
	if r.Version >= 4 {
		if r.StatesFilter, err = pd.getStringArray(); err != nil {
			return err
		}
		if r.Version >= 5 {
			if r.TypesFilter, err = pd.getStringArray(); err != nil {
				return err
			}
		}
	}
	_, err = pd.getEmptyTaggedFieldArray()
	return err
}

func (r *ListGroupsRequest) key() int16 {
	return apiKeyListGroups
}

func (r *ListGroupsRequest) version() int16 {
	return r.Version
}

func (r *ListGroupsRequest) headerVersion() int16 {
	if r.Version >= 3 {
		return 2
	}
	return 1
}

func (r *ListGroupsRequest) isValidVersion() bool {
	return r.Version >= 0 && r.Version <= 5
}

func (r *ListGroupsRequest) isFlexible() bool {
	return r.isFlexibleVersion(r.Version)
}

func (r *ListGroupsRequest) isFlexibleVersion(version int16) bool {
	return version >= 3
}

func (r *ListGroupsRequest) requiredVersion() KafkaVersion {
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
