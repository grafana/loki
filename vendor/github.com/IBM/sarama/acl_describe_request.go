package sarama

// DescribeAclsRequest is a describe acl request type
type DescribeAclsRequest struct {
	Version int
	AclFilter
}

func (d *DescribeAclsRequest) setVersion(v int16) {
	d.Version = int(v)
}

func (d *DescribeAclsRequest) encode(pe packetEncoder) error {
	d.AclFilter.Version = d.Version
	return d.AclFilter.encode(pe)
}

func (d *DescribeAclsRequest) decode(pd packetDecoder, version int16) (err error) {
	d.Version = int(version)
	d.AclFilter.Version = int(version)
	return d.AclFilter.decode(pd, version)
}

func (d *DescribeAclsRequest) key() int16 {
	return apiKeyDescribeAcls
}

func (d *DescribeAclsRequest) version() int16 {
	return int16(d.Version)
}

func (d *DescribeAclsRequest) headerVersion() int16 {
	if d.Version >= 2 {
		return 2
	}
	return 1
}

func (d *DescribeAclsRequest) isValidVersion() bool {
	return d.Version >= 0 && d.Version <= 2
}

func (d *DescribeAclsRequest) isFlexible() bool {
	return d.isFlexibleVersion(int16(d.Version))
}

func (d *DescribeAclsRequest) isFlexibleVersion(version int16) bool {
	return version >= 2
}

func (d *DescribeAclsRequest) requiredVersion() KafkaVersion {
	switch d.Version {
	case 2:
		return V2_5_0_0
	case 1:
		return V2_0_0_0
	default:
		return V0_11_0_0
	}
}
