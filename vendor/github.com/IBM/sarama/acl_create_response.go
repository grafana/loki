package sarama

import "time"

// CreateAclsResponse is a an acl response creation type
type CreateAclsResponse struct {
	Version              int16
	ThrottleTime         time.Duration
	AclCreationResponses []*AclCreationResponse
}

func (c *CreateAclsResponse) setVersion(v int16) {
	c.Version = v
}

func (c *CreateAclsResponse) encode(pe packetEncoder) error {
	pe.putDurationMs(c.ThrottleTime)

	if err := pe.putArrayLength(len(c.AclCreationResponses)); err != nil {
		return err
	}

	for _, aclCreationResponse := range c.AclCreationResponses {
		if err := aclCreationResponse.encode(pe); err != nil {
			return err
		}
	}

	pe.putEmptyTaggedFieldArray()
	return nil
}

func (c *CreateAclsResponse) decode(pd packetDecoder, version int16) (err error) {
	c.Version = version
	c.ThrottleTime, err = pd.getDurationMs()
	if err != nil {
		return err
	}

	n, err := pd.getArrayLength()
	if err != nil {
		return err
	}
	if n < 0 {
		return errInvalidArrayLength
	}

	c.AclCreationResponses = make([]*AclCreationResponse, n)
	for i := range n {
		c.AclCreationResponses[i] = new(AclCreationResponse)
		if err := c.AclCreationResponses[i].decode(pd, version); err != nil {
			return err
		}
	}

	_, err = pd.getEmptyTaggedFieldArray()
	return err
}

func (c *CreateAclsResponse) key() int16 {
	return apiKeyCreateAcls
}

func (c *CreateAclsResponse) version() int16 {
	return c.Version
}

func (c *CreateAclsResponse) headerVersion() int16 {
	if c.Version >= 2 {
		return 1
	}
	return 0
}

func (c *CreateAclsResponse) isValidVersion() bool {
	return c.Version >= 0 && c.Version <= 2
}

func (c *CreateAclsResponse) isFlexible() bool {
	return c.isFlexibleVersion(c.Version)
}

func (c *CreateAclsResponse) isFlexibleVersion(version int16) bool {
	return version >= 2
}

func (c *CreateAclsResponse) requiredVersion() KafkaVersion {
	switch c.Version {
	case 2:
		return V2_5_0_0
	case 1:
		return V2_0_0_0
	default:
		return V0_11_0_0
	}
}

func (r *CreateAclsResponse) throttleTime() time.Duration {
	return r.ThrottleTime
}

// AclCreationResponse is an acl creation response type
type AclCreationResponse struct {
	Err    KError
	ErrMsg *string
}

func (a *AclCreationResponse) encode(pe packetEncoder) error {
	pe.putKError(a.Err)

	if err := pe.putNullableString(a.ErrMsg); err != nil {
		return err
	}

	pe.putEmptyTaggedFieldArray()
	return nil
}

func (a *AclCreationResponse) decode(pd packetDecoder, version int16) (err error) {
	a.Err, err = pd.getKError()
	if err != nil {
		return err
	}

	if a.ErrMsg, err = pd.getNullableString(); err != nil {
		return err
	}

	_, err = pd.getEmptyTaggedFieldArray()
	return err
}
