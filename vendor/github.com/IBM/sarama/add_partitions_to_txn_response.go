package sarama

import (
	"time"
)

// AddPartitionsToTxnResponse is a partition errors to transaction type
type AddPartitionsToTxnResponse struct {
	Version      int16
	ThrottleTime time.Duration
	Errors       map[string][]*PartitionError
}

func (a *AddPartitionsToTxnResponse) setVersion(v int16) {
	a.Version = v
}

func (a *AddPartitionsToTxnResponse) encode(pe packetEncoder) error {
	pe.putDurationMs(a.ThrottleTime)
	if err := pe.putArrayLength(len(a.Errors)); err != nil {
		return err
	}

	for topic, e := range a.Errors {
		if err := pe.putString(topic); err != nil {
			return err
		}
		if err := pe.putArrayLength(len(e)); err != nil {
			return err
		}
		for _, partitionError := range e {
			if err := partitionError.encode(pe); err != nil {
				return err
			}
		}
		pe.putEmptyTaggedFieldArray()
	}

	pe.putEmptyTaggedFieldArray()
	return nil
}

func (a *AddPartitionsToTxnResponse) decode(pd packetDecoder, version int16) (err error) {
	a.Version = version
	if a.ThrottleTime, err = pd.getDurationMs(); err != nil {
		return err
	}

	n, err := pd.getArrayLength()
	if err != nil {
		return err
	}
	if n < 0 {
		return errInvalidArrayLength
	}

	a.Errors = make(map[string][]*PartitionError)

	for range n {
		topic, err := pd.getString()
		if err != nil {
			return err
		}

		m, err := pd.getArrayLength()
		if err != nil {
			return err
		}
		if m < 0 {
			return errInvalidArrayLength
		}

		a.Errors[topic] = make([]*PartitionError, m)

		for j := range m {
			a.Errors[topic][j] = new(PartitionError)
			if err := a.Errors[topic][j].decode(pd, version); err != nil {
				return err
			}
		}

		if _, err = pd.getEmptyTaggedFieldArray(); err != nil {
			return err
		}
	}

	_, err = pd.getEmptyTaggedFieldArray()
	return err
}

func (a *AddPartitionsToTxnResponse) key() int16 {
	return apiKeyAddPartitionsToTxn
}

func (a *AddPartitionsToTxnResponse) version() int16 {
	return a.Version
}

func (a *AddPartitionsToTxnResponse) headerVersion() int16 {
	if a.Version >= 3 {
		return 1
	}
	return 0
}

func (a *AddPartitionsToTxnResponse) isValidVersion() bool {
	return a.Version >= 0 && a.Version <= 3
}

func (a *AddPartitionsToTxnResponse) isFlexible() bool {
	return a.isFlexibleVersion(a.Version)
}

func (a *AddPartitionsToTxnResponse) isFlexibleVersion(version int16) bool {
	return version >= 3
}

func (a *AddPartitionsToTxnResponse) requiredVersion() KafkaVersion {
	switch a.Version {
	case 3:
		return V2_8_0_0
	case 2:
		return V2_7_0_0
	case 1:
		return V2_0_0_0
	default:
		return V0_11_0_0
	}
}

func (r *AddPartitionsToTxnResponse) throttleTime() time.Duration {
	return r.ThrottleTime
}

// PartitionError is a partition error type
type PartitionError struct {
	Partition int32
	Err       KError
}

func (p *PartitionError) encode(pe packetEncoder) error {
	pe.putInt32(p.Partition)
	pe.putKError(p.Err)
	pe.putEmptyTaggedFieldArray()
	return nil
}

func (p *PartitionError) decode(pd packetDecoder, version int16) (err error) {
	if p.Partition, err = pd.getInt32(); err != nil {
		return err
	}

	p.Err, err = pd.getKError()
	if err != nil {
		return err
	}

	_, err = pd.getEmptyTaggedFieldArray()
	return err
}
