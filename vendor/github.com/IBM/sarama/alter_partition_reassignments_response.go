package sarama

import "time"

type alterPartitionReassignmentsErrorBlock struct {
	errorCode    KError
	errorMessage *string
}

func (b *alterPartitionReassignmentsErrorBlock) encode(pe packetEncoder) error {
	pe.putKError(b.errorCode)
	if err := pe.putNullableString(b.errorMessage); err != nil {
		return err
	}
	pe.putEmptyTaggedFieldArray()

	return nil
}

func (b *alterPartitionReassignmentsErrorBlock) decode(pd packetDecoder) (err error) {
	b.errorCode, err = pd.getKError()
	if err != nil {
		return err
	}
	b.errorMessage, err = pd.getNullableString()
	if err != nil {
		return err
	}

	_, err = pd.getEmptyTaggedFieldArray()
	return err
}

type AlterPartitionReassignmentsResponse struct {
	Version        int16
	ThrottleTimeMs int32
	ErrorCode      KError
	ErrorMessage   *string
	Errors         map[string]map[int32]*alterPartitionReassignmentsErrorBlock
}

func (r *AlterPartitionReassignmentsResponse) setVersion(v int16) {
	r.Version = v
}

func (r *AlterPartitionReassignmentsResponse) AddError(topic string, partition int32, kerror KError, message *string) {
	if r.Errors == nil {
		r.Errors = make(map[string]map[int32]*alterPartitionReassignmentsErrorBlock)
	}
	partitions := r.Errors[topic]
	if partitions == nil {
		partitions = make(map[int32]*alterPartitionReassignmentsErrorBlock)
		r.Errors[topic] = partitions
	}

	partitions[partition] = &alterPartitionReassignmentsErrorBlock{errorCode: kerror, errorMessage: message}
}

func (r *AlterPartitionReassignmentsResponse) encode(pe packetEncoder) error {
	pe.putInt32(r.ThrottleTimeMs)
	pe.putKError(r.ErrorCode)
	if err := pe.putNullableString(r.ErrorMessage); err != nil {
		return err
	}

	if err := pe.putArrayLength(len(r.Errors)); err != nil {
		return err
	}
	for topic, partitions := range r.Errors {
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

func (r *AlterPartitionReassignmentsResponse) decode(pd packetDecoder, version int16) (err error) {
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

	if numTopics > 0 {
		r.Errors = make(map[string]map[int32]*alterPartitionReassignmentsErrorBlock, numTopics)
		for i := 0; i < numTopics; i++ {
			topic, err := pd.getString()
			if err != nil {
				return err
			}

			ongoingPartitionReassignments, err := pd.getArrayLength()
			if err != nil {
				return err
			}

			r.Errors[topic] = make(map[int32]*alterPartitionReassignmentsErrorBlock, ongoingPartitionReassignments)

			for j := 0; j < ongoingPartitionReassignments; j++ {
				partition, err := pd.getInt32()
				if err != nil {
					return err
				}
				block := &alterPartitionReassignmentsErrorBlock{}
				if err := block.decode(pd); err != nil {
					return err
				}

				r.Errors[topic][partition] = block
			}
			if _, err = pd.getEmptyTaggedFieldArray(); err != nil {
				return err
			}
		}
	}

	_, err = pd.getEmptyTaggedFieldArray()
	return err
}

func (r *AlterPartitionReassignmentsResponse) key() int16 {
	return apiKeyAlterPartitionReassignments
}

func (r *AlterPartitionReassignmentsResponse) version() int16 {
	return r.Version
}

func (r *AlterPartitionReassignmentsResponse) headerVersion() int16 {
	return 1
}

func (r *AlterPartitionReassignmentsResponse) isValidVersion() bool {
	return r.Version == 0
}

func (r *AlterPartitionReassignmentsResponse) isFlexible() bool {
	return r.isFlexibleVersion(r.Version)
}

func (r *AlterPartitionReassignmentsResponse) isFlexibleVersion(version int16) bool {
	return version >= 0
}

func (r *AlterPartitionReassignmentsResponse) requiredVersion() KafkaVersion {
	return V2_4_0_0
}

func (r *AlterPartitionReassignmentsResponse) throttleTime() time.Duration {
	return time.Duration(r.ThrottleTimeMs) * time.Millisecond
}
