package sarama

// DescribeClientQuotas Request (Version: 0) => [components] strict
//   components => entity_type match_type match
//     entity_type => STRING
//     match_type => INT8
//     match => NULLABLE_STRING
//   strict => BOOLEAN
// DescribeClientQuotas Request (Version: 1) => [components] strict _tagged_fields
//   components => entity_type match_type match _tagged_fields
//     entity_type => COMPACT_STRING
//     match_type => INT8
//     match => COMPACT_NULLABLE_STRING
//   strict => BOOLEAN

// DescribeClientQuotasRequest contains a filter to be applied to matching
// client quotas.
// Components: the components to filter on
// Strict: whether the filter only includes specified components
type DescribeClientQuotasRequest struct {
	Version    int16
	Components []QuotaFilterComponent
	Strict     bool
}

func NewDescribeClientQuotasRequest(version KafkaVersion, components []QuotaFilterComponent, strict bool) *DescribeClientQuotasRequest {
	d := &DescribeClientQuotasRequest{
		Components: components,
		Strict:     strict,
	}
	if version.IsAtLeast(V2_8_0_0) {
		d.Version = 1
	}
	return d
}

func (d *DescribeClientQuotasRequest) setVersion(v int16) {
	d.Version = v
}

// QuotaFilterComponent describes a component for applying a client quota filter.
// EntityType: the entity type the filter component applies to ("user", "client-id", "ip")
// MatchType: the match type of the filter component (any, exact, default)
// Match: the name that's matched exactly (used when MatchType is QuotaMatchExact)
type QuotaFilterComponent struct {
	EntityType QuotaEntityType
	MatchType  QuotaMatchType
	Match      string
}

func (d *DescribeClientQuotasRequest) encode(pe packetEncoder) error {
	// Components
	if err := pe.putArrayLength(len(d.Components)); err != nil {
		return err
	}
	for _, c := range d.Components {
		if err := c.encode(pe); err != nil {
			return err
		}
	}

	// Strict
	pe.putBool(d.Strict)

	pe.putEmptyTaggedFieldArray()

	return nil
}

func (d *DescribeClientQuotasRequest) decode(pd packetDecoder, version int16) error {
	// Components
	componentCount, err := pd.getArrayLength()
	if err != nil {
		return err
	}
	if componentCount > 0 {
		d.Components = make([]QuotaFilterComponent, componentCount)
		for i := range d.Components {
			c := QuotaFilterComponent{}
			if err = c.decode(pd, version); err != nil {
				return err
			}
			d.Components[i] = c
		}
	} else {
		d.Components = []QuotaFilterComponent{}
	}

	// Strict
	strict, err := pd.getBool()
	if err != nil {
		return err
	}
	d.Strict = strict

	_, err = pd.getEmptyTaggedFieldArray()
	return err
}

func (d *QuotaFilterComponent) encode(pe packetEncoder) error {
	// EntityType
	if err := pe.putString(string(d.EntityType)); err != nil {
		return err
	}

	// MatchType
	pe.putInt8(int8(d.MatchType))

	// Match
	if d.MatchType == QuotaMatchAny {
		if err := pe.putNullableString(nil); err != nil {
			return err
		}
	} else if d.MatchType == QuotaMatchDefault {
		if err := pe.putNullableString(nil); err != nil {
			return err
		}
	} else {
		if err := pe.putString(d.Match); err != nil {
			return err
		}
	}

	pe.putEmptyTaggedFieldArray()

	return nil
}

func (d *QuotaFilterComponent) decode(pd packetDecoder, version int16) error {
	// EntityType
	entityType, err := pd.getString()
	if err != nil {
		return err
	}
	d.EntityType = QuotaEntityType(entityType)

	// MatchType
	matchType, err := pd.getInt8()
	if err != nil {
		return err
	}
	d.MatchType = QuotaMatchType(matchType)

	// Match
	match, err := pd.getNullableString()
	if err != nil {
		return err
	}
	if match != nil {
		d.Match = *match
	}

	_, err = pd.getEmptyTaggedFieldArray()
	return err
}

func (d *DescribeClientQuotasRequest) key() int16 {
	return apiKeyDescribeClientQuotas
}

func (d *DescribeClientQuotasRequest) version() int16 {
	return d.Version
}

func (d *DescribeClientQuotasRequest) headerVersion() int16 {
	if d.Version >= 1 {
		return 2
	}

	return 1
}

func (d *DescribeClientQuotasRequest) isValidVersion() bool {
	return d.Version >= 0 && d.Version <= 1
}

func (d *DescribeClientQuotasRequest) isFlexible() bool {
	return d.isFlexibleVersion(d.Version)
}

func (d *DescribeClientQuotasRequest) isFlexibleVersion(version int16) bool {
	return version >= 1
}

func (d *DescribeClientQuotasRequest) requiredVersion() KafkaVersion {
	switch d.Version {
	case 1:
		return V2_8_0_0
	case 0:
		return V2_6_0_0
	default:
		return V2_8_0_0
	}
}
