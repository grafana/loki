package sarama

// DescribeClientQuotas Request (Version: 0) => [components] strict
//   components => entity_type match_type match
//     entity_type => STRING
//     match_type => INT8
//     match => NULLABLE_STRING
//   strict => BOOLEAN

// A filter to be applied to matching client quotas.
// Components: the components to filter on
// Strict: whether the filter only includes specified components
type DescribeClientQuotasRequest struct {
	Version    int16
	Components []QuotaFilterComponent
	Strict     bool
}

// Describe a component for applying a client quota filter.
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

	return nil
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
		if err := pe.putString(""); err != nil {
			return err
		}
	} else {
		if err := pe.putString(d.Match); err != nil {
			return err
		}
	}

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
	return nil
}

func (d *DescribeClientQuotasRequest) key() int16 {
	return 48
}

func (d *DescribeClientQuotasRequest) version() int16 {
	return d.Version
}

func (d *DescribeClientQuotasRequest) headerVersion() int16 {
	return 1
}

func (d *DescribeClientQuotasRequest) isValidVersion() bool {
	return d.Version == 0
}

func (d *DescribeClientQuotasRequest) requiredVersion() KafkaVersion {
	return V2_6_0_0
}
