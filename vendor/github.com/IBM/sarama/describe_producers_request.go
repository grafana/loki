package sarama

type DescribeProducersRequest struct {
	Version int16

	// Topics is the set of topics (and their partitions) to list producers for
	Topics []DescribeProducersRequestTopic
}

func (r *DescribeProducersRequest) setVersion(v int16) {
	r.Version = v
}

type DescribeProducersRequestTopic struct {
	// Name is the topic name
	Name string

	// PartitionIndexes is the indexes of the partitions to list producers for
	PartitionIndexes []int32
}

func (t *DescribeProducersRequestTopic) encode(pe packetEncoder) error {
	if err := pe.putString(t.Name); err != nil {
		return err
	}

	if err := pe.putInt32Array(t.PartitionIndexes); err != nil {
		return err
	}

	pe.putEmptyTaggedFieldArray()
	return nil
}

func (t *DescribeProducersRequestTopic) decode(pd packetDecoder, version int16) (err error) {
	if t.Name, err = pd.getString(); err != nil {
		return err
	}

	if t.PartitionIndexes, err = pd.getInt32Array(); err != nil {
		return err
	}

	_, err = pd.getEmptyTaggedFieldArray()
	return err
}

func (r *DescribeProducersRequest) encode(pe packetEncoder) error {
	if err := pe.putArrayLength(len(r.Topics)); err != nil {
		return err
	}
	for i := range r.Topics {
		if err := r.Topics[i].encode(pe); err != nil {
			return err
		}
	}

	pe.putEmptyTaggedFieldArray()
	return nil
}

func (r *DescribeProducersRequest) decode(pd packetDecoder, version int16) (err error) {
	n, err := pd.getArrayLength()
	if err != nil {
		return err
	}
	if n < 0 {
		return errInvalidArrayLength
	}

	r.Topics = make([]DescribeProducersRequestTopic, n)
	for i := range n {
		if err := r.Topics[i].decode(pd, version); err != nil {
			return err
		}
	}

	_, err = pd.getEmptyTaggedFieldArray()
	return err
}

func (r *DescribeProducersRequest) key() int16 {
	return apiKeyDescribeProducers
}

func (r *DescribeProducersRequest) version() int16 {
	return r.Version
}

func (r *DescribeProducersRequest) headerVersion() int16 {
	return 2
}

func (r *DescribeProducersRequest) isValidVersion() bool {
	return r.Version == 0
}

func (r *DescribeProducersRequest) isFlexible() bool {
	return r.isFlexibleVersion(r.Version)
}

func (r *DescribeProducersRequest) isFlexibleVersion(version int16) bool {
	return version >= 0
}

func (r *DescribeProducersRequest) requiredVersion() KafkaVersion {
	return V2_8_0_0
}
