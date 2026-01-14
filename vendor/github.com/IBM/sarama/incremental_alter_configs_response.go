package sarama

import "time"

// IncrementalAlterConfigsResponse is a response type for incremental alter config
type IncrementalAlterConfigsResponse struct {
	Version      int16
	ThrottleTime time.Duration
	Resources    []*AlterConfigsResourceResponse
}

func (a *IncrementalAlterConfigsResponse) setVersion(v int16) {
	a.Version = v
}

func (a *IncrementalAlterConfigsResponse) encode(pe packetEncoder) error {
	pe.putDurationMs(a.ThrottleTime)

	if err := pe.putArrayLength(len(a.Resources)); err != nil {
		return err
	}

	for _, v := range a.Resources {
		if err := v.encode(pe); err != nil {
			return err
		}
	}

	return nil
}

func (a *IncrementalAlterConfigsResponse) decode(pd packetDecoder, version int16) (err error) {
	if a.ThrottleTime, err = pd.getDurationMs(); err != nil {
		return err
	}

	responseCount, err := pd.getArrayLength()
	if err != nil {
		return err
	}

	a.Resources = make([]*AlterConfigsResourceResponse, responseCount)

	for i := range a.Resources {
		a.Resources[i] = new(AlterConfigsResourceResponse)

		if err := a.Resources[i].decode(pd, version); err != nil {
			return err
		}
	}

	_, err = pd.getEmptyTaggedFieldArray()
	return err
}

func (a *IncrementalAlterConfigsResponse) key() int16 {
	return apiKeyIncrementalAlterConfigs
}

func (a *IncrementalAlterConfigsResponse) version() int16 {
	return a.Version
}

func (a *IncrementalAlterConfigsResponse) headerVersion() int16 {
	if a.Version >= 1 {
		return 1
	}
	return 0
}

func (a *IncrementalAlterConfigsResponse) isFlexible() bool {
	return a.isFlexibleVersion(a.Version)
}

func (a *IncrementalAlterConfigsResponse) isFlexibleVersion(version int16) bool {
	return version >= 1
}

func (a *IncrementalAlterConfigsResponse) isValidVersion() bool {
	return a.Version >= 0 && a.Version <= 1
}

func (a *IncrementalAlterConfigsResponse) requiredVersion() KafkaVersion {
	switch a.Version {
	case 1:
		return V2_4_0_0
	default:
		return V2_3_0_0
	}
}

func (r *IncrementalAlterConfigsResponse) throttleTime() time.Duration {
	return r.ThrottleTime
}
