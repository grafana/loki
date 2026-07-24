package sarama

type DescribeTransactionsRequest struct {
	Version int16

	// TransactionalIDs is the set of transactional ids to include in the
	// describe results. If empty, then no results will be returned
	TransactionalIDs []string
}

func (r *DescribeTransactionsRequest) setVersion(v int16) {
	r.Version = v
}

func (r *DescribeTransactionsRequest) encode(pe packetEncoder) error {
	if err := pe.putStringArray(r.TransactionalIDs); err != nil {
		return err
	}

	pe.putEmptyTaggedFieldArray()
	return nil
}

func (r *DescribeTransactionsRequest) decode(pd packetDecoder, version int16) (err error) {
	if r.TransactionalIDs, err = pd.getStringArray(); err != nil {
		return err
	}

	_, err = pd.getEmptyTaggedFieldArray()
	return err
}

func (r *DescribeTransactionsRequest) key() int16 {
	return apiKeyDescribeTransactions
}

func (r *DescribeTransactionsRequest) version() int16 {
	return r.Version
}

func (r *DescribeTransactionsRequest) headerVersion() int16 {
	return 2
}

func (r *DescribeTransactionsRequest) isValidVersion() bool {
	return r.Version == 0
}

func (r *DescribeTransactionsRequest) isFlexible() bool {
	return r.isFlexibleVersion(r.Version)
}

func (r *DescribeTransactionsRequest) isFlexibleVersion(version int16) bool {
	return version >= 0
}

func (r *DescribeTransactionsRequest) requiredVersion() KafkaVersion {
	return V3_0_0_0
}
