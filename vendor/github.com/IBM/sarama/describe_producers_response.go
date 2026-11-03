package sarama

import "time"

type DescribeProducersResponse struct {
	Version int16

	ThrottleTime time.Duration

	// Topics contains the per-topic results
	Topics []DescribeProducersResponseTopic
}

func (r *DescribeProducersResponse) setVersion(v int16) {
	r.Version = v
}

type DescribeProducersResponseTopic struct {
	// Name is the topic name
	Name string

	// Partitions contains the per-partition results
	Partitions []DescribeProducersResponsePartition
}

type DescribeProducersResponsePartition struct {
	// PartitionIndex is the partition index
	PartitionIndex int32

	ErrorCode    KError
	ErrorMessage *string

	// ActiveProducers is the set of active producers for the partition
	ActiveProducers []ProducerState
}

type ProducerState struct {
	// ProducerID is the producer id
	ProducerID int64

	// ProducerEpoch is the producer epoch
	ProducerEpoch int32

	// LastSequence is the last sequence number sent by the producer, or -1
	LastSequence int32

	// LastTimestamp is the last timestamp sent by the producer, or -1
	LastTimestamp int64

	// CoordinatorEpoch is the current epoch of the producer group
	CoordinatorEpoch int32

	// CurrentTxnStartOffset is the current transaction start offset of the
	// producer, or -1
	CurrentTxnStartOffset int64
}

func (p *ProducerState) encode(pe packetEncoder) error {
	pe.putInt64(p.ProducerID)
	pe.putInt32(p.ProducerEpoch)
	pe.putInt32(p.LastSequence)
	pe.putInt64(p.LastTimestamp)
	pe.putInt32(p.CoordinatorEpoch)
	pe.putInt64(p.CurrentTxnStartOffset)

	pe.putEmptyTaggedFieldArray()
	return nil
}

func (p *ProducerState) decode(pd packetDecoder, version int16) (err error) {
	if p.ProducerID, err = pd.getInt64(); err != nil {
		return err
	}

	if p.ProducerEpoch, err = pd.getInt32(); err != nil {
		return err
	}

	if p.LastSequence, err = pd.getInt32(); err != nil {
		return err
	}

	if p.LastTimestamp, err = pd.getInt64(); err != nil {
		return err
	}

	if p.CoordinatorEpoch, err = pd.getInt32(); err != nil {
		return err
	}

	if p.CurrentTxnStartOffset, err = pd.getInt64(); err != nil {
		return err
	}

	_, err = pd.getEmptyTaggedFieldArray()
	return err
}

func (p *DescribeProducersResponsePartition) encode(pe packetEncoder) error {
	pe.putInt32(p.PartitionIndex)

	pe.putKError(p.ErrorCode)

	if err := pe.putNullableString(p.ErrorMessage); err != nil {
		return err
	}

	if err := pe.putArrayLength(len(p.ActiveProducers)); err != nil {
		return err
	}
	for i := range p.ActiveProducers {
		if err := p.ActiveProducers[i].encode(pe); err != nil {
			return err
		}
	}

	pe.putEmptyTaggedFieldArray()
	return nil
}

func (p *DescribeProducersResponsePartition) decode(pd packetDecoder, version int16) (err error) {
	if p.PartitionIndex, err = pd.getInt32(); err != nil {
		return err
	}

	if p.ErrorCode, err = pd.getKError(); err != nil {
		return err
	}

	if p.ErrorMessage, err = pd.getNullableString(); err != nil {
		return err
	}

	n, err := pd.getArrayLength()
	if err != nil {
		return err
	}
	if n < 0 {
		return errInvalidArrayLength
	}

	p.ActiveProducers = make([]ProducerState, n)
	for i := range n {
		if err := p.ActiveProducers[i].decode(pd, version); err != nil {
			return err
		}
	}

	_, err = pd.getEmptyTaggedFieldArray()
	return err
}

func (t *DescribeProducersResponseTopic) encode(pe packetEncoder) error {
	if err := pe.putString(t.Name); err != nil {
		return err
	}

	if err := pe.putArrayLength(len(t.Partitions)); err != nil {
		return err
	}
	for i := range t.Partitions {
		if err := t.Partitions[i].encode(pe); err != nil {
			return err
		}
	}

	pe.putEmptyTaggedFieldArray()
	return nil
}

func (t *DescribeProducersResponseTopic) decode(pd packetDecoder, version int16) (err error) {
	if t.Name, err = pd.getString(); err != nil {
		return err
	}

	n, err := pd.getArrayLength()
	if err != nil {
		return err
	}
	if n < 0 {
		return errInvalidArrayLength
	}

	t.Partitions = make([]DescribeProducersResponsePartition, n)
	for i := range n {
		if err := t.Partitions[i].decode(pd, version); err != nil {
			return err
		}
	}

	_, err = pd.getEmptyTaggedFieldArray()
	return err
}

func (r *DescribeProducersResponse) encode(pe packetEncoder) error {
	pe.putDurationMs(r.ThrottleTime)

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

func (r *DescribeProducersResponse) decode(pd packetDecoder, version int16) (err error) {
	if r.ThrottleTime, err = pd.getDurationMs(); err != nil {
		return err
	}

	n, err := pd.getArrayLength()
	if err != nil {
		return err
	}
	if n < 0 {
		return errInvalidArrayLength
	}

	r.Topics = make([]DescribeProducersResponseTopic, n)
	for i := range n {
		if err := r.Topics[i].decode(pd, version); err != nil {
			return err
		}
	}

	_, err = pd.getEmptyTaggedFieldArray()
	return err
}

func (r *DescribeProducersResponse) key() int16 {
	return apiKeyDescribeProducers
}

func (r *DescribeProducersResponse) version() int16 {
	return r.Version
}

func (r *DescribeProducersResponse) headerVersion() int16 {
	return 1
}

func (r *DescribeProducersResponse) isValidVersion() bool {
	return r.Version == 0
}

func (r *DescribeProducersResponse) isFlexible() bool {
	return r.isFlexibleVersion(r.Version)
}

func (r *DescribeProducersResponse) isFlexibleVersion(version int16) bool {
	return version >= 0
}

func (r *DescribeProducersResponse) requiredVersion() KafkaVersion {
	return V2_8_0_0
}

func (r *DescribeProducersResponse) throttleTime() time.Duration {
	return r.ThrottleTime
}
