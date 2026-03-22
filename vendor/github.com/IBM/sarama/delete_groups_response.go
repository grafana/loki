package sarama

import (
	"time"
)

type DeleteGroupsResponse struct {
	Version         int16
	ThrottleTime    time.Duration
	GroupErrorCodes map[string]KError
}

func (r *DeleteGroupsResponse) setVersion(v int16) {
	r.Version = v
}

func (r *DeleteGroupsResponse) encode(pe packetEncoder) error {
	pe.putDurationMs(r.ThrottleTime)

	if err := pe.putArrayLength(len(r.GroupErrorCodes)); err != nil {
		return err
	}
	for groupID, errorCode := range r.GroupErrorCodes {
		if err := pe.putString(groupID); err != nil {
			return err
		}
		pe.putKError(errorCode)
		pe.putEmptyTaggedFieldArray()
	}

	pe.putEmptyTaggedFieldArray()
	return nil
}

func (r *DeleteGroupsResponse) decode(pd packetDecoder, version int16) (err error) {
	if r.ThrottleTime, err = pd.getDurationMs(); err != nil {
		return err
	}

	n, err := pd.getArrayLength()
	if err != nil {
		return err
	}
	if n == 0 {
		_, err = pd.getEmptyTaggedFieldArray()
		return err
	}

	r.GroupErrorCodes = make(map[string]KError, n)
	for i := 0; i < n; i++ {
		groupID, err := pd.getString()
		if err != nil {
			return err
		}
		r.GroupErrorCodes[groupID], err = pd.getKError()
		if err != nil {
			return err
		}

		if _, err := pd.getEmptyTaggedFieldArray(); err != nil {
			return err
		}
	}

	_, err = pd.getEmptyTaggedFieldArray()
	return err
}

func (r *DeleteGroupsResponse) key() int16 {
	return apiKeyDeleteGroups
}

func (r *DeleteGroupsResponse) version() int16 {
	return r.Version
}

func (r *DeleteGroupsResponse) headerVersion() int16 {
	if r.Version >= 2 {
		return 1
	}
	return 0
}

func (r *DeleteGroupsResponse) isFlexible() bool {
	return r.isFlexibleVersion(r.Version)
}

func (r *DeleteGroupsResponse) isFlexibleVersion(version int16) bool {
	return version >= 2
}

func (r *DeleteGroupsResponse) isValidVersion() bool {
	return r.Version >= 0 && r.Version <= 2
}

func (r *DeleteGroupsResponse) requiredVersion() KafkaVersion {
	switch r.Version {
	case 2:
		return V2_4_0_0
	case 1:
		return V2_0_0_0
	case 0:
		return V1_1_0_0
	default:
		return V2_0_0_0
	}
}

func (r *DeleteGroupsResponse) throttleTime() time.Duration {
	return r.ThrottleTime
}
