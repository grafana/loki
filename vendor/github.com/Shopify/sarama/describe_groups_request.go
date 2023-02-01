package sarama

type DescribeGroupsRequest struct {
	Version                     int16
	Groups                      []string
	IncludeAuthorizedOperations bool
}

func (r *DescribeGroupsRequest) encode(pe packetEncoder) error {
	if err := pe.putStringArray(r.Groups); err != nil {
		return err
	}
	if r.Version >= 3 {
		pe.putBool(r.IncludeAuthorizedOperations)
	}
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
	return nil
}

func (r *DescribeGroupsRequest) key() int16 {
	return 15
}

func (r *DescribeGroupsRequest) version() int16 {
	return r.Version
}

func (r *DescribeGroupsRequest) headerVersion() int16 {
	return 1
}

func (r *DescribeGroupsRequest) requiredVersion() KafkaVersion {
	switch r.Version {
	case 1, 2, 3, 4:
		return V2_3_0_0
	}
	return V0_9_0_0
}

func (r *DescribeGroupsRequest) AddGroup(group string) {
	r.Groups = append(r.Groups, group)
}
