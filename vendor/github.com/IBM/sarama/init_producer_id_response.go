package sarama

import "time"

type InitProducerIDResponse struct {
	ThrottleTime  time.Duration
	Err           KError
	Version       int16
	ProducerID    int64
	ProducerEpoch int16
}

func (i *InitProducerIDResponse) setVersion(v int16) {
	i.Version = v
}

func (i *InitProducerIDResponse) encode(pe packetEncoder) error {
	pe.putDurationMs(i.ThrottleTime)
	pe.putKError(i.Err)
	pe.putInt64(i.ProducerID)
	pe.putInt16(i.ProducerEpoch)
	pe.putEmptyTaggedFieldArray()
	return nil
}

func (i *InitProducerIDResponse) decode(pd packetDecoder, version int16) (err error) {
	i.Version = version
	if i.ThrottleTime, err = pd.getDurationMs(); err != nil {
		return err
	}

	i.Err, err = pd.getKError()
	if err != nil {
		return err
	}

	if i.ProducerID, err = pd.getInt64(); err != nil {
		return err
	}

	if i.ProducerEpoch, err = pd.getInt16(); err != nil {
		return err
	}

	_, err = pd.getEmptyTaggedFieldArray()
	return err
}

func (i *InitProducerIDResponse) key() int16 {
	return apiKeyInitProducerId
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

func (i *InitProducerIDResponse) isValidVersion() bool {
	return i.Version >= 0 && i.Version <= 4
}

func (i *InitProducerIDResponse) isFlexible() bool {
	return i.isFlexibleVersion(i.Version)
}

func (i *InitProducerIDResponse) isFlexibleVersion(version int16) bool {
	return version >= 2
}

func (i *InitProducerIDResponse) requiredVersion() KafkaVersion {
	switch i.Version {
	case 4:
		return V2_7_0_0
	case 3:
		return V2_5_0_0
	case 2:
		return V2_4_0_0
	case 1:
		return V2_0_0_0
	default:
		return V0_11_0_0
	}
}

func (r *InitProducerIDResponse) throttleTime() time.Duration {
	return r.ThrottleTime
}
