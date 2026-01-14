package sarama

import (
	"fmt"
	"time"
)

type CreateTopicsResponse struct {
	// Version defines the protocol version to use for encode and decode
	Version int16
	// ThrottleTime contains the duration for which the request was throttled due
	// to a quota violation, or zero if the request did not violate any quota.
	ThrottleTime time.Duration
	// TopicErrors contains a map of any errors for the topics we tried to create.
	TopicErrors map[string]*TopicError
	// TopicResults contains a map of the results for the topics we tried to create.
	TopicResults map[string]*CreatableTopicResult
}

func (c *CreateTopicsResponse) setVersion(v int16) {
	c.Version = v
}

func (c *CreateTopicsResponse) encode(pe packetEncoder) error {
	if c.Version >= 2 {
		pe.putDurationMs(c.ThrottleTime)
	}

	if err := pe.putArrayLength(len(c.TopicErrors)); err != nil {
		return err
	}
	for topic, topicError := range c.TopicErrors {
		if err := pe.putString(topic); err != nil {
			return err
		}
		if err := topicError.encode(pe, c.Version); err != nil {
			return err
		}
		if c.Version >= 5 {
			result, ok := c.TopicResults[topic]
			if !ok {
				return fmt.Errorf("expected TopicResult for topic, %s, for V5 protocol", topic)
			}
			if err := result.encode(pe, c.Version); err != nil {
				return err
			}
		}
	}

	pe.putEmptyTaggedFieldArray()
	return nil
}

func (c *CreateTopicsResponse) decode(pd packetDecoder, version int16) (err error) {
	c.Version = version

	if version >= 2 {
		if c.ThrottleTime, err = pd.getDurationMs(); err != nil {
			return err
		}
	}

	n, err := pd.getArrayLength()
	if err != nil {
		return err
	}

	c.TopicErrors = make(map[string]*TopicError, n)
	if version >= 5 {
		c.TopicResults = make(map[string]*CreatableTopicResult, n)
	}
	for i := 0; i < n; i++ {
		topic, err := pd.getString()
		if err != nil {
			return err
		}
		c.TopicErrors[topic] = new(TopicError)
		if err := c.TopicErrors[topic].decode(pd, version); err != nil {
			return err
		}
		if version >= 5 {
			c.TopicResults[topic] = &CreatableTopicResult{}
			if err := c.TopicResults[topic].decode(pd, version); err != nil {
				return err
			}
		}
	}

	if _, err := pd.getEmptyTaggedFieldArray(); err != nil {
		return err
	}
	return nil
}

func (c *CreateTopicsResponse) key() int16 {
	return apiKeyCreateTopics
}

func (c *CreateTopicsResponse) version() int16 {
	return c.Version
}

func (c *CreateTopicsResponse) headerVersion() int16 {
	if c.Version >= 5 {
		return 1
	}
	return 0
}

func (c *CreateTopicsResponse) isFlexible() bool {
	return c.isFlexibleVersion(c.Version)
}

func (c *CreateTopicsResponse) isFlexibleVersion(version int16) bool {
	return version >= 5
}

func (c *CreateTopicsResponse) isValidVersion() bool {
	return c.Version >= 0 && c.Version <= 5
}

func (c *CreateTopicsResponse) requiredVersion() KafkaVersion {
	switch c.Version {
	case 5:
		return V2_4_0_0
	case 4:
		return V2_4_0_0
	case 3:
		return V2_0_0_0
	case 2:
		return V0_11_0_0
	case 1:
		return V0_10_2_0
	case 0:
		return V0_10_1_0
	default:
		return V2_8_0_0
	}
}

func (r *CreateTopicsResponse) throttleTime() time.Duration {
	return r.ThrottleTime
}

type TopicError struct {
	Err    KError
	ErrMsg *string
}

func (t *TopicError) Error() string {
	text := t.Err.Error()
	if t.ErrMsg != nil {
		text = fmt.Sprintf("%s - %s", text, *t.ErrMsg)
	}
	return text
}

func (t *TopicError) Unwrap() error {
	return t.Err
}

func (t *TopicError) encode(pe packetEncoder, version int16) error {
	pe.putKError(t.Err)

	if version >= 1 {
		if err := pe.putNullableString(t.ErrMsg); err != nil {
			return err
		}
	}

	return nil
}

func (t *TopicError) decode(pd packetDecoder, version int16) (err error) {
	t.Err, err = pd.getKError()
	if err != nil {
		return err
	}

	if version >= 1 {
		if t.ErrMsg, err = pd.getNullableString(); err != nil {
			return err
		}
	}

	return nil
}

// CreatableTopicResult struct {
type CreatableTopicResult struct {
	// TopicConfigErrorCode contains a Optional topic config error returned if configs are not returned in the response.
	TopicConfigErrorCode KError
	// NumPartitions contains a Number of partitions of the topic.
	NumPartitions int32
	// ReplicationFactor contains a Replication factor of the topic.
	ReplicationFactor int16
	// Configs contains a Configuration of the topic.
	Configs map[string]*CreatableTopicConfigs
}

func (r *CreatableTopicResult) encode(pe packetEncoder, version int16) error {
	pe.putInt32(r.NumPartitions)
	pe.putInt16(r.ReplicationFactor)

	if err := pe.putArrayLength(len(r.Configs)); err != nil {
		return err
	}
	for name, config := range r.Configs {
		if err := pe.putString(name); err != nil {
			return err
		}
		if err := config.encode(pe, version); err != nil {
			return err
		}
	}
	if r.TopicConfigErrorCode == ErrNoError {
		pe.putEmptyTaggedFieldArray()
		return nil
	}

	// TODO: refactor to helper for tagged fields
	pe.putUVarint(1) // number of tagged fields

	pe.putUVarint(0) // tag

	pe.putUVarint(2) // value length

	pe.putKError(r.TopicConfigErrorCode) // tag value

	return nil
}

func (r *CreatableTopicResult) decode(pd packetDecoder, version int16) (err error) {
	r.NumPartitions, err = pd.getInt32()
	if err != nil {
		return err
	}

	r.ReplicationFactor, err = pd.getInt16()
	if err != nil {
		return err
	}

	n, err := pd.getArrayLength()
	if err != nil {
		return err
	}
	r.Configs = make(map[string]*CreatableTopicConfigs, n)
	for i := 0; i < n; i++ {
		name, err := pd.getString()
		if err != nil {
			return err
		}
		r.Configs[name] = &CreatableTopicConfigs{}
		if err := r.Configs[name].decode(pd, version); err != nil {
			return err
		}
	}
	err = pd.getTaggedFieldArray(taggedFieldDecoders{
		0: func(pd packetDecoder) error {
			r.TopicConfigErrorCode, err = pd.getKError()
			if err != nil {
				return err
			}
			return nil
		},
	})
	if err != nil {
		return err
	}
	return nil
}

// CreatableTopicConfigs contains a Configuration of the topic.
type CreatableTopicConfigs struct {
	// Value contains the configuration value.
	Value *string
	// ReadOnly contains a True if the configuration is read-only.
	ReadOnly bool
	// ConfigSource contains the configuration source.
	ConfigSource ConfigSource
	// IsSensitive contains a True if this configuration is sensitive.
	IsSensitive bool
}

func (c *CreatableTopicConfigs) encode(pe packetEncoder, version int16) (err error) {
	if err = pe.putNullableString(c.Value); err != nil {
		return err
	}
	pe.putBool(c.ReadOnly)
	pe.putInt8(int8(c.ConfigSource))
	pe.putBool(c.IsSensitive)
	pe.putEmptyTaggedFieldArray()
	return nil
}

func (c *CreatableTopicConfigs) decode(pd packetDecoder, version int16) (err error) {
	c.Value, err = pd.getNullableString()
	if err != nil {
		return err
	}
	c.ReadOnly, err = pd.getBool()
	if err != nil {
		return err
	}
	source, err := pd.getInt8()
	if err != nil {
		return err
	}
	c.ConfigSource = ConfigSource(source)
	c.IsSensitive, err = pd.getBool()
	if err != nil {
		return err
	}

	if _, err := pd.getEmptyTaggedFieldArray(); err != nil {
		return err
	}
	return nil
}
