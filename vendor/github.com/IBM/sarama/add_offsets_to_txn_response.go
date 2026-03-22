package sarama

import (
	"time"
)

// AddOffsetsToTxnResponse is a response type for adding offsets to txns
type AddOffsetsToTxnResponse struct {
	Version      int16
	ThrottleTime time.Duration
	Err          KError
}

func (a *AddOffsetsToTxnResponse) setVersion(v int16) {
	a.Version = v
}

func (a *AddOffsetsToTxnResponse) encode(pe packetEncoder) error {
	pe.putDurationMs(a.ThrottleTime)
	pe.putKError(a.Err)
	return nil
}

func (a *AddOffsetsToTxnResponse) decode(pd packetDecoder, version int16) (err error) {
	if a.ThrottleTime, err = pd.getDurationMs(); err != nil {
		return err
	}

	a.Err, err = pd.getKError()
	if err != nil {
		return err
	}

	return nil
}

func (a *AddOffsetsToTxnResponse) key() int16 {
	return apiKeyAddOffsetsToTxn
}

func (a *AddOffsetsToTxnResponse) version() int16 {
	return a.Version
}

func (a *AddOffsetsToTxnResponse) headerVersion() int16 {
	return 0
}

func (a *AddOffsetsToTxnResponse) isValidVersion() bool {
	return a.Version >= 0 && a.Version <= 2
}

func (a *AddOffsetsToTxnResponse) requiredVersion() KafkaVersion {
	switch a.Version {
	case 2:
		return V2_7_0_0
	case 1:
		return V2_0_0_0
	case 0:
		return V0_11_0_0
	default:
		return V2_7_0_0
	}
}

func (r *AddOffsetsToTxnResponse) throttleTime() time.Duration {
	return r.ThrottleTime
}
