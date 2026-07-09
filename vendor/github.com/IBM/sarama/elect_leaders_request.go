package sarama

type ElectLeadersRequest struct {
	Version         int16
	Type            ElectionType
	TopicPartitions map[string][]int32
	TimeoutMs       int32
}

func (r *ElectLeadersRequest) setVersion(v int16) {
	r.Version = v
}

func (r *ElectLeadersRequest) encode(pe packetEncoder) error {
	if r.Version > 0 {
		pe.putInt8(int8(r.Type))
	}

	if err := pe.putArrayLength(len(r.TopicPartitions)); err != nil {
		return err
	}

	for topic, partitions := range r.TopicPartitions {
		if err := pe.putString(topic); err != nil {
			return err
		}

		if err := pe.putInt32Array(partitions); err != nil {
			return err
		}

		pe.putEmptyTaggedFieldArray()
	}

	pe.putInt32(r.TimeoutMs)

	pe.putEmptyTaggedFieldArray()
	return nil
}

func (r *ElectLeadersRequest) decode(pd packetDecoder, version int16) (err error) {
	r.Version = version
	if r.Version > 0 {
		t, err := pd.getInt8()
		if err != nil {
			return err
		}
		r.Type = ElectionType(t)
	}

	topicCount, err := pd.getArrayLength()
	if err != nil {
		return err
	}
	if topicCount > 0 {
		r.TopicPartitions = make(map[string][]int32)
		for range topicCount {
			topic, err := pd.getString()
			if err != nil {
				return err
			}
			if r.TopicPartitions[topic], err = pd.getInt32Array(); err != nil {
				return err
			}
			if _, err := pd.getEmptyTaggedFieldArray(); err != nil {
				return err
			}
		}
	}

	r.TimeoutMs, err = pd.getInt32()
	if err != nil {
		return err
	}

	_, err = pd.getEmptyTaggedFieldArray()
	return err
}

func (r *ElectLeadersRequest) key() int16 {
	return apiKeyElectLeaders
}

func (r *ElectLeadersRequest) version() int16 {
	return r.Version
}

func (r *ElectLeadersRequest) headerVersion() int16 {
	if r.isFlexible() {
		return 2
	}
	return 1
}

func (r *ElectLeadersRequest) isValidVersion() bool {
	return r.Version >= 0 && r.Version <= 2
}

func (r *ElectLeadersRequest) isFlexible() bool {
	return r.isFlexibleVersion(r.Version)
}

func (r *ElectLeadersRequest) isFlexibleVersion(version int16) bool {
	return version >= 2
}

func (r *ElectLeadersRequest) requiredVersion() KafkaVersion {
	switch r.Version {
	case 2:
		return V2_4_0_0
	case 1:
		return V0_11_0_0
	case 0:
		return V0_10_0_0
	default:
		return V2_4_0_0
	}
}
