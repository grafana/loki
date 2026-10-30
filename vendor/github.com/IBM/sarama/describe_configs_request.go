package sarama

type DescribeConfigsRequest struct {
	Version              int16
	Resources            []*ConfigResource
	IncludeSynonyms      bool
	IncludeDocumentation bool // v3
}

func (r *DescribeConfigsRequest) setVersion(v int16) {
	r.Version = v
}

type ConfigResource struct {
	Type        ConfigResourceType
	Name        string
	ConfigNames []string
}

func (r *DescribeConfigsRequest) encode(pe packetEncoder) error {
	if err := pe.putArrayLength(len(r.Resources)); err != nil {
		return err
	}

	for _, c := range r.Resources {
		pe.putInt8(int8(c.Type))
		if err := pe.putString(c.Name); err != nil {
			return err
		}

		if len(c.ConfigNames) == 0 {
			if err := pe.putArrayLength(-1); err != nil {
				return err
			}
		} else if err := pe.putStringArray(c.ConfigNames); err != nil {
			return err
		}

		pe.putEmptyTaggedFieldArray()
	}

	if r.Version >= 1 {
		pe.putBool(r.IncludeSynonyms)
	}

	if r.Version >= 3 {
		pe.putBool(r.IncludeDocumentation)
	}

	pe.putEmptyTaggedFieldArray()

	return nil
}

func (r *DescribeConfigsRequest) decode(pd packetDecoder, version int16) (err error) {
	n, err := pd.getArrayLength()
	if err != nil {
		return err
	}
	if n < 0 {
		return errInvalidArrayLength
	}

	r.Resources = make([]*ConfigResource, n)

	for i := range n {
		r.Resources[i] = &ConfigResource{}
		t, err := pd.getInt8()
		if err != nil {
			return err
		}
		r.Resources[i].Type = ConfigResourceType(t)
		name, err := pd.getString()
		if err != nil {
			return err
		}
		r.Resources[i].Name = name

		confLength, err := pd.getArrayLength()
		if err != nil {
			return err
		}

		if confLength >= 0 {
			cfnames := make([]string, confLength)
			for i := range confLength {
				s, err := pd.getString()
				if err != nil {
					return err
				}
				cfnames[i] = s
			}
			r.Resources[i].ConfigNames = cfnames
		}

		if _, err := pd.getEmptyTaggedFieldArray(); err != nil {
			return err
		}
	}
	r.Version = version
	if r.Version >= 1 {
		b, err := pd.getBool()
		if err != nil {
			return err
		}
		r.IncludeSynonyms = b
	}

	if r.Version >= 3 {
		b, err := pd.getBool()
		if err != nil {
			return err
		}
		r.IncludeDocumentation = b
	}

	if _, err := pd.getEmptyTaggedFieldArray(); err != nil {
		return err
	}

	return nil
}

func (r *DescribeConfigsRequest) key() int16 {
	return apiKeyDescribeConfigs
}

func (r *DescribeConfigsRequest) version() int16 {
	return r.Version
}

func (r *DescribeConfigsRequest) headerVersion() int16 {
	if r.Version >= 4 {
		return 2
	}
	return 1
}

func (r *DescribeConfigsRequest) isValidVersion() bool {
	return r.Version >= 0 && r.Version <= 4
}

func (r *DescribeConfigsRequest) isFlexible() bool {
	return r.isFlexibleVersion(r.Version)
}

func (r *DescribeConfigsRequest) isFlexibleVersion(version int16) bool {
	return version >= 4
}

func (r *DescribeConfigsRequest) requiredVersion() KafkaVersion {
	switch r.Version {
	case 4:
		return V2_8_0_0
	case 3:
		return V2_6_0_0
	case 2:
		return V2_0_0_0
	case 1:
		return V1_1_0_0
	case 0:
		return V0_11_0_0
	default:
		return V2_0_0_0
	}
}
