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

type AlterClientQuotasEntryResponse struct {
	ErrorCode KError                 // The error code, or `0` if the quota alteration succeeded.
	ErrorMsg  *string                // The error message, or `null` if the quota alteration succeeded.
	Entity    []QuotaEntityComponent // The quota entity altered.
}

func (a *AlterClientQuotasResponse) encode(pe packetEncoder) error {
	// ThrottleTime
	pe.putInt32(int32(a.ThrottleTime / time.Millisecond))

	// Entries
	if err := pe.putArrayLength(len(a.Entries)); err != nil {
		return err
	}
	for _, e := range a.Entries {
		if err := e.encode(pe); err != nil {
			return err
		}
	}

	return nil
}

func (a *AlterClientQuotasResponse) decode(pd packetDecoder, version int16) error {
	// ThrottleTime
	throttleTime, err := pd.getInt32()
	if err != nil {
		return err
	}
	a.ThrottleTime = time.Duration(throttleTime) * time.Millisecond

	// Entries
	entryCount, err := pd.getArrayLength()
	if err != nil {
		return err
	}
	if entryCount > 0 {
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

	return nil
}

func (a *AlterClientQuotasEntryResponse) encode(pe packetEncoder) error {
	// ErrorCode
	pe.putInt16(int16(a.ErrorCode))

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

	return nil
}

func (a *AlterClientQuotasEntryResponse) decode(pd packetDecoder, version int16) error {
	// ErrorCode
	errCode, err := pd.getInt16()
	if err != nil {
		return err
	}
	a.ErrorCode = KError(errCode)

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
		for i := 0; i < componentCount; i++ {
			component := QuotaEntityComponent{}
			if err := component.decode(pd, version); err != nil {
				return err
			}
			a.Entity[i] = component
		}
	} else {
		a.Entity = []QuotaEntityComponent{}
	}

	return nil
}

func (a *AlterClientQuotasResponse) key() int16 {
	return 49
}

func (a *AlterClientQuotasResponse) version() int16 {
	return a.Version
}

func (a *AlterClientQuotasResponse) headerVersion() int16 {
	return 0
}

func (a *AlterClientQuotasResponse) isValidVersion() bool {
	return a.Version == 0
}

func (a *AlterClientQuotasResponse) requiredVersion() KafkaVersion {
	return V2_6_0_0
}

func (r *AlterClientQuotasResponse) throttleTime() time.Duration {
	return r.ThrottleTime
}
