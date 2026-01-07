package sarama

import "time"

type OffsetFetchResponseBlock struct {
	Offset      int64
	LeaderEpoch int32
	Metadata    string
	Err         KError
}

func (b *OffsetFetchResponseBlock) decode(pd packetDecoder, version int16) (err error) {
	b.Offset, err = pd.getInt64()
	if err != nil {
		return err
	}

	if version >= 5 {
		b.LeaderEpoch, err = pd.getInt32()
		if err != nil {
			return err
		}
	} else {
		b.LeaderEpoch = -1
	}

	b.Metadata, err = pd.getString()
	if err != nil {
		return err
	}

	b.Err, err = pd.getKError()
	if err != nil {
		return err
	}

	_, err = pd.getEmptyTaggedFieldArray()
	return err
}

func (b *OffsetFetchResponseBlock) encode(pe packetEncoder, version int16) (err error) {
	pe.putInt64(b.Offset)

	if version >= 5 {
		pe.putInt32(b.LeaderEpoch)
	}
	err = pe.putString(b.Metadata)
	if err != nil {
		return err
	}

	pe.putKError(b.Err)

	pe.putEmptyTaggedFieldArray()
	return nil
}

type OffsetFetchResponse struct {
	Version        int16
	ThrottleTimeMs int32
	Blocks         map[string]map[int32]*OffsetFetchResponseBlock
	Err            KError
}

func (r *OffsetFetchResponse) setVersion(v int16) {
	r.Version = v
}

func (r *OffsetFetchResponse) encode(pe packetEncoder) (err error) {
	if r.Version >= 3 {
		pe.putInt32(r.ThrottleTimeMs)
	}
	err = pe.putArrayLength(len(r.Blocks))
	if err != nil {
		return err
	}

	for topic, partitions := range r.Blocks {
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
			if err := block.encode(pe, r.Version); err != nil {
				return err
			}
		}
		pe.putEmptyTaggedFieldArray()
	}
	if r.Version >= 2 {
		pe.putKError(r.Err)
	}
	pe.putEmptyTaggedFieldArray()
	return nil
}

func (r *OffsetFetchResponse) decode(pd packetDecoder, version int16) (err error) {
	r.Version = version

	if version >= 3 {
		r.ThrottleTimeMs, err = pd.getInt32()
		if err != nil {
			return err
		}
	}

	numTopics, err := pd.getArrayLength()
	if err != nil {
		return err
	}

	if numTopics > 0 {
		r.Blocks = make(map[string]map[int32]*OffsetFetchResponseBlock, numTopics)
		for i := 0; i < numTopics; i++ {
			name, err := pd.getString()
			if err != nil {
				return err
			}

			numBlocks, err := pd.getArrayLength()
			if err != nil {
				return err
			}

			r.Blocks[name] = nil
			if numBlocks > 0 {
				r.Blocks[name] = make(map[int32]*OffsetFetchResponseBlock, numBlocks)
			}
			for j := 0; j < numBlocks; j++ {
				id, err := pd.getInt32()
				if err != nil {
					return err
				}

				block := new(OffsetFetchResponseBlock)
				err = block.decode(pd, version)
				if err != nil {
					return err
				}

				r.Blocks[name][id] = block
			}

			if _, err := pd.getEmptyTaggedFieldArray(); err != nil {
				return err
			}
		}
	}

	if version >= 2 {
		r.Err, err = pd.getKError()
		if err != nil {
			return err
		}
	}

	_, err = pd.getEmptyTaggedFieldArray()
	return err
}

func (r *OffsetFetchResponse) key() int16 {
	return apiKeyOffsetFetch
}

func (r *OffsetFetchResponse) version() int16 {
	return r.Version
}

func (r *OffsetFetchResponse) headerVersion() int16 {
	if r.Version >= 6 {
		return 1
	}

	return 0
}

func (r *OffsetFetchResponse) isValidVersion() bool {
	return r.Version >= 0 && r.Version <= 7
}

func (r *OffsetFetchResponse) isFlexible() bool {
	return r.isFlexibleVersion(r.Version)
}

func (r *OffsetFetchResponse) isFlexibleVersion(version int16) bool {
	return version >= 6
}

func (r *OffsetFetchResponse) requiredVersion() KafkaVersion {
	switch r.Version {
	case 7:
		return V2_5_0_0
	case 6:
		return V2_4_0_0
	case 5:
		return V2_1_0_0
	case 4:
		return V2_0_0_0
	case 3:
		return V0_11_0_0
	case 2:
		return V0_10_2_0
	case 1:
		return V0_8_2_0
	case 0:
		return V0_8_2_0
	default:
		return V2_5_0_0
	}
}

func (r *OffsetFetchResponse) throttleTime() time.Duration {
	return time.Duration(r.ThrottleTimeMs) * time.Millisecond
}

func (r *OffsetFetchResponse) GetBlock(topic string, partition int32) *OffsetFetchResponseBlock {
	if r.Blocks == nil {
		return nil
	}

	if r.Blocks[topic] == nil {
		return nil
	}

	return r.Blocks[topic][partition]
}

func (r *OffsetFetchResponse) AddBlock(topic string, partition int32, block *OffsetFetchResponseBlock) {
	if r.Blocks == nil {
		r.Blocks = make(map[string]map[int32]*OffsetFetchResponseBlock)
	}
	partitions := r.Blocks[topic]
	if partitions == nil {
		partitions = make(map[int32]*OffsetFetchResponseBlock)
		r.Blocks[topic] = partitions
	}
	partitions[partition] = block
}
