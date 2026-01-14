package sarama

import "time"

type PartitionReplicaReassignmentsStatus struct {
	Replicas         []int32
	AddingReplicas   []int32
	RemovingReplicas []int32
}

func (b *PartitionReplicaReassignmentsStatus) encode(pe packetEncoder) error {
	if err := pe.putInt32Array(b.Replicas); err != nil {
		return err
	}
	if err := pe.putInt32Array(b.AddingReplicas); err != nil {
		return err
	}
	if err := pe.putInt32Array(b.RemovingReplicas); err != nil {
		return err
	}

	pe.putEmptyTaggedFieldArray()

	return nil
}

func (b *PartitionReplicaReassignmentsStatus) decode(pd packetDecoder) (err error) {
	if b.Replicas, err = pd.getInt32Array(); err != nil {
		return err
	}

	if b.AddingReplicas, err = pd.getInt32Array(); err != nil {
		return err
	}

	if b.RemovingReplicas, err = pd.getInt32Array(); err != nil {
		return err
	}

	_, err = pd.getEmptyTaggedFieldArray()
	return err
}

type ListPartitionReassignmentsResponse struct {
	Version        int16
	ThrottleTimeMs int32
	ErrorCode      KError
	ErrorMessage   *string
	TopicStatus    map[string]map[int32]*PartitionReplicaReassignmentsStatus
}

func (r *ListPartitionReassignmentsResponse) setVersion(v int16) {
	r.Version = v
}

func (r *ListPartitionReassignmentsResponse) AddBlock(topic string, partition int32, replicas, addingReplicas, removingReplicas []int32) {
	if r.TopicStatus == nil {
		r.TopicStatus = make(map[string]map[int32]*PartitionReplicaReassignmentsStatus)
	}
	partitions := r.TopicStatus[topic]
	if partitions == nil {
		partitions = make(map[int32]*PartitionReplicaReassignmentsStatus)
		r.TopicStatus[topic] = partitions
	}

	partitions[partition] = &PartitionReplicaReassignmentsStatus{Replicas: replicas, AddingReplicas: addingReplicas, RemovingReplicas: removingReplicas}
}

func (r *ListPartitionReassignmentsResponse) encode(pe packetEncoder) error {
	pe.putInt32(r.ThrottleTimeMs)
	pe.putKError(r.ErrorCode)
	if err := pe.putNullableString(r.ErrorMessage); err != nil {
		return err
	}

	if err := pe.putArrayLength(len(r.TopicStatus)); err != nil {
		return err
	}
	for topic, partitions := range r.TopicStatus {
		if err := pe.putString(topic); err != nil {
			return err
		}
		if err := pe.putArrayLength(len(partitions)); err != nil {
			return err
		}
		for partition, block := range partitions {
			pe.putInt32(partition)

			if err := block.encode(pe); err != nil {
				return err
			}
		}
		pe.putEmptyTaggedFieldArray()
	}

	pe.putEmptyTaggedFieldArray()

	return nil
}

func (r *ListPartitionReassignmentsResponse) decode(pd packetDecoder, version int16) (err error) {
	r.Version = version

	if r.ThrottleTimeMs, err = pd.getInt32(); err != nil {
		return err
	}

	r.ErrorCode, err = pd.getKError()
	if err != nil {
		return err
	}

	if r.ErrorMessage, err = pd.getNullableString(); err != nil {
		return err
	}

	numTopics, err := pd.getArrayLength()
	if err != nil {
		return err
	}

	r.TopicStatus = make(map[string]map[int32]*PartitionReplicaReassignmentsStatus, numTopics)
	for i := 0; i < numTopics; i++ {
		topic, err := pd.getString()
		if err != nil {
			return err
		}

		ongoingPartitionReassignments, err := pd.getArrayLength()
		if err != nil {
			return err
		}

		r.TopicStatus[topic] = make(map[int32]*PartitionReplicaReassignmentsStatus, ongoingPartitionReassignments)

		for j := 0; j < ongoingPartitionReassignments; j++ {
			partition, err := pd.getInt32()
			if err != nil {
				return err
			}

			block := &PartitionReplicaReassignmentsStatus{}
			if err := block.decode(pd); err != nil {
				return err
			}
			r.TopicStatus[topic][partition] = block
		}

		if _, err := pd.getEmptyTaggedFieldArray(); err != nil {
			return err
		}
	}

	_, err = pd.getEmptyTaggedFieldArray()
	return err
}

func (r *ListPartitionReassignmentsResponse) key() int16 {
	return apiKeyListPartitionReassignments
}

func (r *ListPartitionReassignmentsResponse) version() int16 {
	return r.Version
}

func (r *ListPartitionReassignmentsResponse) headerVersion() int16 {
	return 1
}

func (r *ListPartitionReassignmentsResponse) isValidVersion() bool {
	return r.Version == 0
}

func (r *ListPartitionReassignmentsResponse) isFlexible() bool {
	return r.isFlexibleVersion(r.Version)
}

func (r *ListPartitionReassignmentsResponse) isFlexibleVersion(version int16) bool {
	return version >= 0
}

func (r *ListPartitionReassignmentsResponse) requiredVersion() KafkaVersion {
	return V2_4_0_0
}

func (r *ListPartitionReassignmentsResponse) throttleTime() time.Duration {
	return time.Duration(r.ThrottleTimeMs) * time.Millisecond
}
