package sarama

import "errors"

// ReceiveTime is a special value for the timestamp field of Offset Commit Requests which
// tells the broker to set the timestamp to the time at which the request was received.
// The timestamp is only used if message version 1 is used, which requires kafka 0.8.2.
const ReceiveTime int64 = -1

// GroupGenerationUndefined is a special value for the group generation field of
// Offset Commit Requests that should be used when a consumer group does not rely
// on Kafka for partition management.
const GroupGenerationUndefined = -1

type offsetCommitRequestBlock struct {
	offset               int64
	timestamp            int64
	committedLeaderEpoch int32
	metadata             string
}

func (b *offsetCommitRequestBlock) encode(pe packetEncoder, version int16) error {
	pe.putInt64(b.offset)
	if version == 1 {
		pe.putInt64(b.timestamp)
	} else if b.timestamp != 0 {
		Logger.Println("Non-zero timestamp specified for OffsetCommitRequest not v1, it will be ignored")
	}
	if version >= 6 {
		pe.putInt32(b.committedLeaderEpoch)
	}

	if err := pe.putString(b.metadata); err != nil {
		return err
	}
	pe.putEmptyTaggedFieldArray()
	return nil
}

func (b *offsetCommitRequestBlock) decode(pd packetDecoder, version int16) (err error) {
	if b.offset, err = pd.getInt64(); err != nil {
		return err
	}
	if version == 1 {
		if b.timestamp, err = pd.getInt64(); err != nil {
			return err
		}
	}
	if version >= 6 {
		if b.committedLeaderEpoch, err = pd.getInt32(); err != nil {
			return err
		}
	}

	if b.metadata, err = pd.getString(); err != nil {
		return err
	}
	_, err = pd.getEmptyTaggedFieldArray()
	return err
}

type OffsetCommitRequest struct {
	ConsumerGroup           string
	ConsumerGroupGeneration int32   // v1 or later
	ConsumerID              string  // v1 or later
	GroupInstanceId         *string // v7 or later
	RetentionTime           int64   // v2 or later

	// Version can be:
	// - 0 (kafka 0.8.1 and later)
	// - 1 (kafka 0.8.2 and later)
	// - 2 (kafka 0.9.0 and later)
	// - 3 (kafka 0.11.0 and later)
	// - 4 (kafka 2.0.0 and later)
	// - 5&6 (kafka 2.1.0 and later)
	// - 7 (kafka 2.3.0 and later)
	// - 8 (kafka 2.4.0 and later, first flexible version)
	Version int16
	blocks  map[string]map[int32]*offsetCommitRequestBlock
}

func (r *OffsetCommitRequest) setVersion(v int16) {
	r.Version = v
}

// NewOffsetCommitRequest creates an OffsetCommitRequest initialized for admin use.
//
// The version-mapping logic mirrors offsetManager.constructRequest in
// offset_manager.go; protocol bumps must be applied to both call sites.
func NewOffsetCommitRequest(conf *Config, group string) *OffsetCommitRequest {
	request := &OffsetCommitRequest{
		ConsumerGroup:           group,
		ConsumerGroupGeneration: GroupGenerationUndefined,
	}

	if conf.Version.IsAtLeast(V2_4_0_0) {
		// Version 8 is the first flexible version.
		request.Version = 8
	} else if conf.Version.IsAtLeast(V2_3_0_0) {
		// Version 7 adds GroupInstanceId.
		request.Version = 7
	} else if conf.Version.IsAtLeast(V2_1_0_0) {
		// Version 6 adds committed leader epoch (version 5 removes retention time).
		request.Version = 6
	} else if conf.Version.IsAtLeast(V2_0_0_0) {
		// Version 4 is the same as version 2.
		request.Version = 4
	} else if conf.Version.IsAtLeast(V0_11_0_0) {
		// Version 3 is the same as version 2.
		request.Version = 3
	} else if conf.Version.IsAtLeast(V0_9_0_0) {
		// Version 2 adds retention time and removes the commit timestamp from version 1.
		request.Version = 2
	} else {
		// Version 1 adds commit timestamp and group membership.
		request.Version = 1
	}

	if request.Version >= 2 && request.Version < 5 {
		request.RetentionTime = -1
		if conf.Consumer.Offsets.Retention > 0 {
			request.RetentionTime = conf.Consumer.Offsets.Retention.Milliseconds()
		}
	}

	return request
}

func (r *OffsetCommitRequest) encode(pe packetEncoder) error {
	if r.Version < 0 || r.Version > 8 {
		return PacketEncodingError{"invalid or unsupported OffsetCommitRequest version field"}
	}

	if err := pe.putString(r.ConsumerGroup); err != nil {
		return err
	}

	if r.Version >= 1 {
		pe.putInt32(r.ConsumerGroupGeneration)
		if err := pe.putString(r.ConsumerID); err != nil {
			return err
		}
	} else {
		if r.ConsumerGroupGeneration != 0 {
			Logger.Println("Non-zero ConsumerGroupGeneration specified for OffsetCommitRequest v0, it will be ignored")
		}
		if r.ConsumerID != "" {
			Logger.Println("Non-empty ConsumerID specified for OffsetCommitRequest v0, it will be ignored")
		}
	}

	// Version 5 removes RetentionTime, which is now controlled only by a broker configuration.
	if r.Version >= 2 && r.Version <= 4 {
		pe.putInt64(r.RetentionTime)
	} else if r.RetentionTime != 0 {
		Logger.Println("Non-zero RetentionTime specified for OffsetCommitRequest version <2, it will be ignored")
	}

	if r.Version >= 7 {
		if err := pe.putNullableString(r.GroupInstanceId); err != nil {
			return err
		}
	}

	if err := pe.putArrayLength(len(r.blocks)); err != nil {
		return err
	}
	for topic, partitions := range r.blocks {
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
	pe.putEmptyTaggedFieldArray()
	return nil
}

func (r *OffsetCommitRequest) decode(pd packetDecoder, version int16) (err error) {
	r.Version = version

	if r.ConsumerGroup, err = pd.getString(); err != nil {
		return err
	}

	if r.Version >= 1 {
		if r.ConsumerGroupGeneration, err = pd.getInt32(); err != nil {
			return err
		}
		if r.ConsumerID, err = pd.getString(); err != nil {
			return err
		}
	}

	// Version 5 removes RetentionTime, which is now controlled only by a broker configuration.
	if r.Version >= 2 && r.Version <= 4 {
		if r.RetentionTime, err = pd.getInt64(); err != nil {
			return err
		}
	}

	if r.Version >= 7 {
		if r.GroupInstanceId, err = pd.getNullableString(); err != nil {
			return err
		}
	}

	topicCount, err := pd.getArrayLength()
	if err != nil {
		return err
	}
	if topicCount < 0 {
		return errInvalidArrayLength
	}
	if topicCount > 0 {
		r.blocks = make(map[string]map[int32]*offsetCommitRequestBlock)
		for range topicCount {
			topic, err := pd.getString()
			if err != nil {
				return err
			}
			partitionCount, err := pd.getArrayLength()
			if err != nil {
				return err
			}
			if partitionCount < 0 {
				return errInvalidArrayLength
			}
			r.blocks[topic] = make(map[int32]*offsetCommitRequestBlock)
			for range partitionCount {
				partition, err := pd.getInt32()
				if err != nil {
					return err
				}
				block := &offsetCommitRequestBlock{}
				if err := block.decode(pd, r.Version); err != nil {
					return err
				}
				r.blocks[topic][partition] = block
			}
			if _, err := pd.getEmptyTaggedFieldArray(); err != nil {
				return err
			}
		}
	}

	_, err = pd.getEmptyTaggedFieldArray()
	return err
}

func (r *OffsetCommitRequest) key() int16 {
	return apiKeyOffsetCommit
}

func (r *OffsetCommitRequest) version() int16 {
	return r.Version
}

func (r *OffsetCommitRequest) headerVersion() int16 {
	if r.Version >= 8 {
		return 2
	}
	return 1
}

func (r *OffsetCommitRequest) isValidVersion() bool {
	return r.Version >= 0 && r.Version <= 8
}

func (r *OffsetCommitRequest) isFlexible() bool {
	return r.isFlexibleVersion(r.Version)
}

func (r *OffsetCommitRequest) isFlexibleVersion(version int16) bool {
	return version >= 8
}

func (r *OffsetCommitRequest) requiredVersion() KafkaVersion {
	switch r.Version {
	case 8:
		return V2_4_0_0
	case 7:
		return V2_3_0_0
	case 5, 6:
		return V2_1_0_0
	case 4:
		return V2_0_0_0
	case 3:
		return V0_11_0_0
	case 2:
		return V0_9_0_0
	case 0, 1:
		return V0_8_2_0
	default:
		return V2_4_0_0
	}
}

func (r *OffsetCommitRequest) AddBlock(topic string, partitionID int32, offset int64, timestamp int64, metadata string) {
	r.AddBlockWithLeaderEpoch(topic, partitionID, offset, 0, timestamp, metadata)
}

func (r *OffsetCommitRequest) AddBlockWithLeaderEpoch(topic string, partitionID int32, offset int64, leaderEpoch int32, timestamp int64, metadata string) {
	if r.blocks == nil {
		r.blocks = make(map[string]map[int32]*offsetCommitRequestBlock)
	}

	if r.blocks[topic] == nil {
		r.blocks[topic] = make(map[int32]*offsetCommitRequestBlock)
	}

	r.blocks[topic][partitionID] = &offsetCommitRequestBlock{offset, timestamp, leaderEpoch, metadata}
}

func (r *OffsetCommitRequest) Offset(topic string, partitionID int32) (int64, string, error) {
	partitions := r.blocks[topic]
	if partitions == nil {
		return 0, "", errors.New("no such offset")
	}
	block := partitions[partitionID]
	if block == nil {
		return 0, "", errors.New("no such offset")
	}
	return block.offset, block.metadata, nil
}
