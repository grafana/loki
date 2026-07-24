package sarama

import (
	"time"
)

// AlterClientQuotas Response (Version: 0) => throttle_time_ms [entries]
//   throttle_time_ms => INT32
//   entries => error_code error_message [entity]
//     error_code => INT16
//     error_message => NULLABLE_STRING
//     entity => entity_type entity_name
//       entity_type => STRING
//       entity_name => NULLABLE_STRING

type AlterClientQuotasResponse struct {
	Version      int16
	ThrottleTime time.Duration                    // The duration in milliseconds for which the request was throttled due to a quota violation, or zero if the request did not violate any quota.
	Entries      []AlterClientQuotasEntryResponse // The quota configuration entries altered.
}

func (a *AlterClientQuotasResponse) setVersion(v int16) {
	a.Version = v
}

type AlterClientQuotasEntryResponse struct {
	ErrorCode KError                 // The error code, or `0` if the quota alteration succeeded.
	ErrorMsg  *string                // The error message, or `null` if the quota alteration succeeded.
	Entity    []QuotaEntityComponent // The quota entity altered.
}

func (a *AlterClientQuotasResponse) encode(pe packetEncoder) error {
	pe.putDurationMs(a.ThrottleTime)

	// Entries
	if err := pe.putArrayLength(len(a.Entries)); err != nil {
		return err
	}
	for _, e := range a.Entries {
		if err := e.encode(pe); err != nil {
			return err
		}
	}

	pe.putEmptyTaggedFieldArray()

	return nil
}

func (a *AlterClientQuotasResponse) decode(pd packetDecoder, version int16) (err error) {
	a.Version = version
	if a.ThrottleTime, err = pd.getDurationMs(); err != nil {
		return err
	}

	// Entries
	entryCount, err := pd.getArrayLength()
	if err != nil {
		return err
	}
	if entryCount < 0 {
		return errInvalidArrayLength
	} else if entryCount > 0 {
		a.Entries = make([]AlterClientQuotasEntryResponse, entryCount)
		for i := range a.Entries {
			e := AlterClientQuotasEntryResponse{}
			if err = e.decode(pd, version); err != nil {
				return err
			}
			a.Entries[i] = e
		}
	} else {
		a.Entries = []AlterClientQuotasEntryResponse{}
	}

	if _, err = pd.getEmptyTaggedFieldArray(); err != nil {
		return err
	}

	return nil
}

func (a *AlterClientQuotasEntryResponse) encode(pe packetEncoder) error {
	// ErrorCode
	pe.putKError(a.ErrorCode)

	// ErrorMsg
	if err := pe.putNullableString(a.ErrorMsg); err != nil {
		return err
	}

	// Entity
	if err := pe.putArrayLength(len(a.Entity)); err != nil {
		return err
	}
	for _, component := range a.Entity {
		if err := component.encode(pe); err != nil {
			return err
		}
	}

	pe.putEmptyTaggedFieldArray()

	return nil
}

func (a *AlterClientQuotasEntryResponse) decode(pd packetDecoder, version int16) (err error) {
	// ErrorCode
	a.ErrorCode, err = pd.getKError()
	if err != nil {
		return err
	}

	// ErrorMsg
	errMsg, err := pd.getNullableString()
	if err != nil {
		return err
	}
	a.ErrorMsg = errMsg

	// Entity
	componentCount, err := pd.getArrayLength()
	if err != nil {
		return err
	}
	if componentCount > 0 {
		a.Entity = make([]QuotaEntityComponent, componentCount)
		for i := range componentCount {
			component := QuotaEntityComponent{}
			if err := component.decode(pd, version); err != nil {
				return err
			}
			a.Entity[i] = component
		}
	} else {
		a.Entity = []QuotaEntityComponent{}
	}

	if _, err = pd.getEmptyTaggedFieldArray(); err != nil {
		return err
	}

	return nil
}

func (a *AlterClientQuotasResponse) key() int16 {
	return apiKeyAlterClientQuotas
}

func (a *AlterClientQuotasResponse) version() int16 {
	return a.Version
}

func (a *AlterClientQuotasResponse) headerVersion() int16 {
	if a.Version >= 1 {
		return 1
	}
	return 0
}

func (a *AlterClientQuotasResponse) isValidVersion() bool {
	return a.Version >= 0 && a.Version <= 1
}

func (a *AlterClientQuotasResponse) isFlexible() bool {
	return a.isFlexibleVersion(a.Version)
}

func (a *AlterClientQuotasResponse) isFlexibleVersion(version int16) bool {
	return version >= 1
}

func (a *AlterClientQuotasResponse) requiredVersion() KafkaVersion {
	switch a.Version {
	case 1:
		return V2_8_0_0
	default:
		return V2_6_0_0
	}
}

func (r *AlterClientQuotasResponse) throttleTime() time.Duration {
	return r.ThrottleTime
}
