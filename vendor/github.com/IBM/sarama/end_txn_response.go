package sarama

import (
	"time"
)

type EndTxnResponse struct {
	Version      int16
	ThrottleTime time.Duration
	Err          KError
}

func (e *EndTxnResponse) setVersion(v int16) {
	e.Version = v
}

func (e *EndTxnResponse) encode(pe packetEncoder) error {
	pe.putDurationMs(e.ThrottleTime)
	pe.putKError(e.Err)
	return nil
}

func (e *EndTxnResponse) decode(pd packetDecoder, version int16) (err error) {
	if e.ThrottleTime, err = pd.getDurationMs(); err != nil {
		return err
	}

	e.Err, err = pd.getKError()
	if err != nil {
		return err
	}

	return nil
}

func (e *EndTxnResponse) key() int16 {
	return apiKeyEndTxn
}

func (e *EndTxnResponse) version() int16 {
	return e.Version
}

func (r *EndTxnResponse) headerVersion() int16 {
	return 0
}

func (e *EndTxnResponse) isValidVersion() bool {
	return e.Version >= 0 && e.Version <= 2
}

func (e *EndTxnResponse) requiredVersion() KafkaVersion {
	switch e.Version {
	case 2:
		return V2_7_0_0
	case 1:
		return V2_0_0_0
	default:
		return V0_11_0_0
	}
}

func (r *EndTxnResponse) throttleTime() time.Duration {
	return r.ThrottleTime
}
