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

	metadata, err := pd.getNullableString()
	if err != nil {
		return err
	}
	if metadata != nil {
		b.Metadata = *metadata
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
	err = pe.putNullableString(&b.Metadata)
	if err != nil {
		return err
	}

	pe.putKError(b.Err)

	pe.putEmptyTaggedFieldArray()
	return nil
}

// OffsetFetchResponseGroup is a per-group entry in an OffsetFetch v8+ response.
type OffsetFetchResponseGroup struct {
	GroupId string
	Blocks  map[string]map[int32]*OffsetFetchResponseBlock
	Err     KError
}

func (g *OffsetFetchResponseGroup) AddBlock(topic string, partition int32, block *OffsetFetchResponseBlock) {
	if g.Blocks == nil {
		g.Blocks = make(map[string]map[int32]*OffsetFetchResponseBlock)
	}
	partitions := g.Blocks[topic]
	if partitions == nil {
		partitions = make(map[int32]*OffsetFetchResponseBlock)
		g.Blocks[topic] = partitions
	}
	partitions[partition] = block
}

func (g *OffsetFetchResponseGroup) GetBlock(topic string, partition int32) *OffsetFetchResponseBlock {
	return g.Blocks[topic][partition]
}

type OffsetFetchResponse struct {
	Version        int16
	ThrottleTimeMs int32
	Blocks         map[string]map[int32]*OffsetFetchResponseBlock // v0-7; on v8+ see Groups
	Err            KError                                         // v0-7; on v8+ see Groups[i].Err
	Groups         []OffsetFetchResponseGroup                     // v8+
}

func (r *OffsetFetchResponse) setVersion(v int16) {
	r.Version = v
}

func (r *OffsetFetchResponse) encode(pe packetEncoder) (err error) {
	if r.Version >= 3 {
		pe.putInt32(r.ThrottleTimeMs)
	}

	if r.Version >= 8 {
		if err := pe.putArrayLength(len(r.Groups)); err != nil {
			return err
		}
		for _, g := range r.Groups {
			if err := pe.putString(g.GroupId); err != nil {
				return err
			}

			if err := pe.putArrayLength(len(g.Blocks)); err != nil {
				return err
			}
			for topic, partitions := range g.Blocks {
				if err := pe.putString(topic); err != nil {
					return err
				}
				if err := pe.putArrayLength(len(partitions)); err != nil {
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

			pe.putKError(g.Err)
			pe.putEmptyTaggedFieldArray()
		}

		pe.putEmptyTaggedFieldArray()
		return nil
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

	if version >= 8 {
		groupCount, err := pd.getArrayLength()
		if err != nil {
			return err
		}
		if groupCount < 0 {
			return errInvalidArrayLength
		}
		if groupCount > 0 {
			r.Groups = make([]OffsetFetchResponseGroup, groupCount)
		}
		for i := range groupCount {
			groupID, err := pd.getString()
			if err != nil {
				return err
			}
			r.Groups[i].GroupId = groupID

			numTopics, err := pd.getArrayLength()
			if err != nil {
				return err
			}
			if numTopics < 0 {
				return errInvalidArrayLength
			}

			blocks := make(map[string]map[int32]*OffsetFetchResponseBlock, numTopics)
			for range numTopics {
				name, err := pd.getString()
				if err != nil {
					return err
				}

				numBlocks, err := pd.getArrayLength()
				if err != nil {
					return err
				}
				if numBlocks < 0 {
					return errInvalidArrayLength
				}

				if numBlocks > 0 {
					blocks[name] = make(map[int32]*OffsetFetchResponseBlock, numBlocks)
				}
				for range numBlocks {
					id, err := pd.getInt32()
					if err != nil {
						return err
					}

					block := new(OffsetFetchResponseBlock)
					if err := block.decode(pd, version); err != nil {
						return err
					}

					blocks[name][id] = block
				}

				if _, err := pd.getEmptyTaggedFieldArray(); err != nil {
					return err
				}
			}
			r.Groups[i].Blocks = blocks

			groupErr, err := pd.getKError()
			if err != nil {
				return err
			}
			r.Groups[i].Err = groupErr

			if _, err := pd.getEmptyTaggedFieldArray(); err != nil {
				return err
			}
		}
		if len(r.Groups) > 0 {
			r.Blocks = r.Groups[0].Blocks
			r.Err = r.Groups[0].Err
		}
		_, err = pd.getEmptyTaggedFieldArray()
		return err
	}

	numTopics, err := pd.getArrayLength()
	if err != nil {
		return err
	}
	if numTopics < 0 {
		return errInvalidArrayLength
	}

	if numTopics > 0 {
		r.Blocks = make(map[string]map[int32]*OffsetFetchResponseBlock, numTopics)
		for range numTopics {
			name, err := pd.getString()
			if err != nil {
				return err
			}

			numBlocks, err := pd.getArrayLength()
			if err != nil {
				return err
			}
			if numBlocks < 0 {
				return errInvalidArrayLength
			}

			r.Blocks[name] = nil
			if numBlocks > 0 {
				r.Blocks[name] = make(map[int32]*OffsetFetchResponseBlock, numBlocks)
			}
			for range numBlocks {
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
	return r.Version >= 0 && r.Version <= 8
}

func (r *OffsetFetchResponse) isFlexible() bool {
	return r.isFlexibleVersion(r.Version)
}

func (r *OffsetFetchResponse) isFlexibleVersion(version int16) bool {
	return version >= 6
}

func (r *OffsetFetchResponse) requiredVersion() KafkaVersion {
	switch r.Version {
	case 8:
		return V3_0_0_0
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

// GroupError returns the group-level error: for v8+ the error of Groups[0],
// otherwise the top-level Err.
func (r *OffsetFetchResponse) GroupError() KError {
	if r.Version >= 8 {
		if len(r.Groups) == 0 {
			return ErrNoError
		}
		return r.Groups[0].Err
	}
	return r.Err
}

// GetGroup returns the per-group entry for groupID, or nil if absent or on v0-7.
func (r *OffsetFetchResponse) GetGroup(groupID string) *OffsetFetchResponseGroup {
	if r.Version < 8 {
		return nil
	}
	for i := range r.Groups {
		if r.Groups[i].GroupId == groupID {
			return &r.Groups[i]
		}
	}
	return nil
}

// GetBlock returns the block for topic/partition. On v8+ it looks up Groups[0].
func (r *OffsetFetchResponse) GetBlock(topic string, partition int32) *OffsetFetchResponseBlock {
	if r.Version >= 8 {
		if len(r.Groups) == 0 {
			return nil
		}
		return r.Groups[0].GetBlock(topic, partition)
	}
	if r.Blocks == nil {
		return nil
	}

	if r.Blocks[topic] == nil {
		return nil
	}

	return r.Blocks[topic][partition]
}

// GetGroupBlock returns the block for groupID/topic/partition, or nil on v0-7.
func (r *OffsetFetchResponse) GetGroupBlock(groupID string, topic string, partition int32) *OffsetFetchResponseBlock {
	if r.Version < 8 {
		return nil
	}
	group := r.GetGroup(groupID)
	if group == nil {
		return nil
	}
	return group.GetBlock(topic, partition)
}

// AddBlock adds a block for topic/partition. On v8+ it appends under Groups[0],
// creating it if needed.
func (r *OffsetFetchResponse) AddBlock(topic string, partition int32, block *OffsetFetchResponseBlock) {
	if r.Version >= 8 {
		if len(r.Groups) == 0 {
			r.Groups = []OffsetFetchResponseGroup{{}}
		}
		r.Groups[0].AddBlock(topic, partition, block)
		return
	}
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

// AddGroupBlock adds a block for groupID/topic/partition, creating the group
// entry if needed. On v0-7 it falls back to AddBlock and ignores groupID.
func (r *OffsetFetchResponse) AddGroupBlock(groupID string, topic string, partition int32, block *OffsetFetchResponseBlock) {
	if r.Version < 8 {
		r.AddBlock(topic, partition, block)
		return
	}
	group := r.GetGroup(groupID)
	if group == nil {
		r.Groups = append(r.Groups, OffsetFetchResponseGroup{GroupId: groupID})
		group = &r.Groups[len(r.Groups)-1]
	}
	group.AddBlock(topic, partition, block)
}
