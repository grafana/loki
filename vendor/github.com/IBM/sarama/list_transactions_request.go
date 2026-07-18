package sarama

// Transaction states as reported by the transaction coordinator, valid for
// use in ListTransactionsRequest.StateFilters
const (
	TransactionStateEmpty             = "Empty"
	TransactionStateOngoing           = "Ongoing"
	TransactionStatePrepareCommit     = "PrepareCommit"
	TransactionStatePrepareAbort      = "PrepareAbort"
	TransactionStateCompleteCommit    = "CompleteCommit"
	TransactionStateCompleteAbort     = "CompleteAbort"
	TransactionStateDead              = "Dead"
	TransactionStatePrepareEpochFence = "PrepareEpochFence"
)

type ListTransactionsRequest struct {
	Version int16

	// StateFilters is the set of transaction states to filter by: if empty,
	// all transactions are returned; otherwise, only transactions matching
	// one of the filtered states are returned
	StateFilters []string

	// ProducerIDFilters is the set of producer ids to filter by: if empty,
	// all transactions are returned; otherwise, only transactions matching
	// one of the filtered producer ids are returned
	ProducerIDFilters []int64

	// DurationFilter is the duration in milliseconds to filter by: if less
	// than 0, all transactions are returned; otherwise, only transactions
	// running longer than this duration are returned (v1+)
	DurationFilter int64
}

func (r *ListTransactionsRequest) setVersion(v int16) {
	r.Version = v
}

func (r *ListTransactionsRequest) encode(pe packetEncoder) error {
	if err := pe.putStringArray(r.StateFilters); err != nil {
		return err
	}

	if err := pe.putInt64Array(r.ProducerIDFilters); err != nil {
		return err
	}

	if r.Version >= 1 {
		pe.putInt64(r.DurationFilter)
	}

	pe.putEmptyTaggedFieldArray()
	return nil
}

func (r *ListTransactionsRequest) decode(pd packetDecoder, version int16) (err error) {
	if r.StateFilters, err = pd.getStringArray(); err != nil {
		return err
	}

	if r.ProducerIDFilters, err = pd.getInt64Array(); err != nil {
		return err
	}

	if version >= 1 {
		if r.DurationFilter, err = pd.getInt64(); err != nil {
			return err
		}
	} else {
		r.DurationFilter = -1
	}

	_, err = pd.getEmptyTaggedFieldArray()
	return err
}

func (r *ListTransactionsRequest) key() int16 {
	return apiKeyListTransactions
}

func (r *ListTransactionsRequest) version() int16 {
	return r.Version
}

func (r *ListTransactionsRequest) headerVersion() int16 {
	return 2
}

func (r *ListTransactionsRequest) isValidVersion() bool {
	return r.Version >= 0 && r.Version <= 1
}

func (r *ListTransactionsRequest) isFlexible() bool {
	return r.isFlexibleVersion(r.Version)
}

func (r *ListTransactionsRequest) isFlexibleVersion(version int16) bool {
	return version >= 0
}

func (r *ListTransactionsRequest) requiredVersion() KafkaVersion {
	switch r.Version {
	case 1:
		return V3_8_0_0
	default:
		return V3_0_0_0
	}
}
