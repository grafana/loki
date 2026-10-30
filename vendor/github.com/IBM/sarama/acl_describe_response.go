package sarama

import "time"

// DescribeAclsResponse is a describe acl response type
type DescribeAclsResponse struct {
	Version      int16
	ThrottleTime time.Duration
	Err          KError
	ErrMsg       *string
	ResourceAcls []*ResourceAcls
}

func (d *DescribeAclsResponse) setVersion(v int16) {
	d.Version = v
}

func (d *DescribeAclsResponse) encode(pe packetEncoder) error {
	pe.putDurationMs(d.ThrottleTime)
	pe.putKError(d.Err)

	if err := pe.putNullableString(d.ErrMsg); err != nil {
		return err
	}

	if err := pe.putArrayLength(len(d.ResourceAcls)); err != nil {
		return err
	}

	for _, resourceAcl := range d.ResourceAcls {
		if err := resourceAcl.encode(pe, d.Version); err != nil {
			return err
		}
	}

	pe.putEmptyTaggedFieldArray()
	return nil
}

func (d *DescribeAclsResponse) decode(pd packetDecoder, version int16) (err error) {
	d.Version = version
	if d.ThrottleTime, err = pd.getDurationMs(); err != nil {
		return err
	}

	d.Err, err = pd.getKError()
	if err != nil {
		return err
	}

	if d.ErrMsg, err = pd.getNullableString(); err != nil {
		return err
	}

	n, err := pd.getArrayLength()
	if err != nil {
		return err
	}
	if n < 0 {
		return errInvalidArrayLength
	}
	d.ResourceAcls = make([]*ResourceAcls, n)

	for i := range n {
		d.ResourceAcls[i] = new(ResourceAcls)
		if err := d.ResourceAcls[i].decode(pd, version); err != nil {
			return err
		}
	}

	_, err = pd.getEmptyTaggedFieldArray()
	return err
}

func (d *DescribeAclsResponse) key() int16 {
	return apiKeyDescribeAcls
}

func (d *DescribeAclsResponse) version() int16 {
	return d.Version
}

func (d *DescribeAclsResponse) headerVersion() int16 {
	if d.Version >= 2 {
		return 1
	}
	return 0
}

func (d *DescribeAclsResponse) isValidVersion() bool {
	return d.Version >= 0 && d.Version <= 2
}

func (d *DescribeAclsResponse) isFlexible() bool {
	return d.isFlexibleVersion(d.Version)
}

func (d *DescribeAclsResponse) isFlexibleVersion(version int16) bool {
	return version >= 2
}

func (d *DescribeAclsResponse) requiredVersion() KafkaVersion {
	switch d.Version {
	case 2:
		return V2_5_0_0
	case 1:
		return V2_0_0_0
	default:
		return V0_11_0_0
	}
}

func (r *DescribeAclsResponse) throttleTime() time.Duration {
	return r.ThrottleTime
}
