package sarama

import "time"

type UpdateFeaturesResponse struct {
	Version int16

	ThrottleTime time.Duration

	ErrorCode    KError
	ErrorMessage *string

	// Results contains the per-feature update results
	Results []UpdatableFeatureResult
}

func (r *UpdateFeaturesResponse) setVersion(v int16) {
	r.Version = v
}

type UpdatableFeatureResult struct {
	Feature string

	ErrorCode    KError
	ErrorMessage *string
}

func (u *UpdatableFeatureResult) encode(pe packetEncoder) error {
	if err := pe.putString(u.Feature); err != nil {
		return err
	}

	pe.putKError(u.ErrorCode)

	if err := pe.putNullableString(u.ErrorMessage); err != nil {
		return err
	}

	pe.putEmptyTaggedFieldArray()
	return nil
}

func (u *UpdatableFeatureResult) decode(pd packetDecoder, version int16) (err error) {
	if u.Feature, err = pd.getString(); err != nil {
		return err
	}

	if u.ErrorCode, err = pd.getKError(); err != nil {
		return err
	}

	if u.ErrorMessage, err = pd.getNullableString(); err != nil {
		return err
	}

	_, err = pd.getEmptyTaggedFieldArray()
	return err
}

func (r *UpdateFeaturesResponse) encode(pe packetEncoder) error {
	pe.putDurationMs(r.ThrottleTime)

	pe.putKError(r.ErrorCode)

	if err := pe.putNullableString(r.ErrorMessage); err != nil {
		return err
	}

	if err := pe.putArrayLength(len(r.Results)); err != nil {
		return err
	}
	for i := range r.Results {
		if err := r.Results[i].encode(pe); err != nil {
			return err
		}
	}

	pe.putEmptyTaggedFieldArray()
	return nil
}

func (r *UpdateFeaturesResponse) decode(pd packetDecoder, version int16) (err error) {
	if r.ThrottleTime, err = pd.getDurationMs(); err != nil {
		return err
	}

	if r.ErrorCode, err = pd.getKError(); err != nil {
		return err
	}

	if r.ErrorMessage, err = pd.getNullableString(); err != nil {
		return err
	}

	n, err := pd.getArrayLength()
	if err != nil {
		return err
	}
	if n < 0 {
		return errInvalidArrayLength
	}

	r.Results = make([]UpdatableFeatureResult, n)
	for i := range n {
		if err := r.Results[i].decode(pd, version); err != nil {
			return err
		}
	}

	_, err = pd.getEmptyTaggedFieldArray()
	return err
}

func (r *UpdateFeaturesResponse) key() int16 {
	return apiKeyUpdateFeatures
}

func (r *UpdateFeaturesResponse) version() int16 {
	return r.Version
}

func (r *UpdateFeaturesResponse) headerVersion() int16 {
	return 1
}

func (r *UpdateFeaturesResponse) isValidVersion() bool {
	return r.Version == 0
}

func (r *UpdateFeaturesResponse) isFlexible() bool {
	return r.isFlexibleVersion(r.Version)
}

func (r *UpdateFeaturesResponse) isFlexibleVersion(version int16) bool {
	return version >= 0
}

func (r *UpdateFeaturesResponse) requiredVersion() KafkaVersion {
	return V2_7_0_0
}

func (r *UpdateFeaturesResponse) throttleTime() time.Duration {
	return r.ThrottleTime
}
