package sarama

type SaslAuthenticateResponse struct {
	// Version defines the protocol version to use for encode and decode
	Version           int16
	Err               KError
	ErrorMessage      *string
	SaslAuthBytes     []byte
	SessionLifetimeMs int64
}

func (r *SaslAuthenticateResponse) setVersion(v int16) {
	r.Version = v
}

func (r *SaslAuthenticateResponse) encode(pe packetEncoder) error {
	pe.putKError(r.Err)
	if err := pe.putNullableString(r.ErrorMessage); err != nil {
		return err
	}
	if err := pe.putBytes(r.SaslAuthBytes); err != nil {
		return err
	}
	if r.Version > 0 {
		pe.putInt64(r.SessionLifetimeMs)
	}
	return nil
}

func (r *SaslAuthenticateResponse) decode(pd packetDecoder, version int16) (err error) {
	r.Version = version
	r.Err, err = pd.getKError()
	if err != nil {
		return err
	}

	if r.ErrorMessage, err = pd.getNullableString(); err != nil {
		return err
	}

	if r.SaslAuthBytes, err = pd.getBytes(); err != nil {
		return err
	}

	if version > 0 {
		r.SessionLifetimeMs, err = pd.getInt64()
	}

	return err
}

func (r *SaslAuthenticateResponse) key() int16 {
	return apiKeySASLAuth
}

func (r *SaslAuthenticateResponse) version() int16 {
	return r.Version
}

func (r *SaslAuthenticateResponse) headerVersion() int16 {
	return 0
}

func (r *SaslAuthenticateResponse) isValidVersion() bool {
	return r.Version >= 0 && r.Version <= 1
}

func (r *SaslAuthenticateResponse) requiredVersion() KafkaVersion {
	switch r.Version {
	case 1:
		return V2_2_0_0
	default:
		return V1_0_0_0
	}
}
