package sarama

import "time"

type InitProducerIDRequest struct {
	Version            int16
	TransactionalID    *string
	TransactionTimeout time.Duration
	ProducerID         int64
	ProducerEpoch      int16
}

func (i *InitProducerIDRequest) setVersion(v int16) {
	i.Version = v
}

func (i *InitProducerIDRequest) encode(pe packetEncoder) error {
	if err := pe.putNullableString(i.TransactionalID); err != nil {
		return err
	}
	pe.putInt32(int32(i.TransactionTimeout / time.Millisecond))
	if i.Version >= 3 {
		pe.putInt64(i.ProducerID)
		pe.putInt16(i.ProducerEpoch)
	}

	pe.putEmptyTaggedFieldArray()
	return nil
}

func (i *InitProducerIDRequest) decode(pd packetDecoder, version int16) (err error) {
	i.Version = version
	if i.TransactionalID, err = pd.getNullableString(); err != nil {
		return err
	}

	timeout, err := pd.getInt32()
	if err != nil {
		return err
	}
	i.TransactionTimeout = time.Duration(timeout) * time.Millisecond
	if i.Version >= 3 {
		if i.ProducerID, err = pd.getInt64(); err != nil {
			return err
		}

		if i.ProducerEpoch, err = pd.getInt16(); err != nil {
			return err
		}
	}

	_, err = pd.getEmptyTaggedFieldArray()
	return err
}

func (i *InitProducerIDRequest) key() int16 {
	return apiKeyInitProducerId
}

func (i *InitProducerIDRequest) version() int16 {
	return i.Version
}

func (i *InitProducerIDRequest) headerVersion() int16 {
	if i.Version >= 2 {
		return 2
	}

	return 1
}

func (i *InitProducerIDRequest) isValidVersion() bool {
	return i.Version >= 0 && i.Version <= 4
}

func (i *InitProducerIDRequest) isFlexible() bool {
	return i.isFlexibleVersion(i.Version)
}

func (i *InitProducerIDRequest) isFlexibleVersion(version int16) bool {
	return version >= 2
}

func (i *InitProducerIDRequest) requiredVersion() KafkaVersion {
	switch i.Version {
	case 4:
		return V2_7_0_0
	case 3:
		return V2_5_0_0
	case 2:
		return V2_4_0_0
	case 1:
		return V2_0_0_0
	case 0:
		return V0_11_0_0
	default:
		return V2_7_0_0
	}
}
