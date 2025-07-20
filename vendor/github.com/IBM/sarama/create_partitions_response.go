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

func (c *CreatePartitionsResponse) encode(pe packetEncoder) error {
	pe.putInt32(int32(c.ThrottleTime / time.Millisecond))
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

	return nil
}

func (c *CreatePartitionsResponse) decode(pd packetDecoder, version int16) (err error) {
	throttleTime, err := pd.getInt32()
	if err != nil {
		return err
	}
	c.ThrottleTime = time.Duration(throttleTime) * time.Millisecond

	n, err := pd.getArrayLength()
	if err != nil {
		return err
	}

	c.TopicPartitionErrors = make(map[string]*TopicPartitionError, n)
	for i := 0; i < n; i++ {
		topic, err := pd.getString()
		if err != nil {
			return err
		}
		c.TopicPartitionErrors[topic] = new(TopicPartitionError)
		if err := c.TopicPartitionErrors[topic].decode(pd, version); err != nil {
			return err
		}
	}

	return nil
}

func (r *CreatePartitionsResponse) key() int16 {
	return 37
}

func (r *CreatePartitionsResponse) version() int16 {
	return r.Version
}

func (r *CreatePartitionsResponse) headerVersion() int16 {
	return 0
}

func (r *CreatePartitionsResponse) isValidVersion() bool {
	return r.Version >= 0 && r.Version <= 1
}

func (r *CreatePartitionsResponse) requiredVersion() KafkaVersion {
	switch r.Version {
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
	pe.putInt16(int16(t.Err))

	if err := pe.putNullableString(t.ErrMsg); err != nil {
		return err
	}

	return nil
}

func (t *TopicPartitionError) decode(pd packetDecoder, version int16) (err error) {
	kerr, err := pd.getInt16()
	if err != nil {
		return err
	}
	t.Err = KError(kerr)

	if t.ErrMsg, err = pd.getNullableString(); err != nil {
		return err
	}

	return nil
}
