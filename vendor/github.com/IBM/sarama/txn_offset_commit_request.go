package sarama

type TxnOffsetCommitRequest struct {
	Version         int16
	TransactionalID string
	GroupID         string
	ProducerID      int64
	ProducerEpoch   int16
	GenerationID    int32   // v3+, generation of the group or member epoch
	MemberID        string  // v3+, member ID assigned by the group coordinator
	GroupInstanceID *string // v3+, unique identifier of the consumer instance
	Topics          map[string][]*PartitionOffsetMetadata
}

func (t *TxnOffsetCommitRequest) setVersion(v int16) {
	t.Version = v
}

func (t *TxnOffsetCommitRequest) encode(pe packetEncoder) error {
	if err := pe.putString(t.TransactionalID); err != nil {
		return err
	}
	if err := pe.putString(t.GroupID); err != nil {
		return err
	}
	pe.putInt64(t.ProducerID)
	pe.putInt16(t.ProducerEpoch)

	if t.Version >= 3 {
		pe.putInt32(t.GenerationID)
		if err := pe.putString(t.MemberID); err != nil {
			return err
		}
		if err := pe.putNullableString(t.GroupInstanceID); err != nil {
			return err
		}
	}

	if err := pe.putArrayLength(len(t.Topics)); err != nil {
		return err
	}
	for topic, partitions := range t.Topics {
		if err := pe.putString(topic); err != nil {
			return err
		}
		if err := pe.putArrayLength(len(partitions)); err != nil {
			return err
		}
		for _, partition := range partitions {
			if err := partition.encode(pe, t.Version); err != nil {
				return err
			}
		}
		pe.putEmptyTaggedFieldArray()
	}

	pe.putEmptyTaggedFieldArray()
	return nil
}

func (t *TxnOffsetCommitRequest) decode(pd packetDecoder, version int16) (err error) {
	t.Version = version
	if t.TransactionalID, err = pd.getString(); err != nil {
		return err
	}
	if t.GroupID, err = pd.getString(); err != nil {
		return err
	}
	if t.ProducerID, err = pd.getInt64(); err != nil {
		return err
	}
	if t.ProducerEpoch, err = pd.getInt16(); err != nil {
		return err
	}

	if t.Version >= 3 {
		if t.GenerationID, err = pd.getInt32(); err != nil {
			return err
		}
		if t.MemberID, err = pd.getString(); err != nil {
			return err
		}
		if t.GroupInstanceID, err = pd.getNullableString(); err != nil {
			return err
		}
	}

	n, err := pd.getArrayLength()
	if err != nil {
		return err
	}
	if n < 0 {
		return errInvalidArrayLength
	}

	t.Topics = make(map[string][]*PartitionOffsetMetadata)
	for range n {
		topic, err := pd.getString()
		if err != nil {
			return err
		}

		m, err := pd.getArrayLength()
		if err != nil {
			return err
		}
		if m < 0 {
			return errInvalidArrayLength
		}

		t.Topics[topic] = make([]*PartitionOffsetMetadata, m)

		for j := range m {
			partitionOffsetMetadata := new(PartitionOffsetMetadata)
			if err := partitionOffsetMetadata.decode(pd, version); err != nil {
				return err
			}
			t.Topics[topic][j] = partitionOffsetMetadata
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

func (a *TxnOffsetCommitRequest) key() int16 {
	return apiKeyTxnOffsetCommit
}

func (a *TxnOffsetCommitRequest) version() int16 {
	return a.Version
}

func (a *TxnOffsetCommitRequest) headerVersion() int16 {
	if a.Version >= 3 {
		return 2
	}
	return 1
}

func (a *TxnOffsetCommitRequest) isValidVersion() bool {
	return a.Version >= 0 && a.Version <= 3
}

func (a *TxnOffsetCommitRequest) isFlexible() bool {
	return a.isFlexibleVersion(a.Version)
}

func (a *TxnOffsetCommitRequest) isFlexibleVersion(version int16) bool {
	return version >= 3
}

func (a *TxnOffsetCommitRequest) requiredVersion() KafkaVersion {
	switch a.Version {
	case 3:
		return V2_5_0_0
	case 2:
		return V2_1_0_0
	case 1:
		return V2_0_0_0
	case 0:
		return V0_11_0_0
	default:
		return V2_5_0_0
	}
}

type PartitionOffsetMetadata struct {
	// Partition contains the index of the partition within the topic.
	Partition int32
	// Offset contains the message offset to be committed.
	Offset int64
	// LeaderEpoch contains the leader epoch of the last consumed record.
	LeaderEpoch int32
	// Metadata contains any associated metadata the client wants to keep.
	Metadata *string
}

func (p *PartitionOffsetMetadata) encode(pe packetEncoder, version int16) error {
	pe.putInt32(p.Partition)
	pe.putInt64(p.Offset)

	if version >= 2 {
		pe.putInt32(p.LeaderEpoch)
	}

	if err := pe.putNullableString(p.Metadata); err != nil {
		return err
	}

	pe.putEmptyTaggedFieldArray()
	return nil
}

func (p *PartitionOffsetMetadata) decode(pd packetDecoder, version int16) (err error) {
	if p.Partition, err = pd.getInt32(); err != nil {
		return err
	}
	if p.Offset, err = pd.getInt64(); err != nil {
		return err
	}

	if version >= 2 {
		if p.LeaderEpoch, err = pd.getInt32(); err != nil {
			return err
		}
	}

	if p.Metadata, err = pd.getNullableString(); err != nil {
		return err
	}

	if _, err := pd.getEmptyTaggedFieldArray(); err != nil {
		return err
	}

	return nil
}
