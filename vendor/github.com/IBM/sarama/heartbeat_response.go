package sarama

import "time"

type HeartbeatResponse struct {
	Version      int16
	ThrottleTime int32
	Err          KError
}

func (r *HeartbeatResponse) encode(pe packetEncoder) error {
	if r.Version >= 1 {
		pe.putInt32(r.ThrottleTime)
	}
	pe.putInt16(int16(r.Err))
	return nil
}

func (r *HeartbeatResponse) decode(pd packetDecoder, version int16) error {
	var err error
	r.Version = version
	if r.Version >= 1 {
		if r.ThrottleTime, err = pd.getInt32(); err != nil {
			return err
		}
	}
	kerr, err := pd.getInt16()
	if err != nil {
		return err
	}
	r.Err = KError(kerr)

	return nil
}

func (r *HeartbeatResponse) key() int16 {
	return 12
}

func (r *HeartbeatResponse) version() int16 {
	return r.Version
}

func (r *HeartbeatResponse) headerVersion() int16 {
	return 0
}

func (r *HeartbeatResponse) isValidVersion() bool {
	return r.Version >= 0 && r.Version <= 3
}

func (r *HeartbeatResponse) requiredVersion() KafkaVersion {
	switch r.Version {
	case 3:
		return V2_3_0_0
	case 2:
		return V2_0_0_0
	case 1:
		return V0_11_0_0
	case 0:
		return V0_8_2_0
	default:
		return V2_3_0_0
	}
}

func (r *HeartbeatResponse) throttleTime() time.Duration {
	return time.Duration(r.ThrottleTime) * time.Millisecond
}
