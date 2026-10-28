package sarama

import "time"

type ListTransactionsResponse struct {
	Version int16

	ThrottleTime time.Duration

	ErrorCode KError

	// UnknownStateFilters is the set of state filters provided in the request
	// which were unknown to the transaction coordinator
	UnknownStateFilters []string

	// TransactionStates contains the current state of each matched transaction
	TransactionStates []ListTransactionsResponseTransactionState
}

func (r *ListTransactionsResponse) setVersion(v int16) {
	r.Version = v
}

type ListTransactionsResponseTransactionState struct {
	// TransactionalID is the transactional id
	TransactionalID string

	// ProducerID is the producer id
	ProducerID int64

	// TransactionState is the current transaction state of the producer
	TransactionState string
}

func (s *ListTransactionsResponseTransactionState) encode(pe packetEncoder) error {
	if err := pe.putString(s.TransactionalID); err != nil {
		return err
	}

	pe.putInt64(s.ProducerID)

	if err := pe.putString(s.TransactionState); err != nil {
		return err
	}

	pe.putEmptyTaggedFieldArray()
	return nil
}

func (s *ListTransactionsResponseTransactionState) decode(pd packetDecoder, version int16) (err error) {
	if s.TransactionalID, err = pd.getString(); err != nil {
		return err
	}

	if s.ProducerID, err = pd.getInt64(); err != nil {
		return err
	}

	if s.TransactionState, err = pd.getString(); err != nil {
		return err
	}

	_, err = pd.getEmptyTaggedFieldArray()
	return err
}

func (r *ListTransactionsResponse) encode(pe packetEncoder) error {
	pe.putDurationMs(r.ThrottleTime)

	pe.putKError(r.ErrorCode)

	if err := pe.putStringArray(r.UnknownStateFilters); err != nil {
		return err
	}

	if err := pe.putArrayLength(len(r.TransactionStates)); err != nil {
		return err
	}
	for i := range r.TransactionStates {
		if err := r.TransactionStates[i].encode(pe); err != nil {
			return err
		}
	}

	pe.putEmptyTaggedFieldArray()
	return nil
}

func (r *ListTransactionsResponse) decode(pd packetDecoder, version int16) (err error) {
	r.Version = version

	if r.ThrottleTime, err = pd.getDurationMs(); err != nil {
		return err
	}

	if r.ErrorCode, err = pd.getKError(); err != nil {
		return err
	}

	if r.UnknownStateFilters, err = pd.getStringArray(); err != nil {
		return err
	}

	n, err := pd.getArrayLength()
	if err != nil {
		return err
	}
	if n < 0 {
		return errInvalidArrayLength
	}

	r.TransactionStates = make([]ListTransactionsResponseTransactionState, n)
	for i := range n {
		if err := r.TransactionStates[i].decode(pd, version); err != nil {
			return err
		}
	}

	_, err = pd.getEmptyTaggedFieldArray()
	return err
}

func (r *ListTransactionsResponse) key() int16 {
	return apiKeyListTransactions
}

func (r *ListTransactionsResponse) version() int16 {
	return r.Version
}

func (r *ListTransactionsResponse) headerVersion() int16 {
	return 1
}

func (r *ListTransactionsResponse) isValidVersion() bool {
	return r.Version >= 0 && r.Version <= 1
}

func (r *ListTransactionsResponse) isFlexible() bool {
	return r.isFlexibleVersion(r.Version)
}

func (r *ListTransactionsResponse) isFlexibleVersion(version int16) bool {
	return version >= 0
}

func (r *ListTransactionsResponse) requiredVersion() KafkaVersion {
	switch r.Version {
	case 1:
		return V3_8_0_0
	default:
		return V3_0_0_0
	}
}

func (r *ListTransactionsResponse) throttleTime() time.Duration {
	return r.ThrottleTime
}
