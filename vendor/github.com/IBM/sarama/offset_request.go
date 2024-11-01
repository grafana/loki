package sarama

type offsetRequestBlock struct {
	// currentLeaderEpoch contains the current leader epoch (used in version 4+).
	currentLeaderEpoch int32
	// timestamp contains the current timestamp.
	timestamp int64
	// maxNumOffsets contains the maximum number of offsets to report.
	maxNumOffsets int32 // Only used in version 0
}

func (b *offsetRequestBlock) encode(pe packetEncoder, version int16) error {
	if version >= 4 {
		pe.putInt32(b.currentLeaderEpoch)
	}

	pe.putInt64(b.timestamp)

	if version == 0 {
		pe.putInt32(b.maxNumOffsets)
	}

	return nil
}

func (b *offsetRequestBlock) decode(pd packetDecoder, version int16) (err error) {
	b.currentLeaderEpoch = -1
	if version >= 4 {
		if b.currentLeaderEpoch, err = pd.getInt32(); err != nil {
			return err
		}
	}

	if b.timestamp, err = pd.getInt64(); err != nil {
		return err
	}

	if version == 0 {
		if b.maxNumOffsets, err = pd.getInt32(); err != nil {
			return err
		}
	}

	return nil
}

type OffsetRequest struct {
	Version        int16
	IsolationLevel IsolationLevel
	replicaID      int32
	isReplicaIDSet bool
	blocks         map[string]map[int32]*offsetRequestBlock
}

func (r *OffsetRequest) encode(pe packetEncoder) error {
	if r.isReplicaIDSet {
		pe.putInt32(r.replicaID)
	} else {
		// default replica ID is always -1 for clients
		pe.putInt32(-1)
	}

	if r.Version >= 2 {
		pe.putBool(r.IsolationLevel == ReadCommitted)
	}

	err := pe.putArrayLength(len(r.blocks))
	if err != nil {
		return err
	}
	for topic, partitions := range r.blocks {
		err = pe.putString(topic)
		if err != nil {
			return err
		}
		err = pe.putArrayLength(len(partitions))
		if err != nil {
			return err
		}
		for partition, block := range partitions {
			pe.putInt32(partition)
			if err = block.encode(pe, r.Version); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *OffsetRequest) decode(pd packetDecoder, version int16) error {
	r.Version = version

	replicaID, err := pd.getInt32()
	if err != nil {
		return err
	}
	if replicaID >= 0 {
		r.SetReplicaID(replicaID)
	}

	if r.Version >= 2 {
		tmp, err := pd.getBool()
		if err != nil {
			return err
		}

		r.IsolationLevel = ReadUncommitted
		if tmp {
			r.IsolationLevel = ReadCommitted
		}
	}

	blockCount, err := pd.getArrayLength()
	if err != nil {
		return err
	}
	if blockCount == 0 {
		return nil
	}
	r.blocks = make(map[string]map[int32]*offsetRequestBlock)
	for i := 0; i < blockCount; i++ {
		topic, err := pd.getString()
		if err != nil {
			return err
		}
		partitionCount, err := pd.getArrayLength()
		if err != nil {
			return err
		}
		r.blocks[topic] = make(map[int32]*offsetRequestBlock)
		for j := 0; j < partitionCount; j++ {
			partition, err := pd.getInt32()
			if err != nil {
				return err
			}
			block := &offsetRequestBlock{}
			if err := block.decode(pd, version); err != nil {
				return err
			}
			r.blocks[topic][partition] = block
		}
	}
	return nil
}

func (r *OffsetRequest) key() int16 {
	return 2
}

func (r *OffsetRequest) version() int16 {
	return r.Version
}

func (r *OffsetRequest) headerVersion() int16 {
	return 1
}

func (r *OffsetRequest) isValidVersion() bool {
	return r.Version >= 0 && r.Version <= 4
}

func (r *OffsetRequest) requiredVersion() KafkaVersion {
	switch r.Version {
	case 4:
		return V2_1_0_0
	case 3:
		return V2_0_0_0
	case 2:
		return V0_11_0_0
	case 1:
		return V0_10_1_0
	case 0:
		return V0_8_2_0
	default:
		return V2_0_0_0
	}
}

func (r *OffsetRequest) SetReplicaID(id int32) {
	r.replicaID = id
	r.isReplicaIDSet = true
}

func (r *OffsetRequest) ReplicaID() int32 {
	if r.isReplicaIDSet {
		return r.replicaID
	}
	return -1
}

func (r *OffsetRequest) AddBlock(topic string, partitionID int32, timestamp int64, maxOffsets int32) {
	if r.blocks == nil {
		r.blocks = make(map[string]map[int32]*offsetRequestBlock)
	}

	if r.blocks[topic] == nil {
		r.blocks[topic] = make(map[int32]*offsetRequestBlock)
	}

	tmp := new(offsetRequestBlock)
	tmp.currentLeaderEpoch = -1
	tmp.timestamp = timestamp
	if r.Version == 0 {
		tmp.maxNumOffsets = maxOffsets
	}

	r.blocks[topic][partitionID] = tmp
}
