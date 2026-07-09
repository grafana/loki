package sarama

import "time"

// DeleteAclsResponse is a delete acl response
type DeleteAclsResponse struct {
	Version         int16
	ThrottleTime    time.Duration
	FilterResponses []*FilterResponse
}

func (d *DeleteAclsResponse) setVersion(v int16) {
	d.Version = v
}

func (d *DeleteAclsResponse) encode(pe packetEncoder) error {
	pe.putDurationMs(d.ThrottleTime)

	if err := pe.putArrayLength(len(d.FilterResponses)); err != nil {
		return err
	}

	for _, filterResponse := range d.FilterResponses {
		if err := filterResponse.encode(pe, d.Version); err != nil {
			return err
		}
	}

	pe.putEmptyTaggedFieldArray()
	return nil
}

func (d *DeleteAclsResponse) decode(pd packetDecoder, version int16) (err error) {
	d.Version = version
	if d.ThrottleTime, err = pd.getDurationMs(); err != nil {
		return err
	}

	n, err := pd.getArrayLength()
	if err != nil {
		return err
	}
	if n < 0 {
		return errInvalidArrayLength
	}

	d.FilterResponses = make([]*FilterResponse, n)
	for i := range n {
		d.FilterResponses[i] = new(FilterResponse)
		if err := d.FilterResponses[i].decode(pd, version); err != nil {
			return err
		}
	}

	_, err = pd.getEmptyTaggedFieldArray()
	return err
}

func (d *DeleteAclsResponse) key() int16 {
	return apiKeyDeleteAcls
}

func (d *DeleteAclsResponse) version() int16 {
	return d.Version
}

func (d *DeleteAclsResponse) headerVersion() int16 {
	if d.Version >= 2 {
		return 1
	}
	return 0
}

func (d *DeleteAclsResponse) isValidVersion() bool {
	return d.Version >= 0 && d.Version <= 2
}

func (d *DeleteAclsResponse) isFlexible() bool {
	return d.isFlexibleVersion(d.Version)
}

func (d *DeleteAclsResponse) isFlexibleVersion(version int16) bool {
	return version >= 2
}

func (d *DeleteAclsResponse) requiredVersion() KafkaVersion {
	switch d.Version {
	case 2:
		return V2_5_0_0
	case 1:
		return V2_0_0_0
	default:
		return V0_11_0_0
	}
}

func (r *DeleteAclsResponse) throttleTime() time.Duration {
	return r.ThrottleTime
}

// FilterResponse is a filter response type
type FilterResponse struct {
	Err          KError
	ErrMsg       *string
	MatchingAcls []*MatchingAcl
}

func (f *FilterResponse) encode(pe packetEncoder, version int16) error {
	pe.putKError(f.Err)
	if err := pe.putNullableString(f.ErrMsg); err != nil {
		return err
	}

	if err := pe.putArrayLength(len(f.MatchingAcls)); err != nil {
		return err
	}
	for _, matchingAcl := range f.MatchingAcls {
		if err := matchingAcl.encode(pe, version); err != nil {
			return err
		}
	}

	pe.putEmptyTaggedFieldArray()
	return nil
}

func (f *FilterResponse) decode(pd packetDecoder, version int16) (err error) {
	f.Err, err = pd.getKError()
	if err != nil {
		return err
	}

	if f.ErrMsg, err = pd.getNullableString(); err != nil {
		return err
	}

	n, err := pd.getArrayLength()
	if err != nil {
		return err
	}
	if n < 0 {
		return errInvalidArrayLength
	}
	f.MatchingAcls = make([]*MatchingAcl, n)
	for i := range n {
		f.MatchingAcls[i] = new(MatchingAcl)
		if err := f.MatchingAcls[i].decode(pd, version); err != nil {
			return err
		}
	}

	_, err = pd.getEmptyTaggedFieldArray()
	return err
}

// MatchingAcl is a matching acl type
type MatchingAcl struct {
	Err    KError
	ErrMsg *string
	Resource
	Acl
}

func (m *MatchingAcl) encode(pe packetEncoder, version int16) error {
	pe.putKError(m.Err)
	if err := pe.putNullableString(m.ErrMsg); err != nil {
		return err
	}

	if err := m.Resource.encode(pe, version); err != nil {
		return err
	}

	if err := m.Acl.encode(pe); err != nil {
		return err
	}

	// empty tagged fields encoded in Acl
	return nil
}

func (m *MatchingAcl) decode(pd packetDecoder, version int16) (err error) {
	m.Err, err = pd.getKError()
	if err != nil {
		return err
	}

	if m.ErrMsg, err = pd.getNullableString(); err != nil {
		return err
	}

	if err := m.Resource.decode(pd, version); err != nil {
		return err
	}

	if err := m.Acl.decode(pd, version); err != nil {
		return err
	}

	// empty tagged fields decoded in Acl
	return nil
}
