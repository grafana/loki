package sarama

import "time"

type PartitionResult struct {
	ErrorCode    KError
	ErrorMessage *string
}

func (b *PartitionResult) encode(pe packetEncoder, version int16) error {
	pe.putKError(b.ErrorCode)
	if err := pe.putNullableString(b.ErrorMessage); err != nil {
		return err
	}
	pe.putEmptyTaggedFieldArray()
	return nil
}

func (b *PartitionResult) decode(pd packetDecoder, version int16) (err error) {
	b.ErrorCode, err = pd.getKError()
	if err != nil {
		return err
	}
	b.ErrorMessage, err = pd.getNullableString()
	if err != nil {
		return err
	}
	_, err = pd.getEmptyTaggedFieldArray()
	return err
}

type ElectLeadersResponse struct {
	Version                int16
	ThrottleTimeMs         int32
	ErrorCode              KError
	ReplicaElectionResults map[string]map[int32]*PartitionResult
}

func (r *ElectLeadersResponse) setVersion(v int16) {
	r.Version = v
}

func (r *ElectLeadersResponse) encode(pe packetEncoder) error {
	pe.putInt32(r.ThrottleTimeMs)

	if r.Version > 0 {
		pe.putKError(r.ErrorCode)
	}

	if err := pe.putArrayLength(len(r.ReplicaElectionResults)); err != nil {
		return err
	}
	for topic, partitions := range r.ReplicaElectionResults {
		if err := pe.putString(topic); err != nil {
			return err
		}
		if err := pe.putArrayLength(len(partitions)); err != nil {
			return err
		}
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
		r.ErrorCode, err = pd.getKError()
		if err != nil {
			return err
		}
	}

	numTopics, err := pd.getArrayLength()
	if err != nil {
		return err
	}

	r.ReplicaElectionResults = make(map[string]map[int32]*PartitionResult, numTopics)
	for i := 0; i < numTopics; i++ {
		topic, err := pd.getString()
		if err != nil {
			return err
		}

		numPartitions, err := pd.getArrayLength()
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

	_, err = pd.getEmptyTaggedFieldArray()
	return err
}

func (r *ElectLeadersResponse) key() int16 {
	return apiKeyElectLeaders
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

func (r *ElectLeadersResponse) isFlexible() bool {
	return r.isFlexibleVersion(r.Version)
}

func (r *ElectLeadersResponse) isFlexibleVersion(version int16) bool {
	return version >= 2
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
