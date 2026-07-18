package sarama

// OffsetFetchRequestGroup is a per-group entry in an OffsetFetch v8+ request.
// A nil Partitions map fetches offsets for all topics in the group.
type OffsetFetchRequestGroup struct {
	GroupId    string
	Partitions map[string][]int32
}

func (g *OffsetFetchRequestGroup) AddPartition(topic string, partitionID int32) {
	if g.Partitions == nil {
		g.Partitions = make(map[string][]int32)
	}
	g.Partitions[topic] = append(g.Partitions[topic], partitionID)
}

type OffsetFetchRequest struct {
	Version       int16
	ConsumerGroup string // v0-7; on v8+ populate Groups instead
	RequireStable bool   // v7+
	partitions    map[string][]int32
	Groups        []OffsetFetchRequestGroup // v8+
}

func (r *OffsetFetchRequest) setVersion(v int16) {
	r.Version = v
}

func NewOffsetFetchRequest(
	version KafkaVersion,
	group string,
	partitions map[string][]int32,
) *OffsetFetchRequest {
	request := &OffsetFetchRequest{
		ConsumerGroup: group,
		partitions:    partitions,
	}
	if version.IsAtLeast(V3_0_0_0) {
		// Version 8 is adding support for fetching offsets for multiple groups at a time.
		request.Version = 8
		request.Groups = []OffsetFetchRequestGroup{{GroupId: group, Partitions: partitions}}
		return request
	}
	if version.IsAtLeast(V2_5_0_0) {
		// Version 7 is adding the require stable flag.
		request.Version = 7
	} else if version.IsAtLeast(V2_4_0_0) {
		// Version 6 is the first flexible version.
		request.Version = 6
	} else if version.IsAtLeast(V2_1_0_0) {
		// Version 3, 4, and 5 are the same as version 2.
		request.Version = 5
	} else if version.IsAtLeast(V2_0_0_0) {
		request.Version = 4
	} else if version.IsAtLeast(V0_11_0_0) {
		request.Version = 3
	} else if version.IsAtLeast(V0_10_2_0) {
		// Starting in version 2, the request can contain a null topics array to indicate that offsets
		// for all topics should be fetched. It also returns a top level error code
		// for group or coordinator level errors.
		request.Version = 2
	} else if version.IsAtLeast(V0_8_2_0) {
		// In version 0, the request read offsets from ZK.
		//
		// Starting in version 1, the broker supports fetching offsets from the internal __consumer_offsets topic.
		request.Version = 1
	}

	return request
}

func (r *OffsetFetchRequest) encode(pe packetEncoder) (err error) {
	if r.Version < 0 || r.Version > 8 {
		return PacketEncodingError{"invalid or unsupported OffsetFetchRequest version field"}
	}

	if r.RequireStable && r.Version < 7 {
		return PacketEncodingError{"requireStable is not supported. use version 7 or later"}
	}

	if r.Version >= 8 {
		if len(r.Groups) == 0 {
			return PacketEncodingError{"version 8 or later requires Groups to be populated"}
		}
		if err := pe.putArrayLength(len(r.Groups)); err != nil {
			return err
		}
		for _, g := range r.Groups {
			if err := pe.putString(g.GroupId); err != nil {
				return err
			}

			// nil Partitions encodes as null topics array (fetch all)
			topicCount := len(g.Partitions)
			if g.Partitions == nil {
				topicCount = -1
			}
			if err := pe.putArrayLength(topicCount); err != nil {
				return err
			}
			for topic, partitions := range g.Partitions {
				if err := pe.putString(topic); err != nil {
					return err
				}
				if err := pe.putInt32Array(partitions); err != nil {
					return err
				}
				pe.putEmptyTaggedFieldArray()
			}

			pe.putEmptyTaggedFieldArray()
		}

		pe.putBool(r.RequireStable)
		pe.putEmptyTaggedFieldArray()
		return nil
	}

	if len(r.Groups) > 1 {
		return PacketEncodingError{"multiple groups require version 8 or later"}
	}

	consumerGroup := r.ConsumerGroup
	partitions := r.partitions
	if consumerGroup == "" && len(r.Groups) == 1 {
		consumerGroup = r.Groups[0].GroupId
		partitions = r.Groups[0].Partitions
	}

	err = pe.putString(consumerGroup)
	if err != nil {
		return err
	}

	if partitions == nil && r.Version >= 2 {
		if err := pe.putArrayLength(-1); err != nil {
			return err
		}
	} else {
		if err = pe.putArrayLength(len(partitions)); err != nil {
			return err
		}
	}

	for topic, partitions := range partitions {
		err = pe.putString(topic)
		if err != nil {
			return err
		}

		err = pe.putInt32Array(partitions)
		if err != nil {
			return err
		}

		pe.putEmptyTaggedFieldArray()
	}

	if r.Version >= 7 {
		pe.putBool(r.RequireStable)
	}

	pe.putEmptyTaggedFieldArray()
	return nil
}

func (r *OffsetFetchRequest) decode(pd packetDecoder, version int16) (err error) {
	r.Version = version

	if r.Version >= 8 {
		groupCount, err := pd.getArrayLength()
		if err != nil {
			return err
		}
		if groupCount < 0 {
			return errInvalidArrayLength
		}
		if groupCount > 0 {
			r.Groups = make([]OffsetFetchRequestGroup, groupCount)
		}
		for i := range groupCount {
			groupID, err := pd.getString()
			if err != nil {
				return err
			}
			r.Groups[i].GroupId = groupID

			// peek first byte to distinguish null (0x00) from empty (0x01)
			topicCountMarker, err := pd.peekInt8(0)
			if err != nil {
				return err
			}
			topicCount, err := pd.getArrayLength()
			if err != nil {
				return err
			}
			if topicCountMarker != 0 {
				partitions := make(map[string][]int32, topicCount)
				for range topicCount {
					topic, err := pd.getString()
					if err != nil {
						return err
					}
					ps, err := pd.getInt32Array()
					if err != nil {
						return err
					}
					if _, err := pd.getEmptyTaggedFieldArray(); err != nil {
						return err
					}
					partitions[topic] = ps
				}
				r.Groups[i].Partitions = partitions
			}

			if _, err := pd.getEmptyTaggedFieldArray(); err != nil {
				return err
			}
		}

		r.RequireStable, err = pd.getBool()
		if err != nil {
			return err
		}

		_, err = pd.getEmptyTaggedFieldArray()
		return err
	}

	r.ConsumerGroup, err = pd.getString()
	if err != nil {
		return err
	}

	partitionCount, err := pd.getArrayLength()
	if err != nil {
		return err
	}

	if (partitionCount == 0 && version < 2) || partitionCount < 0 {
		return nil
	}

	r.partitions = make(map[string][]int32, partitionCount)
	for range partitionCount {
		topic, err := pd.getString()
		if err != nil {
			return err
		}

		partitions, err := pd.getInt32Array()
		if err != nil {
			return err
		}
		_, err = pd.getEmptyTaggedFieldArray()
		if err != nil {
			return err
		}

		r.partitions[topic] = partitions
	}

	if r.Version >= 7 {
		r.RequireStable, err = pd.getBool()
		if err != nil {
			return err
		}
	}

	_, err = pd.getEmptyTaggedFieldArray()
	return err
}

func (r *OffsetFetchRequest) key() int16 {
	return apiKeyOffsetFetch
}

func (r *OffsetFetchRequest) version() int16 {
	return r.Version
}

func (r *OffsetFetchRequest) headerVersion() int16 {
	if r.Version >= 6 {
		return 2
	}

	return 1
}

func (r *OffsetFetchRequest) isValidVersion() bool {
	return r.Version >= 0 && r.Version <= 8
}

func (r *OffsetFetchRequest) isFlexible() bool {
	return r.isFlexibleVersion(r.Version)
}

func (r *OffsetFetchRequest) isFlexibleVersion(version int16) bool {
	return version >= 6
}

func (r *OffsetFetchRequest) requiredVersion() KafkaVersion {
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

func (r *OffsetFetchRequest) ZeroPartitions() {
	if r.Version >= 8 {
		if len(r.Groups) == 0 {
			r.Groups = []OffsetFetchRequestGroup{{GroupId: r.ConsumerGroup}}
		}
		if r.Groups[0].Partitions == nil {
			r.Groups[0].Partitions = make(map[string][]int32)
		}
		return
	}
	if r.partitions == nil && r.Version >= 2 {
		r.partitions = make(map[string][]int32)
	}
}

// AddPartition appends a partition to the request. On v8+ it targets Groups[0]
// (seeded from ConsumerGroup if needed); use AddGroupPartition for v8 batches.
func (r *OffsetFetchRequest) AddPartition(topic string, partitionID int32) {
	if r.Version >= 8 {
		if len(r.Groups) == 0 {
			r.Groups = []OffsetFetchRequestGroup{{GroupId: r.ConsumerGroup}}
		}
		r.Groups[0].AddPartition(topic, partitionID)
		return
	}
	if r.partitions == nil {
		r.partitions = make(map[string][]int32)
	}

	r.partitions[topic] = append(r.partitions[topic], partitionID)
}

// AddGroupPartition adds a partition under group in a v8+ batch, creating the
// group entry if absent.
func (r *OffsetFetchRequest) AddGroupPartition(group, topic string, partitionID int32) {
	for i := range r.Groups {
		if r.Groups[i].GroupId == group {
			r.Groups[i].AddPartition(topic, partitionID)
			return
		}
	}
	g := OffsetFetchRequestGroup{GroupId: group}
	g.AddPartition(topic, partitionID)
	r.Groups = append(r.Groups, g)
}
