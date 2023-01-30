package sarama

import "time"

type InitProducerIDResponse struct {
	ThrottleTime  time.Duration
	Err           KError
	Version       int16
	ProducerID    int64
	ProducerEpoch int16
}

func (i *InitProducerIDResponse) encode(pe packetEncoder) error {
	pe.putInt32(int32(i.ThrottleTime / time.Millisecond))
	pe.putInt16(int16(i.Err))
	pe.putInt64(i.ProducerID)
	pe.putInt16(i.ProducerEpoch)

	if i.Version >= 2 {
		pe.putEmptyTaggedFieldArray()
	}

	return nil
}

func (i *InitProducerIDResponse) decode(pd packetDecoder, version int16) (err error) {
	i.Version = version
	throttleTime, err := pd.getInt32()
	if err != nil {
		return err
	}
	i.ThrottleTime = time.Duration(throttleTime) * time.Millisecond

	kerr, err := pd.getInt16()
	if err != nil {
		return err
	}
	i.Err = KError(kerr)

	if i.ProducerID, err = pd.getInt64(); err != nil {
		return err
	}

	if i.ProducerEpoch, err = pd.getInt16(); err != nil {
		return err
	}

	if i.Version >= 2 {
		if _, err := pd.getEmptyTaggedFieldArray(); err != nil {
			return err
		}
	}

	return nil
}

func (i *InitProducerIDResponse) key() int16 {
	return 22
}

func (i *InitProducerIDResponse) version() int16 {
	return i.Version
}

func (i *InitProducerIDResponse) headerVersion() int16 {
	if i.Version >= 2 {
		return 1
	}
	return 0
}

func (i *InitProducerIDResponse) requiredVersion() KafkaVersion {
	switch i.Version {
	case 2:
		fallthrough
	case 3:
		return V2_4_0_0
	case 0:
		fallthrough
	case 1:
		fallthrough
	default:
		return V0_11_0_0
	}
}
