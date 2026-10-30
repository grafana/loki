package sarama

import "time"

type CreatePartitionsRequest struct {
	Version         int16
	TopicPartitions map[string]*TopicPartition
	Timeout         time.Duration
	ValidateOnly    bool
}

func (c *CreatePartitionsRequest) setVersion(v int16) {
	c.Version = v
}

func (c *CreatePartitionsRequest) encode(pe packetEncoder) error {
	if err := pe.putArrayLength(len(c.TopicPartitions)); err != nil {
		return err
	}

	for topic, partition := range c.TopicPartitions {
		if err := pe.putString(topic); err != nil {
			return err
		}
		if err := partition.encode(pe); err != nil {
			return err
		}
	}

	pe.putInt32(int32(c.Timeout / time.Millisecond))

	pe.putBool(c.ValidateOnly)

	pe.putEmptyTaggedFieldArray()
	return nil
}

func (c *CreatePartitionsRequest) decode(pd packetDecoder, version int16) (err error) {
	n, err := pd.getArrayLength()
	if err != nil {
		return err
	}
	if n < 0 {
		return errInvalidArrayLength
	}
	c.TopicPartitions = make(map[string]*TopicPartition, n)
	for range n {
		topic, err := pd.getString()
		if err != nil {
			return err
		}
		c.TopicPartitions[topic] = new(TopicPartition)
		if err := c.TopicPartitions[topic].decode(pd, version); err != nil {
			return err
		}
	}

	timeout, err := pd.getInt32()
	if err != nil {
		return err
	}
	c.Timeout = time.Duration(timeout) * time.Millisecond

	if c.ValidateOnly, err = pd.getBool(); err != nil {
		return err
	}

	_, err = pd.getEmptyTaggedFieldArray()
	return err
}

func (r *CreatePartitionsRequest) key() int16 {
	return apiKeyCreatePartitions
}

func (r *CreatePartitionsRequest) version() int16 {
	return r.Version
}

func (r *CreatePartitionsRequest) headerVersion() int16 {
	if r.Version >= 2 {
		return 2
	}
	return 1
}

func (r *CreatePartitionsRequest) isValidVersion() bool {
	return r.Version >= 0 && r.Version <= 3
}

func (r *CreatePartitionsRequest) isFlexible() bool {
	return r.isFlexibleVersion(r.Version)
}

func (r *CreatePartitionsRequest) isFlexibleVersion(version int16) bool {
	return version >= 2
}

func (r *CreatePartitionsRequest) requiredVersion() KafkaVersion {
	switch r.Version {
	case 3:
		return V2_7_0_0
	case 2:
		return V2_5_0_0
	case 1:
		return V2_0_0_0
	case 0:
		return V1_0_0_0
	default:
		return V2_0_0_0
	}
}

type TopicPartition struct {
	Count      int32
	Assignment [][]int32
}

func (t *TopicPartition) encode(pe packetEncoder) error {
	pe.putInt32(t.Count)

	if len(t.Assignment) == 0 {
		if err := pe.putArrayLength(-1); err != nil {
			return err
		}
		pe.putEmptyTaggedFieldArray()
		return nil
	}

	if err := pe.putArrayLength(len(t.Assignment)); err != nil {
		return err
	}

	for _, assign := range t.Assignment {
		if err := pe.putInt32Array(assign); err != nil {
			return err
		}
		pe.putEmptyTaggedFieldArray()
	}

	pe.putEmptyTaggedFieldArray()
	return nil
}

func (t *TopicPartition) decode(pd packetDecoder, version int16) (err error) {
	if t.Count, err = pd.getInt32(); err != nil {
		return err
	}

	n, err := pd.getArrayLength()
	if err != nil {
		return err
	}
	if n <= 0 {
		_, err = pd.getEmptyTaggedFieldArray()
		return err
	}
	t.Assignment = make([][]int32, n)

	for i := range n {
		if t.Assignment[i], err = pd.getInt32Array(); err != nil {
			return err
		}
		_, err = pd.getEmptyTaggedFieldArray()
		if err != nil {
			return err
		}
	}

	_, err = pd.getEmptyTaggedFieldArray()
	return err
}
