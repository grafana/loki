package sarama

import (
	"time"
)

type TxnOffsetCommitResponse struct {
	Version      int16
	ThrottleTime time.Duration
	Topics       map[string][]*PartitionError
}

func (t *TxnOffsetCommitResponse) setVersion(v int16) {
	t.Version = v
}

func (t *TxnOffsetCommitResponse) encode(pe packetEncoder) error {
	pe.putDurationMs(t.ThrottleTime)
	if err := pe.putArrayLength(len(t.Topics)); err != nil {
		return err
	}

	for topic, e := range t.Topics {
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

func (t *TxnOffsetCommitResponse) decode(pd packetDecoder, version int16) (err error) {
	t.Version = version
	throttleTime, err := pd.getInt32()
	if err != nil {
		return err
	}
	t.ThrottleTime = time.Duration(throttleTime) * time.Millisecond

	n, err := pd.getArrayLength()
	if err != nil {
		return err
	}
	if n < 0 {
		return errInvalidArrayLength
	}

	t.Topics = make(map[string][]*PartitionError)

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

		t.Topics[topic] = make([]*PartitionError, m)

		for j := range m {
			t.Topics[topic][j] = new(PartitionError)
			if err := t.Topics[topic][j].decode(pd, version); err != nil {
				return err
			}
		}

		if _, err := pd.getEmptyTaggedFieldArray(); err != nil {
			return err
		}
	}

	if _, err := pd.getEmptyTaggedFieldArray(); err != nil {
		return err
	}

	return nil
}

func (a *TxnOffsetCommitResponse) key() int16 {
	return apiKeyTxnOffsetCommit
}

func (a *TxnOffsetCommitResponse) version() int16 {
	return a.Version
}

func (a *TxnOffsetCommitResponse) headerVersion() int16 {
	if a.Version >= 3 {
		return 1
	}
	return 0
}

func (a *TxnOffsetCommitResponse) isValidVersion() bool {
	return a.Version >= 0 && a.Version <= 3
}

func (a *TxnOffsetCommitResponse) isFlexible() bool {
	return a.isFlexibleVersion(a.Version)
}

func (a *TxnOffsetCommitResponse) isFlexibleVersion(version int16) bool {
	return version >= 3
}

func (a *TxnOffsetCommitResponse) requiredVersion() KafkaVersion {
	switch a.Version {
	case 3:
		return V2_5_0_0
	case 2:
		return V2_1_0_0
	case 1:
		return V2_0_0_0
	case 0:
		return V0_11_0_0
	default:
		return V2_5_0_0
	}
}

func (r *TxnOffsetCommitResponse) throttleTime() time.Duration {
	return r.ThrottleTime
}
