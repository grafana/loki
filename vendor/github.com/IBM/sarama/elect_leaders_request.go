package sarama

type ElectLeadersRequest struct {
	Version         int16
	Type            ElectionType
	TopicPartitions map[string][]int32
	TimeoutMs       int32
}

func (r *ElectLeadersRequest) encode(pe packetEncoder) error {
	if r.Version > 0 {
		pe.putInt8(int8(r.Type))
	}

	pe.putCompactArrayLength(len(r.TopicPartitions))

	for topic, partitions := range r.TopicPartitions {
		if r.Version < 2 {
			if err := pe.putString(topic); err != nil {
				return err
			}
		} else {
			if err := pe.putCompactString(topic); err != nil {
				return err
			}
		}

		if err := pe.putCompactInt32Array(partitions); err != nil {
			return err
		}

		if r.Version >= 2 {
			pe.putEmptyTaggedFieldArray()
		}
	}

	pe.putInt32(r.TimeoutMs)

	if r.Version >= 2 {
		pe.putEmptyTaggedFieldArray()
	}

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

	topicCount, err := pd.getCompactArrayLength()
	if err != nil {
		return err
	}
	if topicCount > 0 {
		r.TopicPartitions = make(map[string][]int32)
		for i := 0; i < topicCount; i++ {
			var topic string
			if r.Version < 2 {
				topic, err = pd.getString()
			} else {
				topic, err = pd.getCompactString()
			}
			if err != nil {
				return err
			}
			partitionCount, err := pd.getCompactArrayLength()
			if err != nil {
				return err
			}
			partitions := make([]int32, partitionCount)
			for j := 0; j < partitionCount; j++ {
				partition, err := pd.getInt32()
				if err != nil {
					return err
				}
				partitions[j] = partition
			}
			r.TopicPartitions[topic] = partitions
			if r.Version >= 2 {
				if _, err := pd.getEmptyTaggedFieldArray(); err != nil {
					return err
				}
			}
		}
	}

	r.TimeoutMs, err = pd.getInt32()
	if err != nil {
		return err
	}

	if r.Version >= 2 {
		if _, err := pd.getEmptyTaggedFieldArray(); err != nil {
			return err
		}
	}

	return nil
}

func (r *ElectLeadersRequest) key() int16 {
	return 43
}

func (r *ElectLeadersRequest) version() int16 {
	return r.Version
}

func (r *ElectLeadersRequest) headerVersion() int16 {
	return 2
}

func (r *ElectLeadersRequest) isValidVersion() bool {
	return r.Version >= 0 && r.Version <= 2
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
