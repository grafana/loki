package sarama

import "time"

type UpdateFeaturesRequest struct {
	Version int16

	// Timeout is how long to wait before timing out the request
	Timeout time.Duration

	// FeatureUpdates is the list of updates to finalized features
	FeatureUpdates []FeatureUpdate
}

func (r *UpdateFeaturesRequest) setVersion(v int16) {
	r.Version = v
}

type FeatureUpdate struct {
	// Feature is the name of the finalized feature to update
	Feature string

	// MaxVersionLevel is the new maximum version level for the finalized
	// feature; a value < 1 requests deletion of the finalized feature
	MaxVersionLevel int16

	// AllowDowngrade, when true, allows the finalized feature version level
	// to be downgraded or deleted
	AllowDowngrade bool
}

func (f *FeatureUpdate) encode(pe packetEncoder) error {
	if err := pe.putString(f.Feature); err != nil {
		return err
	}

	pe.putInt16(f.MaxVersionLevel)

	pe.putBool(f.AllowDowngrade)

	pe.putEmptyTaggedFieldArray()
	return nil
}

func (f *FeatureUpdate) decode(pd packetDecoder, version int16) (err error) {
	if f.Feature, err = pd.getString(); err != nil {
		return err
	}

	if f.MaxVersionLevel, err = pd.getInt16(); err != nil {
		return err
	}

	if f.AllowDowngrade, err = pd.getBool(); err != nil {
		return err
	}

	_, err = pd.getEmptyTaggedFieldArray()
	return err
}

func (r *UpdateFeaturesRequest) encode(pe packetEncoder) error {
	pe.putDurationMs(r.Timeout)

	if err := pe.putArrayLength(len(r.FeatureUpdates)); err != nil {
		return err
	}
	for i := range r.FeatureUpdates {
		if err := r.FeatureUpdates[i].encode(pe); err != nil {
			return err
		}
	}

	pe.putEmptyTaggedFieldArray()
	return nil
}

func (r *UpdateFeaturesRequest) decode(pd packetDecoder, version int16) (err error) {
	if r.Timeout, err = pd.getDurationMs(); err != nil {
		return err
	}

	n, err := pd.getArrayLength()
	if err != nil {
		return err
	}
	if n < 0 {
		return errInvalidArrayLength
	}

	r.FeatureUpdates = make([]FeatureUpdate, n)
	for i := range n {
		if err := r.FeatureUpdates[i].decode(pd, version); err != nil {
			return err
		}
	}

	_, err = pd.getEmptyTaggedFieldArray()
	return err
}

func (r *UpdateFeaturesRequest) key() int16 {
	return apiKeyUpdateFeatures
}

func (r *UpdateFeaturesRequest) version() int16 {
	return r.Version
}

func (r *UpdateFeaturesRequest) headerVersion() int16 {
	return 2
}

func (r *UpdateFeaturesRequest) isValidVersion() bool {
	return r.Version == 0
}

func (r *UpdateFeaturesRequest) isFlexible() bool {
	return r.isFlexibleVersion(r.Version)
}

func (r *UpdateFeaturesRequest) isFlexibleVersion(version int16) bool {
	return version >= 0
}

func (r *UpdateFeaturesRequest) requiredVersion() KafkaVersion {
	return V2_7_0_0
}
