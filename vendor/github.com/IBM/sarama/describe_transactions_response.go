package sarama

import "time"

type DescribeTransactionsResponse struct {
	Version int16

	ThrottleTime time.Duration

	// TransactionStates contains the current state of each queried transaction
	TransactionStates []TransactionState
}

func (r *DescribeTransactionsResponse) setVersion(v int16) {
	r.Version = v
}

type TransactionState struct {
	ErrorCode KError

	// TransactionalID is the transactional id
	TransactionalID string

	// TransactionState is the current transaction state of the producer
	TransactionState string

	// TransactionTimeout is the timeout of the transaction
	TransactionTimeout time.Duration

	// TransactionStartTime is the start time of the transaction in
	// milliseconds
	TransactionStartTime int64

	// ProducerID is the current producer id associated with the transaction
	ProducerID int64

	// ProducerEpoch is the current epoch associated with the producer id
	ProducerEpoch int16

	// Topics is the set of partitions included in the current transaction (if
	// active). When a transaction is preparing to commit or abort, this will
	// include only partitions which do not have markers
	Topics []DescribeTransactionsResponseTopic
}

type DescribeTransactionsResponseTopic struct {
	// Topic is the topic name
	Topic string

	// Partitions is the partition ids included in the current transaction
	Partitions []int32
}

func (t *DescribeTransactionsResponseTopic) encode(pe packetEncoder) error {
	if err := pe.putString(t.Topic); err != nil {
		return err
	}

	if err := pe.putInt32Array(t.Partitions); err != nil {
		return err
	}

	pe.putEmptyTaggedFieldArray()
	return nil
}

func (t *DescribeTransactionsResponseTopic) decode(pd packetDecoder, version int16) (err error) {
	if t.Topic, err = pd.getString(); err != nil {
		return err
	}

	if t.Partitions, err = pd.getInt32Array(); err != nil {
		return err
	}

	_, err = pd.getEmptyTaggedFieldArray()
	return err
}

func (s *TransactionState) encode(pe packetEncoder) error {
	pe.putKError(s.ErrorCode)

	if err := pe.putString(s.TransactionalID); err != nil {
		return err
	}

	if err := pe.putString(s.TransactionState); err != nil {
		return err
	}

	pe.putDurationMs(s.TransactionTimeout)
	pe.putInt64(s.TransactionStartTime)
	pe.putInt64(s.ProducerID)
	pe.putInt16(s.ProducerEpoch)

	if err := pe.putArrayLength(len(s.Topics)); err != nil {
		return err
	}
	for i := range s.Topics {
		if err := s.Topics[i].encode(pe); err != nil {
			return err
		}
	}

	pe.putEmptyTaggedFieldArray()
	return nil
}

func (s *TransactionState) decode(pd packetDecoder, version int16) (err error) {
	if s.ErrorCode, err = pd.getKError(); err != nil {
		return err
	}

	if s.TransactionalID, err = pd.getString(); err != nil {
		return err
	}

	if s.TransactionState, err = pd.getString(); err != nil {
		return err
	}

	if s.TransactionTimeout, err = pd.getDurationMs(); err != nil {
		return err
	}

	if s.TransactionStartTime, err = pd.getInt64(); err != nil {
		return err
	}

	if s.ProducerID, err = pd.getInt64(); err != nil {
		return err
	}

	if s.ProducerEpoch, err = pd.getInt16(); err != nil {
		return err
	}

	n, err := pd.getArrayLength()
	if err != nil {
		return err
	}
	if n < 0 {
		return errInvalidArrayLength
	}

	s.Topics = make([]DescribeTransactionsResponseTopic, n)
	for i := range n {
		if err := s.Topics[i].decode(pd, version); err != nil {
			return err
		}
	}

	_, err = pd.getEmptyTaggedFieldArray()
	return err
}

func (r *DescribeTransactionsResponse) encode(pe packetEncoder) error {
	pe.putDurationMs(r.ThrottleTime)

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

func (r *DescribeTransactionsResponse) decode(pd packetDecoder, version int16) (err error) {
	if r.ThrottleTime, err = pd.getDurationMs(); err != nil {
		return err
	}

	n, err := pd.getArrayLength()
	if err != nil {
		return err
	}
	if n < 0 {
		return errInvalidArrayLength
	}

	r.TransactionStates = make([]TransactionState, n)
	for i := range n {
		if err := r.TransactionStates[i].decode(pd, version); err != nil {
			return err
		}
	}

	_, err = pd.getEmptyTaggedFieldArray()
	return err
}

func (r *DescribeTransactionsResponse) key() int16 {
	return apiKeyDescribeTransactions
}

func (r *DescribeTransactionsResponse) version() int16 {
	return r.Version
}

func (r *DescribeTransactionsResponse) headerVersion() int16 {
	return 1
}

func (r *DescribeTransactionsResponse) isValidVersion() bool {
	return r.Version == 0
}

func (r *DescribeTransactionsResponse) isFlexible() bool {
	return r.isFlexibleVersion(r.Version)
}

func (r *DescribeTransactionsResponse) isFlexibleVersion(version int16) bool {
	return version >= 0
}

func (r *DescribeTransactionsResponse) requiredVersion() KafkaVersion {
	return V3_0_0_0
}

func (r *DescribeTransactionsResponse) throttleTime() time.Duration {
	return r.ThrottleTime
}
