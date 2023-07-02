package sarama

// PartitionMetadata contains each partition in the topic.
type PartitionMetadata struct {
	// Version defines the protocol version to use for encode and decode
	Version int16
	// Err contains the partition error, or 0 if there was no error.
	Err KError
	// ID contains the partition index.
	ID int32
	// Leader contains the ID of the leader broker.
	Leader int32
	// LeaderEpoch contains the leader epoch of this partition.
	LeaderEpoch int32
	// Replicas contains the set of all nodes that host this partition.
	Replicas []int32
	// Isr contains the set of nodes that are in sync with the leader for this partition.
	Isr []int32
	// OfflineReplicas contains the set of offline replicas of this partition.
	OfflineReplicas []int32
}

func (p *PartitionMetadata) decode(pd packetDecoder, version int16) (err error) {
	p.Version = version
	tmp, err := pd.getInt16()
	if err != nil {
		return err
	}
	p.Err = KError(tmp)

	if p.ID, err = pd.getInt32(); err != nil {
		return err
	}

	if p.Leader, err = pd.getInt32(); err != nil {
		return err
	}

	if p.Version >= 7 {
		if p.LeaderEpoch, err = pd.getInt32(); err != nil {
			return err
		}
	}

	if p.Replicas, err = pd.getInt32Array(); err != nil {
		return err
	}

	if p.Isr, err = pd.getInt32Array(); err != nil {
		return err
	}

	if p.Version >= 5 {
		if p.OfflineReplicas, err = pd.getInt32Array(); err != nil {
			return err
		}
	}

	return nil
}

func (p *PartitionMetadata) encode(pe packetEncoder, version int16) (err error) {
	p.Version = version
	pe.putInt16(int16(p.Err))

	pe.putInt32(p.ID)

	pe.putInt32(p.Leader)

	if p.Version >= 7 {
		pe.putInt32(p.LeaderEpoch)
	}

	if err := pe.putInt32Array(p.Replicas); err != nil {
		return err
	}

	if err := pe.putInt32Array(p.Isr); err != nil {
		return err
	}

	if p.Version >= 5 {
		if err := pe.putInt32Array(p.OfflineReplicas); err != nil {
			return err
		}
	}

	return nil
}

// TopicMetadata contains each topic in the response.
type TopicMetadata struct {
	// Version defines the protocol version to use for encode and decode
	Version int16
	// Err contains the topic error, or 0 if there was no error.
	Err KError
	// Name contains the topic name.
	Name string
	// IsInternal contains a True if the topic is internal.
	IsInternal bool
	// Partitions contains each partition in the topic.
	Partitions []*PartitionMetadata
}

func (t *TopicMetadata) decode(pd packetDecoder, version int16) (err error) {
	t.Version = version
	tmp, err := pd.getInt16()
	if err != nil {
		return err
	}
	t.Err = KError(tmp)

	if t.Name, err = pd.getString(); err != nil {
		return err
	}

	if t.Version >= 1 {
		if t.IsInternal, err = pd.getBool(); err != nil {
			return err
		}
	}

	if numPartitions, err := pd.getArrayLength(); err != nil {
		return err
	} else {
		t.Partitions = make([]*PartitionMetadata, numPartitions)
		for i := 0; i < numPartitions; i++ {
			block := &PartitionMetadata{}
			if err := block.decode(pd, t.Version); err != nil {
				return err
			}
			t.Partitions[i] = block
		}
	}

	return nil
}

func (t *TopicMetadata) encode(pe packetEncoder, version int16) (err error) {
	t.Version = version
	pe.putInt16(int16(t.Err))

	if err := pe.putString(t.Name); err != nil {
		return err
	}

	if t.Version >= 1 {
		pe.putBool(t.IsInternal)
	}

	if err := pe.putArrayLength(len(t.Partitions)); err != nil {
		return err
	}
	for _, block := range t.Partitions {
		if err := block.encode(pe, t.Version); err != nil {
			return err
		}
	}

	return nil
}

type MetadataResponse struct {
	// Version defines the protocol version to use for encode and decode
	Version int16
	// ThrottleTimeMs contains the duration in milliseconds for which the request was throttled due to a quota violation, or zero if the request did not violate any quota.
	ThrottleTimeMs int32
	// Brokers contains each broker in the response.
	Brokers []*Broker
	// ClusterID contains the cluster ID that responding broker belongs to.
	ClusterID *string
	// ControllerID contains the ID of the controller broker.
	ControllerID int32
	// Topics contains each topic in the response.
	Topics []*TopicMetadata
}

func (r *MetadataResponse) decode(pd packetDecoder, version int16) (err error) {
	r.Version = version
	if r.Version >= 3 {
		if r.ThrottleTimeMs, err = pd.getInt32(); err != nil {
			return err
		}
	}

	n, err := pd.getArrayLength()
	if err != nil {
		return err
	}
	r.Brokers = make([]*Broker, n)
	for i := 0; i < n; i++ {
		r.Brokers[i] = new(Broker)
		err = r.Brokers[i].decode(pd, version)
		if err != nil {
			return err
		}
	}

	if r.Version >= 2 {
		if r.ClusterID, err = pd.getNullableString(); err != nil {
			return err
		}
	}

	if r.Version >= 1 {
		if r.ControllerID, err = pd.getInt32(); err != nil {
			return err
		}
	}

	if numTopics, err := pd.getArrayLength(); err != nil {
		return err
	} else {
		r.Topics = make([]*TopicMetadata, numTopics)
		for i := 0; i < numTopics; i++ {
			block := &TopicMetadata{}
			if err := block.decode(pd, r.Version); err != nil {
				return err
			}
			r.Topics[i] = block
		}
	}

	return nil
}

func (r *MetadataResponse) encode(pe packetEncoder) (err error) {
	if r.Version >= 3 {
		pe.putInt32(r.ThrottleTimeMs)
	}

	if err := pe.putArrayLength(len(r.Brokers)); err != nil {
		return err
	}
	for _, broker := range r.Brokers {
		err = broker.encode(pe, r.Version)
		if err != nil {
			return err
		}
	}

	if r.Version >= 2 {
		if err := pe.putNullableString(r.ClusterID); err != nil {
			return err
		}
	}

	if r.Version >= 1 {
		pe.putInt32(r.ControllerID)
	}

	if err := pe.putArrayLength(len(r.Topics)); err != nil {
		return err
	}
	for _, block := range r.Topics {
		if err := block.encode(pe, r.Version); err != nil {
			return err
		}
	}

	return nil
}

func (r *MetadataResponse) key() int16 {
	return 3
}

func (r *MetadataResponse) version() int16 {
	return r.Version
}

func (r *MetadataResponse) headerVersion() int16 {
	return 0
}

func (r *MetadataResponse) requiredVersion() KafkaVersion {
	switch r.Version {
	case 1:
		return V0_10_0_0
	case 2:
		return V0_10_1_0
	case 3, 4:
		return V0_11_0_0
	case 5:
		return V1_0_0_0
	case 6:
		return V2_0_0_0
	case 7:
		return V2_1_0_0
	default:
		return MinVersion
	}
}

// testing API

func (r *MetadataResponse) AddBroker(addr string, id int32) {
	r.Brokers = append(r.Brokers, &Broker{id: id, addr: addr})
}

func (r *MetadataResponse) AddTopic(topic string, err KError) *TopicMetadata {
	var tmatch *TopicMetadata

	for _, tm := range r.Topics {
		if tm.Name == topic {
			tmatch = tm
			goto foundTopic
		}
	}

	tmatch = new(TopicMetadata)
	tmatch.Name = topic
	r.Topics = append(r.Topics, tmatch)

foundTopic:

	tmatch.Err = err
	return tmatch
}

func (r *MetadataResponse) AddTopicPartition(topic string, partition, brokerID int32, replicas, isr []int32, offline []int32, err KError) {
	tmatch := r.AddTopic(topic, ErrNoError)
	var pmatch *PartitionMetadata

	for _, pm := range tmatch.Partitions {
		if pm.ID == partition {
			pmatch = pm
			goto foundPartition
		}
	}

	pmatch = new(PartitionMetadata)
	pmatch.ID = partition
	tmatch.Partitions = append(tmatch.Partitions, pmatch)

foundPartition:
	pmatch.Leader = brokerID
	pmatch.Replicas = replicas
	pmatch.Isr = isr
	pmatch.OfflineReplicas = offline
	pmatch.Err = err
}
