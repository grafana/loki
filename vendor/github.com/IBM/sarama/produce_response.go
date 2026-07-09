package sarama

import (
	"fmt"
	"time"
)

// Protocol, http://kafka.apache.org/protocol.html
// v1
// v2 = v3 = v4
// v5 = v6 = v7
// Produce Response (Version: 7) => [responses] throttle_time_ms
//   responses => topic [partition_responses]
//     topic => STRING
//     partition_responses => partition error_code base_offset log_append_time log_start_offset
//       partition => INT32
//       error_code => INT16
//       base_offset => INT64
//       log_append_time => INT64
//       log_start_offset => INT64
//   throttle_time_ms => INT32

// partition_responses in protocol
type ProduceResponseBlock struct {
	Err          KError                       // v0, error_code
	Offset       int64                        // v0, base_offset
	Timestamp    time.Time                    // v2, log_append_time, and the broker is configured with `LogAppendTime`
	StartOffset  int64                        // v5, log_start_offset
	RecordErrors []ProduceResponseRecordError // v8, record_errors (KIP-467)
	ErrorMessage *string                      // v8, error_message (KIP-467)
}

// ProduceResponseRecordError identifies a record within a produced batch that
// caused the whole batch to be dropped, along with the per-record error
// message. Added in Produce response v8 (KIP-467).
type ProduceResponseRecordError struct {
	BatchIndex             int32   // v8, batch_index
	BatchIndexErrorMessage *string // v8, batch_index_error_message (nullable)
}

func (p *ProduceResponseRecordError) encode(pe packetEncoder) error {
	pe.putInt32(p.BatchIndex)
	if err := pe.putNullableString(p.BatchIndexErrorMessage); err != nil {
		return err
	}
	pe.putEmptyTaggedFieldArray()
	return nil
}

func (p *ProduceResponseRecordError) decode(pd packetDecoder) (err error) {
	if p.BatchIndex, err = pd.getInt32(); err != nil {
		return err
	}
	if p.BatchIndexErrorMessage, err = pd.getNullableString(); err != nil {
		return err
	}
	if _, err := pd.getEmptyTaggedFieldArray(); err != nil {
		return err
	}
	return nil
}

func (b *ProduceResponseBlock) decode(pd packetDecoder, version int16) (err error) {
	b.Err, err = pd.getKError()
	if err != nil {
		return err
	}

	b.Offset, err = pd.getInt64()
	if err != nil {
		return err
	}

	if version >= 2 {
		if millis, err := pd.getInt64(); err != nil {
			return err
		} else if millis != -1 {
			b.Timestamp = time.Unix(millis/1000, (millis%1000)*int64(time.Millisecond))
		}
	}

	if version >= 5 {
		b.StartOffset, err = pd.getInt64()
		if err != nil {
			return err
		}
	}

	if version >= 8 {
		numRecordErrors, err := pd.getArrayLength()
		if err != nil {
			return err
		}
		if numRecordErrors < 0 {
			return errInvalidArrayLength
		}
		if numRecordErrors > 0 {
			b.RecordErrors = make([]ProduceResponseRecordError, numRecordErrors)
			for i := range b.RecordErrors {
				if err := b.RecordErrors[i].decode(pd); err != nil {
					return err
				}
			}
		}
		if b.ErrorMessage, err = pd.getNullableString(); err != nil {
			return err
		}
	}

	if _, err := pd.getEmptyTaggedFieldArray(); err != nil {
		return err
	}

	return nil
}

func (b *ProduceResponseBlock) encode(pe packetEncoder, version int16) (err error) {
	pe.putKError(b.Err)
	pe.putInt64(b.Offset)

	if version >= 2 {
		timestamp := int64(-1)
		if !b.Timestamp.Before(time.Unix(0, 0)) {
			timestamp = b.Timestamp.UnixNano() / int64(time.Millisecond)
		} else if !b.Timestamp.IsZero() {
			return PacketEncodingError{fmt.Sprintf("invalid timestamp (%v)", b.Timestamp)}
		}
		pe.putInt64(timestamp)
	}

	if version >= 5 {
		pe.putInt64(b.StartOffset)
	}

	if version >= 8 {
		if err = pe.putArrayLength(len(b.RecordErrors)); err != nil {
			return err
		}
		for i := range b.RecordErrors {
			if err = b.RecordErrors[i].encode(pe); err != nil {
				return err
			}
		}
		if err = pe.putNullableString(b.ErrorMessage); err != nil {
			return err
		}
	}

	pe.putEmptyTaggedFieldArray()

	return nil
}

type ProduceResponse struct {
	Blocks       map[string]map[int32]*ProduceResponseBlock // v0, responses
	Version      int16
	ThrottleTime time.Duration // v1, throttle_time_ms
}

func (r *ProduceResponse) setVersion(v int16) {
	r.Version = v
}

func (r *ProduceResponse) decode(pd packetDecoder, version int16) (err error) {
	r.Version = version

	numTopics, err := pd.getArrayLength()
	if err != nil {
		return err
	}
	if numTopics < 0 {
		return errInvalidArrayLength
	}

	r.Blocks = make(map[string]map[int32]*ProduceResponseBlock, numTopics)
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

		r.Blocks[name] = make(map[int32]*ProduceResponseBlock, numBlocks)

		for range numBlocks {
			id, err := pd.getInt32()
			if err != nil {
				return err
			}

			block := new(ProduceResponseBlock)
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

	if r.Version >= 1 {
		if r.ThrottleTime, err = pd.getDurationMs(); err != nil {
			return err
		}
	}

	_, err = pd.getEmptyTaggedFieldArray()
	return err
}

func (r *ProduceResponse) encode(pe packetEncoder) error {
	err := pe.putArrayLength(len(r.Blocks))
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
		for id, prb := range partitions {
			pe.putInt32(id)
			err = prb.encode(pe, r.Version)
			if err != nil {
				return err
			}
		}
		pe.putEmptyTaggedFieldArray()
	}

	if r.Version >= 1 {
		pe.putDurationMs(r.ThrottleTime)
	}
	pe.putEmptyTaggedFieldArray()
	return nil
}

func (r *ProduceResponse) key() int16 {
	return apiKeyProduce
}

func (r *ProduceResponse) version() int16 {
	return r.Version
}

func (r *ProduceResponse) headerVersion() int16 {
	if r.Version >= 9 {
		return 1
	}
	return 0
}

func (r *ProduceResponse) isFlexible() bool {
	return r.isFlexibleVersion(r.Version)
}

func (r *ProduceResponse) isFlexibleVersion(version int16) bool {
	return version >= 9
}

func (r *ProduceResponse) isValidVersion() bool {
	return r.Version >= 0 && r.Version <= 9
}

func (r *ProduceResponse) requiredVersion() KafkaVersion {
	switch r.Version {
	case 9:
		return V2_8_0_0
	case 8:
		return V2_4_0_0
	case 7:
		return V2_1_0_0
	case 6:
		return V2_0_0_0
	case 4, 5:
		return V1_0_0_0
	case 3:
		return V0_11_0_0
	case 2:
		return V0_10_0_0
	case 1:
		return V0_9_0_0
	case 0:
		return V0_8_2_0
	default:
		return V2_1_0_0
	}
}

func (r *ProduceResponse) throttleTime() time.Duration {
	return r.ThrottleTime
}

func (r *ProduceResponse) GetBlock(topic string, partition int32) *ProduceResponseBlock {
	if r.Blocks == nil {
		return nil
	}

	if r.Blocks[topic] == nil {
		return nil
	}

	return r.Blocks[topic][partition]
}

// Testing API

func (r *ProduceResponse) AddTopicPartition(topic string, partition int32, err KError) {
	if r.Blocks == nil {
		r.Blocks = make(map[string]map[int32]*ProduceResponseBlock)
	}
	byTopic, ok := r.Blocks[topic]
	if !ok {
		byTopic = make(map[int32]*ProduceResponseBlock)
		r.Blocks[topic] = byTopic
	}
	block := &ProduceResponseBlock{
		Err: err,
	}
	if r.Version >= 2 {
		block.Timestamp = time.Now()
	}
	byTopic[partition] = block
}
