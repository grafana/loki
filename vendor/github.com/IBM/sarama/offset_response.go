package sarama

import "time"

type OffsetResponseBlock struct {
	Err KError
	// Offsets contains the result offsets (for V0/V1 compatibility)
	Offsets []int64 // Version 0
	// Timestamp contains the timestamp associated with the returned offset.
	Timestamp int64 // Version 1
	// Offset contains the returned offset.
	Offset int64 // Version 1
	// LeaderEpoch contains the current leader epoch of the partition.
	LeaderEpoch int32
}

func (b *OffsetResponseBlock) decode(pd packetDecoder, version int16) (err error) {
	tmp, err := pd.getInt16()
	if err != nil {
		return err
	}
	b.Err = KError(tmp)

	if version == 0 {
		b.Offsets, err = pd.getInt64Array()
		return err
	}

	if version >= 1 {
		b.Timestamp, err = pd.getInt64()
		if err != nil {
			return err
		}

		b.Offset, err = pd.getInt64()
		if err != nil {
			return err
		}

		// For backwards compatibility put the offset in the offsets array too
		b.Offsets = []int64{b.Offset}
	}

	if version >= 4 {
		if b.LeaderEpoch, err = pd.getInt32(); err != nil {
			return err
		}
	}

	return nil
}

func (b *OffsetResponseBlock) encode(pe packetEncoder, version int16) (err error) {
	pe.putInt16(int16(b.Err))

	if version == 0 {
		return pe.putInt64Array(b.Offsets)
	}

	if version >= 1 {
		pe.putInt64(b.Timestamp)
		pe.putInt64(b.Offset)
	}

	if version >= 4 {
		pe.putInt32(b.LeaderEpoch)
	}

	return nil
}

type OffsetResponse struct {
	Version        int16
	ThrottleTimeMs int32
	Blocks         map[string]map[int32]*OffsetResponseBlock
}

func (r *OffsetResponse) decode(pd packetDecoder, version int16) (err error) {
	if version >= 2 {
		r.ThrottleTimeMs, err = pd.getInt32()
		if err != nil {
			return err
		}
	}

	numTopics, err := pd.getArrayLength()
	if err != nil {
		return err
	}

	r.Blocks = make(map[string]map[int32]*OffsetResponseBlock, numTopics)
	for i := 0; i < numTopics; i++ {
		name, err := pd.getString()
		if err != nil {
			return err
		}

		numBlocks, err := pd.getArrayLength()
		if err != nil {
			return err
		}

		r.Blocks[name] = make(map[int32]*OffsetResponseBlock, numBlocks)

		for j := 0; j < numBlocks; j++ {
			id, err := pd.getInt32()
			if err != nil {
				return err
			}

			block := new(OffsetResponseBlock)
			err = block.decode(pd, version)
			if err != nil {
				return err
			}
			r.Blocks[name][id] = block
		}
	}

	return nil
}

func (r *OffsetResponse) GetBlock(topic string, partition int32) *OffsetResponseBlock {
	if r.Blocks == nil {
		return nil
	}

	if r.Blocks[topic] == nil {
		return nil
	}

	return r.Blocks[topic][partition]
}

/*
// [0 0 0 1 ntopics
0 8 109 121 95 116 111 112 105 99 topic
0 0 0 1 npartitions
0 0 0 0 id
0 0

0 0 0 1 0 0 0 0
0 1 1 1 0 0 0 1
0 8 109 121 95 116 111 112
105 99 0 0 0 1 0 0
0 0 0 0 0 0 0 1
0 0 0 0 0 1 1 1] <nil>
*/
func (r *OffsetResponse) encode(pe packetEncoder) (err error) {
	if r.Version >= 2 {
		pe.putInt32(r.ThrottleTimeMs)
	}

	if err = pe.putArrayLength(len(r.Blocks)); err != nil {
		return err
	}

	for topic, partitions := range r.Blocks {
		if err = pe.putString(topic); err != nil {
			return err
		}
		if err = pe.putArrayLength(len(partitions)); err != nil {
			return err
		}
		for partition, block := range partitions {
			pe.putInt32(partition)
			if err = block.encode(pe, r.version()); err != nil {
				return err
			}
		}
	}

	return nil
}

func (r *OffsetResponse) key() int16 {
	return 2
}

func (r *OffsetResponse) version() int16 {
	return r.Version
}

func (r *OffsetResponse) headerVersion() int16 {
	return 0
}

func (r *OffsetResponse) isValidVersion() bool {
	return r.Version >= 0 && r.Version <= 4
}

func (r *OffsetResponse) requiredVersion() KafkaVersion {
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

func (r *OffsetResponse) throttleTime() time.Duration {
	return time.Duration(r.ThrottleTimeMs) * time.Millisecond
}

// testing API

func (r *OffsetResponse) AddTopicPartition(topic string, partition int32, offset int64) {
	if r.Blocks == nil {
		r.Blocks = make(map[string]map[int32]*OffsetResponseBlock)
	}
	byTopic, ok := r.Blocks[topic]
	if !ok {
		byTopic = make(map[int32]*OffsetResponseBlock)
		r.Blocks[topic] = byTopic
	}
	byTopic[partition] = &OffsetResponseBlock{Offsets: []int64{offset}, Offset: offset}
}
