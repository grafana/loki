package sarama

import "time"

// ApiVersionsResponseKey contains the APIs supported by the broker.
type ApiVersionsResponseKey struct {
	// Version defines the protocol version to use for encode and decode
	Version int16
	// ApiKey contains the API index.
	ApiKey int16
	// MinVersion contains the minimum supported version, inclusive.
	MinVersion int16
	// MaxVersion contains the maximum supported version, inclusive.
	MaxVersion int16
}

func (a *ApiVersionsResponseKey) encode(pe packetEncoder, version int16) (err error) {
	a.Version = version
	pe.putInt16(a.ApiKey)

	pe.putInt16(a.MinVersion)

	pe.putInt16(a.MaxVersion)

	if version >= 3 {
		pe.putEmptyTaggedFieldArray()
	}

	return nil
}

func (a *ApiVersionsResponseKey) decode(pd packetDecoder, version int16) (err error) {
	a.Version = version
	if a.ApiKey, err = pd.getInt16(); err != nil {
		return err
	}

	if a.MinVersion, err = pd.getInt16(); err != nil {
		return err
	}

	if a.MaxVersion, err = pd.getInt16(); err != nil {
		return err
	}

	if version >= 3 {
		if _, err := pd.getEmptyTaggedFieldArray(); err != nil {
			return err
		}
	}

	return nil
}

type ApiVersionsResponse struct {
	// Version defines the protocol version to use for encode and decode
	Version int16
	// ErrorCode contains the top-level error code.
	ErrorCode int16
	// ApiKeys contains the APIs supported by the broker.
	ApiKeys []ApiVersionsResponseKey
	// ThrottleTimeMs contains the duration in milliseconds for which the request was throttled due to a quota violation, or zero if the request did not violate any quota.
	ThrottleTimeMs int32
}

func (r *ApiVersionsResponse) encode(pe packetEncoder) (err error) {
	pe.putInt16(r.ErrorCode)

	if r.Version >= 3 {
		pe.putCompactArrayLength(len(r.ApiKeys))
	} else {
		if err := pe.putArrayLength(len(r.ApiKeys)); err != nil {
			return err
		}
	}
	for _, block := range r.ApiKeys {
		if err := block.encode(pe, r.Version); err != nil {
			return err
		}
	}

	if r.Version >= 1 {
		pe.putInt32(r.ThrottleTimeMs)
	}

	if r.Version >= 3 {
		pe.putEmptyTaggedFieldArray()
	}

	return nil
}

func (r *ApiVersionsResponse) decode(pd packetDecoder, version int16) (err error) {
	r.Version = version
	if r.ErrorCode, err = pd.getInt16(); err != nil {
		return err
	}

	var numApiKeys int
	if r.Version >= 3 {
		numApiKeys, err = pd.getCompactArrayLength()
		if err != nil {
			return err
		}
	} else {
		numApiKeys, err = pd.getArrayLength()
		if err != nil {
			return err
		}
	}
	r.ApiKeys = make([]ApiVersionsResponseKey, numApiKeys)
	for i := 0; i < numApiKeys; i++ {
		var block ApiVersionsResponseKey
		if err = block.decode(pd, r.Version); err != nil {
			return err
		}
		r.ApiKeys[i] = block
	}

	if r.Version >= 1 {
		if r.ThrottleTimeMs, err = pd.getInt32(); err != nil {
			return err
		}
	}

	if r.Version >= 3 {
		if _, err = pd.getEmptyTaggedFieldArray(); err != nil {
			return err
		}
	}

	return nil
}

func (r *ApiVersionsResponse) key() int16 {
	return 18
}

func (r *ApiVersionsResponse) version() int16 {
	return r.Version
}

func (r *ApiVersionsResponse) headerVersion() int16 {
	// ApiVersionsResponse always includes a v0 header.
	// See KIP-511 for details
	return 0
}

func (r *ApiVersionsResponse) isValidVersion() bool {
	return r.Version >= 0 && r.Version <= 3
}

func (r *ApiVersionsResponse) requiredVersion() KafkaVersion {
	switch r.Version {
	case 3:
		return V2_4_0_0
	case 2:
		return V2_0_0_0
	case 1:
		return V0_11_0_0
	case 0:
		return V0_10_0_0
	default:
		return V2_4_0_0
	}
}

func (r *ApiVersionsResponse) throttleTime() time.Duration {
	return time.Duration(r.ThrottleTimeMs) * time.Millisecond
}
