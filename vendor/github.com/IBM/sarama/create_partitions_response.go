package sarama

import (
	"fmt"
	"time"
)

type CreatePartitionsResponse struct {
	Version              int16
	ThrottleTime         time.Duration
	TopicPartitionErrors map[string]*TopicPartitionError
}

func (c *CreatePartitionsResponse) setVersion(v int16) {
	c.Version = v
}

func (c *CreatePartitionsResponse) encode(pe packetEncoder) error {
	pe.putDurationMs(c.ThrottleTime)
	if err := pe.putArrayLength(len(c.TopicPartitionErrors)); err != nil {
		return err
	}

	for topic, partitionError := range c.TopicPartitionErrors {
		if err := pe.putString(topic); err != nil {
			return err
		}
		if err := partitionError.encode(pe); err != nil {
			return err
		}
	}

	pe.putEmptyTaggedFieldArray()
	return nil
}

func (c *CreatePartitionsResponse) decode(pd packetDecoder, version int16) (err error) {
	c.Version = version
	if c.ThrottleTime, err = pd.getDurationMs(); err != nil {
		return err
	}

	n, err := pd.getArrayLength()
	if err != nil {
		return err
	}
	if n < 0 {
		return errInvalidArrayLength
	}

	c.TopicPartitionErrors = make(map[string]*TopicPartitionError, n)
	for range n {
		topic, err := pd.getString()
		if err != nil {
			return err
		}
		c.TopicPartitionErrors[topic] = new(TopicPartitionError)
		if err := c.TopicPartitionErrors[topic].decode(pd, version); err != nil {
			return err
		}
	}

	_, err = pd.getEmptyTaggedFieldArray()
	return err
}

func (r *CreatePartitionsResponse) key() int16 {
	return apiKeyCreatePartitions
}

func (r *CreatePartitionsResponse) version() int16 {
	return r.Version
}

func (r *CreatePartitionsResponse) headerVersion() int16 {
	if r.Version >= 2 {
		return 1
	}
	return 0
}

func (r *CreatePartitionsResponse) isValidVersion() bool {
	return r.Version >= 0 && r.Version <= 3
}

func (r *CreatePartitionsResponse) isFlexible() bool {
	return r.isFlexibleVersion(r.Version)
}

func (r *CreatePartitionsResponse) isFlexibleVersion(version int16) bool {
	return version >= 2
}

func (r *CreatePartitionsResponse) requiredVersion() KafkaVersion {
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

func (r *CreatePartitionsResponse) throttleTime() time.Duration {
	return r.ThrottleTime
}

type TopicPartitionError struct {
	Err    KError
	ErrMsg *string
}

func (t *TopicPartitionError) Error() string {
	text := t.Err.Error()
	if t.ErrMsg != nil {
		text = fmt.Sprintf("%s - %s", text, *t.ErrMsg)
	}
	return text
}

func (t *TopicPartitionError) Unwrap() error {
	return t.Err
}

func (t *TopicPartitionError) encode(pe packetEncoder) error {
	pe.putKError(t.Err)

	if err := pe.putNullableString(t.ErrMsg); err != nil {
		return err
	}

	pe.putEmptyTaggedFieldArray()
	return nil
}

func (t *TopicPartitionError) decode(pd packetDecoder, version int16) (err error) {
	t.Err, err = pd.getKError()
	if err != nil {
		return err
	}

	if t.ErrMsg, err = pd.getNullableString(); err != nil {
		return err
	}

	_, err = pd.getEmptyTaggedFieldArray()
	return err
}
