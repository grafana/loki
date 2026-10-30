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
	pe.putEmptyTaggedFieldArray()
	return nil
}

func (a *AddOffsetsToTxnResponse) decode(pd packetDecoder, version int16) (err error) {
	a.Version = version
	if a.ThrottleTime, err = pd.getDurationMs(); err != nil {
		return err
	}

	a.Err, err = pd.getKError()
	if err != nil {
		return err
	}

	if _, err = pd.getEmptyTaggedFieldArray(); err != nil {
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
	if a.Version >= 3 {
		return 1
	}
	return 0
}

func (a *AddOffsetsToTxnResponse) isValidVersion() bool {
	return a.Version >= 0 && a.Version <= 3
}

func (a *AddOffsetsToTxnResponse) isFlexible() bool {
	return a.isFlexibleVersion(a.Version)
}

func (a *AddOffsetsToTxnResponse) isFlexibleVersion(version int16) bool {
	return version >= 3
}

func (a *AddOffsetsToTxnResponse) requiredVersion() KafkaVersion {
	switch a.Version {
	case 3:
		return V2_8_0_0
	case 2:
		return V2_7_0_0
	case 1:
		return V2_0_0_0
	case 0:
		return V0_11_0_0
	default:
		return V2_8_0_0
	}
}

func (r *AddOffsetsToTxnResponse) throttleTime() time.Duration {
	return r.ThrottleTime
}
