package sarama

import "time"

// IncrementalAlterConfigsResponse is a response type for incremental alter config
type IncrementalAlterConfigsResponse struct {
	ThrottleTime time.Duration
	Resources    []*AlterConfigsResourceResponse
}

func (a *IncrementalAlterConfigsResponse) encode(pe packetEncoder) error {
	pe.putInt32(int32(a.ThrottleTime / time.Millisecond))

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

func (a *IncrementalAlterConfigsResponse) decode(pd packetDecoder, version int16) error {
	throttleTime, err := pd.getInt32()
	if err != nil {
		return err
	}
	a.ThrottleTime = time.Duration(throttleTime) * time.Millisecond

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

	return nil
}

func (a *IncrementalAlterConfigsResponse) key() int16 {
	return 44
}

func (a *IncrementalAlterConfigsResponse) version() int16 {
	return 0
}

func (a *IncrementalAlterConfigsResponse) headerVersion() int16 {
	return 0
}

func (a *IncrementalAlterConfigsResponse) requiredVersion() KafkaVersion {
	return V2_3_0_0
}
