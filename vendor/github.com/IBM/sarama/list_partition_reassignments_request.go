package sarama

type ListPartitionReassignmentsRequest struct {
	TimeoutMs int32
	blocks    map[string][]int32
	Version   int16
}

func (r *ListPartitionReassignmentsRequest) setVersion(v int16) {
	r.Version = v
}

func (r *ListPartitionReassignmentsRequest) encode(pe packetEncoder) error {
	pe.putInt32(r.TimeoutMs)

	if err := pe.putArrayLength(len(r.blocks)); err != nil {
		return err
	}

	for topic, partitions := range r.blocks {
		if err := pe.putString(topic); err != nil {
			return err
		}

		if err := pe.putInt32Array(partitions); err != nil {
			return err
		}

		pe.putEmptyTaggedFieldArray()
	}

	pe.putEmptyTaggedFieldArray()

	return nil
}

func (r *ListPartitionReassignmentsRequest) decode(pd packetDecoder, version int16) (err error) {
	r.Version = version

	if r.TimeoutMs, err = pd.getInt32(); err != nil {
		return err
	}

	topicCount, err := pd.getArrayLength()
	if err != nil {
		return err
	}
	if topicCount > 0 {
		r.blocks = make(map[string][]int32)
		for i := 0; i < topicCount; i++ {
			topic, err := pd.getString()
			if err != nil {
				return err
			}
			partitionCount, err := pd.getArrayLength()
			if err != nil {
				return err
			}
			r.blocks[topic] = make([]int32, partitionCount)
			for j := 0; j < partitionCount; j++ {
				partition, err := pd.getInt32()
				if err != nil {
					return err
				}
				r.blocks[topic][j] = partition
			}
			if _, err := pd.getEmptyTaggedFieldArray(); err != nil {
				return err
			}
		}
	}

	_, err = pd.getEmptyTaggedFieldArray()
	return err
}

func (r *ListPartitionReassignmentsRequest) key() int16 {
	return apiKeyListPartitionReassignments
}

func (r *ListPartitionReassignmentsRequest) version() int16 {
	return r.Version
}

func (r *ListPartitionReassignmentsRequest) headerVersion() int16 {
	return 2
}

func (r *ListPartitionReassignmentsRequest) isValidVersion() bool {
	return r.Version == 0
}

func (r *ListPartitionReassignmentsRequest) isFlexible() bool {
	return r.isFlexibleVersion(r.Version)
}

func (r *ListPartitionReassignmentsRequest) isFlexibleVersion(version int16) bool {
	return version >= 0
}

func (r *ListPartitionReassignmentsRequest) requiredVersion() KafkaVersion {
	return V2_4_0_0
}

func (r *ListPartitionReassignmentsRequest) AddBlock(topic string, partitionIDs []int32) {
	if r.blocks == nil {
		r.blocks = make(map[string][]int32)
	}

	if r.blocks[topic] == nil {
		r.blocks[topic] = partitionIDs
	}
}
