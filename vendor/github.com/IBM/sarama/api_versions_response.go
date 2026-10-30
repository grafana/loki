package sarama

import (
	"time"
)

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

	_, err = pd.getEmptyTaggedFieldArray()
	return err
}

// SupportedFeatureKey contains a feature supported by the broker.
type SupportedFeatureKey struct {
	// Name contains the name of the feature.
	Name string
	// MinVersion contains the minimum supported version for the feature.
	MinVersion int16
	// MaxVersion contains the maximum supported version for the feature.
	MaxVersion int16
}

func (f *SupportedFeatureKey) encode(pe packetEncoder) error {
	if err := pe.putString(f.Name); err != nil {
		return err
	}

	pe.putInt16(f.MinVersion)

	pe.putInt16(f.MaxVersion)

	pe.putEmptyTaggedFieldArray()
	return nil
}

func (f *SupportedFeatureKey) decode(pd packetDecoder) (err error) {
	if f.Name, err = pd.getString(); err != nil {
		return err
	}

	if f.MinVersion, err = pd.getInt16(); err != nil {
		return err
	}

	if f.MaxVersion, err = pd.getInt16(); err != nil {
		return err
	}

	_, err = pd.getEmptyTaggedFieldArray()
	return err
}

// FinalizedFeatureKey contains a cluster-wide finalized feature.
type FinalizedFeatureKey struct {
	// Name contains the name of the feature.
	Name string
	// MaxVersionLevel contains the cluster-wide finalized max version level for the feature.
	MaxVersionLevel int16
	// MinVersionLevel contains the cluster-wide finalized min version level for the feature.
	MinVersionLevel int16
}

func (f *FinalizedFeatureKey) encode(pe packetEncoder) error {
	if err := pe.putString(f.Name); err != nil {
		return err
	}

	pe.putInt16(f.MaxVersionLevel)

	pe.putInt16(f.MinVersionLevel)

	pe.putEmptyTaggedFieldArray()
	return nil
}

func (f *FinalizedFeatureKey) decode(pd packetDecoder) (err error) {
	if f.Name, err = pd.getString(); err != nil {
		return err
	}

	if f.MaxVersionLevel, err = pd.getInt16(); err != nil {
		return err
	}

	if f.MinVersionLevel, err = pd.getInt16(); err != nil {
		return err
	}

	_, err = pd.getEmptyTaggedFieldArray()
	return err
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
	// SupportedFeatures contains the features supported by the broker. (v3+, tag 0)
	SupportedFeatures []SupportedFeatureKey
	// FinalizedFeaturesEpoch contains the monotonically increasing epoch for the finalized features information, or -1 if unknown. (v3+, tag 1)
	FinalizedFeaturesEpoch int64
	// FinalizedFeatures contains the cluster-wide finalized features; the information is only valid if FinalizedFeaturesEpoch >= 0. (v3+, tag 2)
	FinalizedFeatures []FinalizedFeatureKey
}

func (r *ApiVersionsResponse) setVersion(v int16) {
	r.Version = v
}

func (r *ApiVersionsResponse) encode(pe packetEncoder) (err error) {
	pe.putInt16(r.ErrorCode)

	if err := pe.putArrayLength(len(r.ApiKeys)); err != nil {
		return err
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
		return r.encodeTaggedFields(pe)
	}

	return nil
}

func (r *ApiVersionsResponse) encodeTaggedFields(pe packetEncoder) error {
	type taggedField struct {
		tag   uint64
		value []byte
	}
	var fields []taggedField

	if len(r.SupportedFeatures) > 0 {
		value, err := encode(taggedFieldValue(func(pe packetEncoder) error {
			if err := pe.putArrayLength(len(r.SupportedFeatures)); err != nil {
				return err
			}
			for i := range r.SupportedFeatures {
				if err := r.SupportedFeatures[i].encode(pe); err != nil {
					return err
				}
			}
			return nil
		}), nil)
		if err != nil {
			return err
		}
		fields = append(fields, taggedField{tag: 0, value: value})
	}

	if r.FinalizedFeaturesEpoch >= 0 {
		value, err := encode(taggedFieldValue(func(pe packetEncoder) error {
			pe.putInt64(r.FinalizedFeaturesEpoch)
			return nil
		}), nil)
		if err != nil {
			return err
		}
		fields = append(fields, taggedField{tag: 1, value: value})
	}

	if len(r.FinalizedFeatures) > 0 {
		value, err := encode(taggedFieldValue(func(pe packetEncoder) error {
			if err := pe.putArrayLength(len(r.FinalizedFeatures)); err != nil {
				return err
			}
			for i := range r.FinalizedFeatures {
				if err := r.FinalizedFeatures[i].encode(pe); err != nil {
					return err
				}
			}
			return nil
		}), nil)
		if err != nil {
			return err
		}
		fields = append(fields, taggedField{tag: 2, value: value})
	}

	if len(fields) == 0 {
		pe.putEmptyTaggedFieldArray()
		return nil
	}

	pe.putUVarint(uint64(len(fields)))
	for _, f := range fields {
		pe.putUVarint(f.tag)
		pe.putUVarint(uint64(len(f.value)))
		if err := pe.putRawBytes(f.value); err != nil {
			return err
		}
	}
	return nil
}

func (r *ApiVersionsResponse) decode(pd packetDecoder, version int16) (err error) {
	r.Version = version
	if r.ErrorCode, err = pd.getInt16(); err != nil {
		return err
	}

	// KIP-511: if broker didn't understand the ApiVersionsRequest version then
	// it replies with a V0 non-flexible ApiVersionResponse where its supported
	// ApiVersionsRequest version is available in ApiKeys
	if r.ErrorCode == int16(ErrUnsupportedVersion) {
		// drop version to 0 and to revert packageDecoder to non-flexible for remaining decoding
		r.Version = 0
		pd = downgradeFlexibleDecoder(pd)
	}

	numApiKeys, err := pd.getArrayLength()
	if err != nil {
		return err
	}
	if numApiKeys < 0 {
		return errInvalidArrayLength
	}
	r.ApiKeys = make([]ApiVersionsResponseKey, numApiKeys)
	for i := range numApiKeys {
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
		r.FinalizedFeaturesEpoch = -1
		return pd.getTaggedFieldArray(taggedFieldDecoders{
			0: func(pd packetDecoder) error {
				n, err := pd.getArrayLength()
				if err != nil {
					return err
				}
				if n < 0 {
					return errInvalidArrayLength
				}
				r.SupportedFeatures = make([]SupportedFeatureKey, n)
				for i := range n {
					if err := r.SupportedFeatures[i].decode(pd); err != nil {
						return err
					}
				}
				return nil
			},
			1: func(pd packetDecoder) (err error) {
				r.FinalizedFeaturesEpoch, err = pd.getInt64()
				return err
			},
			2: func(pd packetDecoder) error {
				n, err := pd.getArrayLength()
				if err != nil {
					return err
				}
				if n < 0 {
					return errInvalidArrayLength
				}
				r.FinalizedFeatures = make([]FinalizedFeatureKey, n)
				for i := range n {
					if err := r.FinalizedFeatures[i].decode(pd); err != nil {
						return err
					}
				}
				return nil
			},
		})
	}

	return nil
}

func (r *ApiVersionsResponse) key() int16 {
	return apiKeyApiVersions
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
	return r.Version >= 0 && r.Version <= 4
}

func (r *ApiVersionsResponse) isFlexible() bool {
	return r.isFlexibleVersion(r.Version)
}

func (r *ApiVersionsResponse) isFlexibleVersion(version int16) bool {
	return version >= 3
}

func (r *ApiVersionsResponse) requiredVersion() KafkaVersion {
	switch r.Version {
	case 4:
		return V3_9_0_0
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
