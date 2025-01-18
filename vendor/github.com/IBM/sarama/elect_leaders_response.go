package sarama

import "time"

type PartitionResult struct {
	ErrorCode    KError
	ErrorMessage *string
}

func (b *PartitionResult) encode(pe packetEncoder, version int16) error {
	pe.putInt16(int16(b.ErrorCode))
	if version < 2 {
		if err := pe.putNullableString(b.ErrorMessage); err != nil {
			return err
		}
	} else {
		if err := pe.putNullableCompactString(b.ErrorMessage); err != nil {
			return err
		}
	}
	if version >= 2 {
		pe.putEmptyTaggedFieldArray()
	}
	return nil
}

func (b *PartitionResult) decode(pd packetDecoder, version int16) (err error) {
	kerr, err := pd.getInt16()
	if err != nil {
		return err
	}
	b.ErrorCode = KError(kerr)
	if version < 2 {
		b.ErrorMessage, err = pd.getNullableString()
	} else {
		b.ErrorMessage, err = pd.getCompactNullableString()
	}
	if version >= 2 {
		_, err = pd.getEmptyTaggedFieldArray()
	}
	return err
}

type ElectLeadersResponse struct {
	Version                int16
	ThrottleTimeMs         int32
	ErrorCode              KError
	ReplicaElectionResults map[string]map[int32]*PartitionResult
}

func (r *ElectLeadersResponse) encode(pe packetEncoder) error {
	pe.putInt32(r.ThrottleTimeMs)

	if r.Version > 0 {
		pe.putInt16(int16(r.ErrorCode))
	}

	pe.putCompactArrayLength(len(r.ReplicaElectionResults))
	for topic, partitions := range r.ReplicaElectionResults {
		if r.Version < 2 {
			if err := pe.putString(topic); err != nil {
				return err
			}
		} else {
			if err := pe.putCompactString(topic); err != nil {
				return err
			}
		}
		pe.putCompactArrayLength(len(partitions))
		for partition, result := range partitions {
			pe.putInt32(partition)
			if err := result.encode(pe, r.Version); err != nil {
				return err
			}
		}
		pe.putEmptyTaggedFieldArray()
	}

	pe.putEmptyTaggedFieldArray()

	return nil
}

func (r *ElectLeadersResponse) decode(pd packetDecoder, version int16) (err error) {
	r.Version = version
	if r.ThrottleTimeMs, err = pd.getInt32(); err != nil {
		return err
	}
	if r.Version > 0 {
		kerr, err := pd.getInt16()
		if err != nil {
			return err
		}
		r.ErrorCode = KError(kerr)
	}

	numTopics, err := pd.getCompactArrayLength()
	if err != nil {
		return err
	}

	r.ReplicaElectionResults = make(map[string]map[int32]*PartitionResult, numTopics)
	for i := 0; i < numTopics; i++ {
		var topic string
		if r.Version < 2 {
			topic, err = pd.getString()
		} else {
			topic, err = pd.getCompactString()
		}
		if err != nil {
			return err
		}

		numPartitions, err := pd.getCompactArrayLength()
		if err != nil {
			return err
		}
		r.ReplicaElectionResults[topic] = make(map[int32]*PartitionResult, numPartitions)
		for j := 0; j < numPartitions; j++ {
			partition, err := pd.getInt32()
			if err != nil {
				return err
			}
			result := new(PartitionResult)
			if err := result.decode(pd, r.Version); err != nil {
				return err
			}
			r.ReplicaElectionResults[topic][partition] = result
		}
		if _, err := pd.getEmptyTaggedFieldArray(); err != nil {
			return err
		}
	}

	if _, err := pd.getEmptyTaggedFieldArray(); err != nil {
		return err
	}

	return nil
}

func (r *ElectLeadersResponse) key() int16 {
	return 43
}

func (r *ElectLeadersResponse) version() int16 {
	return r.Version
}

func (r *ElectLeadersResponse) headerVersion() int16 {
	return 1
}

func (r *ElectLeadersResponse) isValidVersion() bool {
	return r.Version >= 0 && r.Version <= 2
}

func (r *ElectLeadersResponse) requiredVersion() KafkaVersion {
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

func (r *ElectLeadersResponse) throttleTime() time.Duration {
	return time.Duration(r.ThrottleTimeMs) * time.Millisecond
}
