package sarama

type SaslHandshakeResponse struct {
	Version           int16
	Err               KError
	EnabledMechanisms []string
}

func (r *SaslHandshakeResponse) encode(pe packetEncoder) error {
	pe.putInt16(int16(r.Err))
	return pe.putStringArray(r.EnabledMechanisms)
}

func (r *SaslHandshakeResponse) decode(pd packetDecoder, version int16) error {
	kerr, err := pd.getInt16()
	if err != nil {
		return err
	}

	r.Err = KError(kerr)

	if r.EnabledMechanisms, err = pd.getStringArray(); err != nil {
		return err
	}

	return nil
}

func (r *SaslHandshakeResponse) key() int16 {
	return 17
}

func (r *SaslHandshakeResponse) version() int16 {
	return r.Version
}

func (r *SaslHandshakeResponse) headerVersion() int16 {
	return 0
}

func (r *SaslHandshakeResponse) isValidVersion() bool {
	return r.Version >= 0 && r.Version <= 1
}

func (r *SaslHandshakeResponse) requiredVersion() KafkaVersion {
	switch r.Version {
	case 1:
		return V1_0_0_0
	default:
		return V0_10_0_0
	}
}
