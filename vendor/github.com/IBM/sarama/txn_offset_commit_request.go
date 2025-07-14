package sarama

type TxnOffsetCommitRequest struct {
	Version         int16
	TransactionalID string
	GroupID         string
	ProducerID      int64
	ProducerEpoch   int16
	Topics          map[string][]*PartitionOffsetMetadata
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
	}

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

	n, err := pd.getArrayLength()
	if err != nil {
		return err
	}

	t.Topics = make(map[string][]*PartitionOffsetMetadata)
	for i := 0; i < n; i++ {
		topic, err := pd.getString()
		if err != nil {
			return err
		}

		m, err := pd.getArrayLength()
		if err != nil {
			return err
		}

		t.Topics[topic] = make([]*PartitionOffsetMetadata, m)

		for j := 0; j < m; j++ {
			partitionOffsetMetadata := new(PartitionOffsetMetadata)
			if err := partitionOffsetMetadata.decode(pd, version); err != nil {
				return err
			}
			t.Topics[topic][j] = partitionOffsetMetadata
		}
	}

	return nil
}

func (a *TxnOffsetCommitRequest) key() int16 {
	return 28
}

func (a *TxnOffsetCommitRequest) version() int16 {
	return a.Version
}

func (a *TxnOffsetCommitRequest) headerVersion() int16 {
	return 1
}

func (a *TxnOffsetCommitRequest) isValidVersion() bool {
	return a.Version >= 0 && a.Version <= 2
}

func (a *TxnOffsetCommitRequest) requiredVersion() KafkaVersion {
	switch a.Version {
	case 2:
		return V2_1_0_0
	case 1:
		return V2_0_0_0
	case 0:
		return V0_11_0_0
	default:
		return V2_1_0_0
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

	return nil
}
