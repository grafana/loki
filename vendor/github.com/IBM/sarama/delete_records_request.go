package sarama

import (
	"slices"
	"sort"
	"time"
)

// request message format is:
// [topic] timeout(int32)
// where topic is:
//  name(string) [partition]
// where partition is:
//  id(int32) offset(int64)

type DeleteRecordsRequest struct {
	Version int16
	Topics  map[string]*DeleteRecordsRequestTopic
	Timeout time.Duration
}

func (d *DeleteRecordsRequest) setVersion(v int16) {
	d.Version = v
}

func (d *DeleteRecordsRequest) encode(pe packetEncoder) error {
	if err := pe.putArrayLength(len(d.Topics)); err != nil {
		return err
	}
	keys := make([]string, 0, len(d.Topics))
	for topic := range d.Topics {
		keys = append(keys, topic)
	}
	sort.Strings(keys)
	for _, topic := range keys {
		if err := pe.putString(topic); err != nil {
			return err
		}
		if err := d.Topics[topic].encode(pe); err != nil {
			return err
		}
	}
	pe.putInt32(int32(d.Timeout / time.Millisecond))

	pe.putEmptyTaggedFieldArray()

	return nil
}

func (d *DeleteRecordsRequest) decode(pd packetDecoder, version int16) error {
	n, err := pd.getArrayLength()
	if err != nil {
		return err
	}
	if n < 0 {
		return errInvalidArrayLength
	}

	if n > 0 {
		d.Topics = make(map[string]*DeleteRecordsRequestTopic, n)
		for range n {
			topic, err := pd.getString()
			if err != nil {
				return err
			}
			details := new(DeleteRecordsRequestTopic)
			if err = details.decode(pd, version); err != nil {
				return err
			}
			d.Topics[topic] = details
		}
	}

	timeout, err := pd.getInt32()
	if err != nil {
		return err
	}
	d.Timeout = time.Duration(timeout) * time.Millisecond

	if _, err := pd.getEmptyTaggedFieldArray(); err != nil {
		return err
	}

	return nil
}

func (d *DeleteRecordsRequest) key() int16 {
	return apiKeyDeleteRecords
}

func (d *DeleteRecordsRequest) version() int16 {
	return d.Version
}

func (d *DeleteRecordsRequest) headerVersion() int16 {
	if d.Version >= 2 {
		return 2
	}
	return 1
}

func (d *DeleteRecordsRequest) isValidVersion() bool {
	return d.Version >= 0 && d.Version <= 2
}

func (d *DeleteRecordsRequest) requiredVersion() KafkaVersion {
	switch d.Version {
	case 2:
		return V2_6_0_0
	case 1:
		return V2_0_0_0
	default:
		return V0_11_0_0
	}
}

func (d *DeleteRecordsRequest) isFlexible() bool {
	return d.isFlexibleVersion(d.Version)
}

func (d *DeleteRecordsRequest) isFlexibleVersion(version int16) bool {
	return version >= 2
}

type DeleteRecordsRequestTopic struct {
	PartitionOffsets map[int32]int64 // partition => offset
}

func (t *DeleteRecordsRequestTopic) encode(pe packetEncoder) error {
	if err := pe.putArrayLength(len(t.PartitionOffsets)); err != nil {
		return err
	}
	keys := make([]int32, 0, len(t.PartitionOffsets))
	for partition := range t.PartitionOffsets {
		keys = append(keys, partition)
	}
	slices.Sort(keys)
	for _, partition := range keys {
		pe.putInt32(partition)
		pe.putInt64(t.PartitionOffsets[partition])
		pe.putEmptyTaggedFieldArray()
	}
	pe.putEmptyTaggedFieldArray()
	return nil
}

func (t *DeleteRecordsRequestTopic) decode(pd packetDecoder, version int16) error {
	n, err := pd.getArrayLength()
	if err != nil {
		return err
	}
	if n < 0 {
		return errInvalidArrayLength
	}

	if n > 0 {
		t.PartitionOffsets = make(map[int32]int64, n)
		for range n {
			partition, err := pd.getInt32()
			if err != nil {
				return err
			}
			offset, err := pd.getInt64()
			if err != nil {
				return err
			}
			t.PartitionOffsets[partition] = offset
			if _, err := pd.getEmptyTaggedFieldArray(); err != nil {
				return err
			}
		}
	}

	if _, err := pd.getEmptyTaggedFieldArray(); err != nil {
		return err
	}

	return nil
}
