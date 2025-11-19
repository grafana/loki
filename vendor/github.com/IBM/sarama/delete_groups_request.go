package sarama

type DeleteGroupsRequest struct {
	Version int16
	Groups  []string
}

func (r *DeleteGroupsRequest) setVersion(v int16) {
	r.Version = v
}

func (r *DeleteGroupsRequest) encode(pe packetEncoder) error {
	if err := pe.putStringArray(r.Groups); err != nil {
		return err
	}
	pe.putEmptyTaggedFieldArray()
	return nil
}

func (r *DeleteGroupsRequest) decode(pd packetDecoder, version int16) (err error) {
	r.Groups, err = pd.getStringArray()
	if err != nil {
		return err
	}
	_, err = pd.getEmptyTaggedFieldArray()
	return
}

func (r *DeleteGroupsRequest) key() int16 {
	return apiKeyDeleteGroups
}

func (r *DeleteGroupsRequest) version() int16 {
	return r.Version
}

func (r *DeleteGroupsRequest) headerVersion() int16 {
	if r.Version >= 2 {
		return 2
	}
	return 1
}

func (r *DeleteGroupsRequest) isFlexible() bool {
	return r.isFlexibleVersion(r.Version)
}

func (r *DeleteGroupsRequest) isFlexibleVersion(version int16) bool {
	return version >= 2
}

func (r *DeleteGroupsRequest) isValidVersion() bool {
	return r.Version >= 0 && r.Version <= 2
}

func (r *DeleteGroupsRequest) requiredVersion() KafkaVersion {
	switch r.Version {
	case 2:
		return V2_4_0_0
	case 1:
		return V2_0_0_0
	case 0:
		return V1_1_0_0
	default:
		return V2_0_0_0
	}
}

func (r *DeleteGroupsRequest) AddGroup(group string) {
	r.Groups = append(r.Groups, group)
}
