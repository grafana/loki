package sarama

import "time"

type AlterUserScramCredentialsResponse struct {
	Version int16

	ThrottleTime time.Duration

	Results []*AlterUserScramCredentialsResult
}

func (r *AlterUserScramCredentialsResponse) setVersion(v int16) {
	r.Version = v
}

type AlterUserScramCredentialsResult struct {
	User string

	ErrorCode    KError
	ErrorMessage *string
}

func (r *AlterUserScramCredentialsResponse) encode(pe packetEncoder) error {
	pe.putDurationMs(r.ThrottleTime)
	if err := pe.putArrayLength(len(r.Results)); err != nil {
		return err
	}

	for _, u := range r.Results {
		if err := pe.putString(u.User); err != nil {
			return err
		}
		pe.putKError(u.ErrorCode)
		if err := pe.putNullableString(u.ErrorMessage); err != nil {
			return err
		}
		pe.putEmptyTaggedFieldArray()
	}

	pe.putEmptyTaggedFieldArray()
	return nil
}

func (r *AlterUserScramCredentialsResponse) decode(pd packetDecoder, version int16) (err error) {
	if r.ThrottleTime, err = pd.getDurationMs(); err != nil {
		return err
	}

	numResults, err := pd.getArrayLength()
	if err != nil {
		return err
	}

	if numResults > 0 {
		r.Results = make([]*AlterUserScramCredentialsResult, numResults)
		for i := 0; i < numResults; i++ {
			r.Results[i] = &AlterUserScramCredentialsResult{}
			if r.Results[i].User, err = pd.getString(); err != nil {
				return err
			}

			r.Results[i].ErrorCode, err = pd.getKError()
			if err != nil {
				return err
			}

			if r.Results[i].ErrorMessage, err = pd.getNullableString(); err != nil {
				return err
			}
			if _, err := pd.getEmptyTaggedFieldArray(); err != nil {
				return err
			}
		}
	}

	_, err = pd.getEmptyTaggedFieldArray()
	return err
}

func (r *AlterUserScramCredentialsResponse) key() int16 {
	return apiKeyAlterUserScramCredentials
}

func (r *AlterUserScramCredentialsResponse) version() int16 {
	return r.Version
}

func (r *AlterUserScramCredentialsResponse) headerVersion() int16 {
	return 2
}

func (r *AlterUserScramCredentialsResponse) isValidVersion() bool {
	return r.Version == 0
}

func (r *AlterUserScramCredentialsResponse) isFlexible() bool {
	return r.isFlexibleVersion(r.Version)
}

func (r *AlterUserScramCredentialsResponse) isFlexibleVersion(version int16) bool {
	return version >= 0
}

func (r *AlterUserScramCredentialsResponse) requiredVersion() KafkaVersion {
	return V2_7_0_0
}

func (r *AlterUserScramCredentialsResponse) throttleTime() time.Duration {
	return r.ThrottleTime
}
