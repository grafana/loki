package sarama

type DescribeGroupsRequest struct {
	Version                     int16
	Groups                      []string
	IncludeAuthorizedOperations bool
}

func (r *DescribeGroupsRequest) setVersion(v int16) {
	r.Version = v
}

func (r *DescribeGroupsRequest) encode(pe packetEncoder) error {
	if err := pe.putStringArray(r.Groups); err != nil {
		return err
	}
	if r.Version >= 3 {
		pe.putBool(r.IncludeAuthorizedOperations)
	}
	pe.putEmptyTaggedFieldArray()
	return nil
}

func (r *DescribeGroupsRequest) decode(pd packetDecoder, version int16) (err error) {
	r.Version = version
	r.Groups, err = pd.getStringArray()
	if err != nil {
		return err
	}
	if r.Version >= 3 {
		if r.IncludeAuthorizedOperations, err = pd.getBool(); err != nil {
			return err
		}
	}
	_, err = pd.getEmptyTaggedFieldArray()
	return err
}

func (r *DescribeGroupsRequest) key() int16 {
	return apiKeyDescribeGroups
}

func (r *DescribeGroupsRequest) version() int16 {
	return r.Version
}

func (r *DescribeGroupsRequest) headerVersion() int16 {
	if r.Version >= 5 {
		return 2
	}
	return 1
}

func (r *DescribeGroupsRequest) isValidVersion() bool {
	return r.Version >= 0 && r.Version <= 5
}

func (r *DescribeGroupsRequest) isFlexible() bool {
	return r.isFlexibleVersion(r.Version)
}

func (r *DescribeGroupsRequest) isFlexibleVersion(version int16) bool {
	return version >= 5
}

func (r *DescribeGroupsRequest) requiredVersion() KafkaVersion {
	switch r.Version {
	case 5:
		return V2_4_0_0
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
		return V2_4_0_0
	}
}

func (r *DescribeGroupsRequest) AddGroup(group string) {
	r.Groups = append(r.Groups, group)
}
