package sarama

// AddOffsetsToTxnRequest adds offsets to a transaction request
type AddOffsetsToTxnRequest struct {
	Version         int16
	TransactionalID string
	ProducerID      int64
	ProducerEpoch   int16
	GroupID         string
}

func (a *AddOffsetsToTxnRequest) setVersion(v int16) {
	a.Version = v
}

func (a *AddOffsetsToTxnRequest) encode(pe packetEncoder) error {
	if err := pe.putString(a.TransactionalID); err != nil {
		return err
	}

	pe.putInt64(a.ProducerID)

	pe.putInt16(a.ProducerEpoch)

	if err := pe.putString(a.GroupID); err != nil {
		return err
	}

	pe.putEmptyTaggedFieldArray()

	return nil
}

func (a *AddOffsetsToTxnRequest) decode(pd packetDecoder, version int16) (err error) {
	a.Version = version
	if a.TransactionalID, err = pd.getString(); err != nil {
		return err
	}
	if a.ProducerID, err = pd.getInt64(); err != nil {
		return err
	}
	if a.ProducerEpoch, err = pd.getInt16(); err != nil {
		return err
	}
	if a.GroupID, err = pd.getString(); err != nil {
		return err
	}
	if _, err = pd.getEmptyTaggedFieldArray(); err != nil {
		return err
	}
	return nil
}

func (a *AddOffsetsToTxnRequest) key() int16 {
	return apiKeyAddOffsetsToTxn
}

func (a *AddOffsetsToTxnRequest) version() int16 {
	return a.Version
}

func (a *AddOffsetsToTxnRequest) headerVersion() int16 {
	if a.Version >= 3 {
		return 2
	}
	return 1
}

func (a *AddOffsetsToTxnRequest) isValidVersion() bool {
	return a.Version >= 0 && a.Version <= 3
}

func (a *AddOffsetsToTxnRequest) isFlexible() bool {
	return a.isFlexibleVersion(a.Version)
}

func (a *AddOffsetsToTxnRequest) isFlexibleVersion(version int16) bool {
	return version >= 3
}

func (a *AddOffsetsToTxnRequest) requiredVersion() KafkaVersion {
	switch a.Version {
	case 3:
		return V2_8_0_0
	case 2:
		return V2_7_0_0
	case 1:
		return V2_0_0_0
	case 0:
		return V0_11_0_0
	default:
		return V2_8_0_0
	}
}
