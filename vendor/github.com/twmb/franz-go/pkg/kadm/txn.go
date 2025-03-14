package kadm

import (
	"context"
	"errors"
	"sort"

	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// DescribedProducer contains the state of a transactional producer's last
// produce.
type DescribedProducer struct {
	Leader                int32  // Leader is the leader broker for this topic / partition.
	Topic                 string // Topic is the topic being produced to.
	Partition             int32  // Partition is the partition being produced to.
	ProducerID            int64  // ProducerID is the producer ID that produced.
	ProducerEpoch         int16  // ProducerEpoch is the epoch that produced.
	LastSequence          int32  // LastSequence is the last sequence number the producer produced.
	LastTimestamp         int64  // LastTimestamp is the last time this producer produced.
	CoordinatorEpoch      int32  // CoordinatorEpoch is the epoch of the transactional coordinator for the last produce.
	CurrentTxnStartOffset int64  // CurrentTxnStartOffset is the first offset in the transaction.
}

// Less returns whether the left described producer is less than the right,
// in order of:
//
//   - Topic
//   - Partition
//   - ProducerID
//   - ProducerEpoch
//   - LastTimestamp
//   - LastSequence
func (l *DescribedProducer) Less(r *DescribedProducer) bool {
	if l.Topic < r.Topic {
		return true
	}
	if l.Topic > r.Topic {
		return false
	}
	if l.Partition < r.Partition {
		return true
	}
	if l.Partition > r.Partition {
		return false
	}
	if l.ProducerID < r.ProducerID {
		return true
	}
	if l.ProducerID > r.ProducerID {
		return false
	}
	if l.ProducerEpoch < r.ProducerEpoch {
		return true
	}
	if l.ProducerEpoch > r.ProducerEpoch {
		return false
	}
	if l.LastTimestamp < r.LastTimestamp {
		return true
	}
	if l.LastTimestamp > r.LastTimestamp {
		return false
	}
	return l.LastSequence < r.LastSequence
}

// DescribedProducers maps producer IDs to the full described producer.
type DescribedProducers map[int64]DescribedProducer

// Sorted returns the described producers sorted by topic, partition, and
// producer ID.
func (ds DescribedProducers) Sorted() []DescribedProducer {
	var all []DescribedProducer
	for _, d := range ds {
		all = append(all, d)
	}
	sort.Slice(all, func(i, j int) bool {
		l, r := all[i], all[j]
		return l.Topic < r.Topic || l.Topic == r.Topic && (l.Partition < r.Partition || l.Partition == r.Partition && l.ProducerID < r.ProducerID)
	})
	return all
}

// Each calls fn for each described producer.
func (ds DescribedProducers) Each(fn func(DescribedProducer)) {
	for _, d := range ds {
		fn(d)
	}
}

// DescribedProducersPartition is a partition whose producer's were described.
type DescribedProducersPartition struct {
	Leader          int32              // Leader is the leader broker for this topic / partition.
	Topic           string             // Topic is the topic whose producer's were described.
	Partition       int32              // Partition is the partition whose producer's were described.
	ActiveProducers DescribedProducers // ActiveProducers are producer's actively transactionally producing to this partition.
	Err             error              // Err is non-nil if describing this partition failed.
	ErrMessage      string             // ErrMessage a potential extra message describing any error.
}

// DescribedProducersPartitions contains partitions whose producer's were described.
type DescribedProducersPartitions map[int32]DescribedProducersPartition

// Sorted returns the described partitions sorted by topic and partition.
func (ds DescribedProducersPartitions) Sorted() []DescribedProducersPartition {
	var all []DescribedProducersPartition
	for _, d := range ds {
		all = append(all, d)
	}
	sort.Slice(all, func(i, j int) bool {
		l, r := all[i], all[j]
		return l.Topic < r.Topic || l.Topic == r.Topic && l.Partition < r.Partition
	})
	return all
}

// SortedProducer returns all producers sorted first by partition, then by producer ID.
func (ds DescribedProducersPartitions) SortedProducers() []DescribedProducer {
	var all []DescribedProducer
	ds.EachProducer(func(d DescribedProducer) {
		all = append(all, d)
	})
	sort.Slice(all, func(i, j int) bool {
		l, r := all[i], all[j]
		return l.Topic < r.Topic || l.Topic == r.Topic && (l.Partition < r.Partition || l.Partition == r.Partition && l.ProducerID < r.ProducerID)
	})
	return all
}

// Each calls fn for each partition.
func (ds DescribedProducersPartitions) Each(fn func(DescribedProducersPartition)) {
	for _, d := range ds {
		fn(d)
	}
}

// EachProducer calls fn for each producer in all partitions.
func (ds DescribedProducersPartitions) EachProducer(fn func(DescribedProducer)) {
	for _, d := range ds {
		for _, p := range d.ActiveProducers {
			fn(p)
		}
	}
}

// DescribedProducersTopic contains topic partitions whose producer's were described.
type DescribedProducersTopic struct {
	Topic      string                       // Topic is the topic whose producer's were described.
	Partitions DescribedProducersPartitions // Partitions are partitions whose producer's were described.
}

// DescribedProducersTopics contains topics whose producer's were described.
type DescribedProducersTopics map[string]DescribedProducersTopic

// Sorted returns the described topics sorted by topic.
func (ds DescribedProducersTopics) Sorted() []DescribedProducersTopic {
	var all []DescribedProducersTopic
	ds.Each(func(d DescribedProducersTopic) {
		all = append(all, d)
	})
	sort.Slice(all, func(i, j int) bool {
		l, r := all[i], all[j]
		return l.Topic < r.Topic
	})
	return all
}

// Sorted returns the described partitions sorted by topic and partition.
func (ds DescribedProducersTopics) SortedPartitions() []DescribedProducersPartition {
	var all []DescribedProducersPartition
	ds.EachPartition(func(d DescribedProducersPartition) {
		all = append(all, d)
	})
	sort.Slice(all, func(i, j int) bool {
		l, r := all[i], all[j]
		return l.Topic < r.Topic || l.Topic == r.Topic && l.Partition < r.Partition
	})
	return all
}

// SortedProducer returns all producers sorted first by partition, then by producer ID.
func (ds DescribedProducersTopics) SortedProducers() []DescribedProducer {
	var all []DescribedProducer
	ds.EachProducer(func(d DescribedProducer) {
		all = append(all, d)
	})
	sort.Slice(all, func(i, j int) bool {
		l, r := all[i], all[j]
		return l.Topic < r.Topic || l.Topic == r.Topic && (l.Partition < r.Partition || l.Partition == r.Partition && l.ProducerID < r.ProducerID)
	})
	return all
}

// Each calls fn for every topic.
func (ds DescribedProducersTopics) Each(fn func(DescribedProducersTopic)) {
	for _, d := range ds {
		fn(d)
	}
}

// EachPartitions calls fn for all topic partitions.
func (ds DescribedProducersTopics) EachPartition(fn func(DescribedProducersPartition)) {
	for _, d := range ds {
		for _, p := range d.Partitions {
			fn(p)
		}
	}
}

// EachProducer calls fn for each producer in all topics and partitions.
func (ds DescribedProducersTopics) EachProducer(fn func(DescribedProducer)) {
	for _, d := range ds {
		for _, p := range d.Partitions {
			for _, b := range p.ActiveProducers {
				fn(b)
			}
		}
	}
}

// DescribeProducers describes all producers that are transactionally producing
// to the requested topic set. This request can be used to detect hanging
// transactions or other transaction related problems. If the input set is
// empty, this requests data for all partitions.
//
// This may return *ShardErrors or *AuthError.
func (cl *Client) DescribeProducers(ctx context.Context, s TopicsSet) (DescribedProducersTopics, error) {
	if len(s) == 0 {
		m, err := cl.Metadata(ctx)
		if err != nil {
			return nil, err
		}
		s = m.Topics.TopicsSet()
	} else if e := s.EmptyTopics(); len(e) > 0 {
		m, err := cl.Metadata(ctx, e...)
		if err != nil {
			return nil, err
		}
		for t, ps := range m.Topics.TopicsSet() {
			s[t] = ps
		}
	}

	req := kmsg.NewPtrDescribeProducersRequest()
	for _, t := range s.IntoList() {
		rt := kmsg.NewDescribeProducersRequestTopic()
		rt.Topic = t.Topic
		rt.Partitions = t.Partitions
		req.Topics = append(req.Topics, rt)
	}
	shards := cl.cl.RequestSharded(ctx, req)
	dts := make(DescribedProducersTopics)
	return dts, shardErrEachBroker(req, shards, func(b BrokerDetail, kr kmsg.Response) error {
		resp := kr.(*kmsg.DescribeProducersResponse)
		for _, rt := range resp.Topics {
			dt, exists := dts[rt.Topic]
			if !exists { // topic could be spread around brokers, we need to check existence
				dt = DescribedProducersTopic{
					Topic:      rt.Topic,
					Partitions: make(DescribedProducersPartitions),
				}
				dts[rt.Topic] = dt
			}
			dps := dt.Partitions
			for _, rp := range rt.Partitions {
				if err := maybeAuthErr(rp.ErrorCode); err != nil {
					return err
				}
				drs := make(DescribedProducers)
				dp := DescribedProducersPartition{
					Leader:          b.NodeID,
					Topic:           rt.Topic,
					Partition:       rp.Partition,
					ActiveProducers: drs,
					Err:             kerr.ErrorForCode(rp.ErrorCode),
					ErrMessage:      unptrStr(rp.ErrorMessage),
				}
				dps[rp.Partition] = dp // one partition globally, no need to exist-check
				for _, rr := range rp.ActiveProducers {
					dr := DescribedProducer{
						Leader:                b.NodeID,
						Topic:                 rt.Topic,
						Partition:             rp.Partition,
						ProducerID:            rr.ProducerID,
						ProducerEpoch:         int16(rr.ProducerEpoch),
						LastSequence:          rr.LastSequence,
						LastTimestamp:         rr.LastTimestamp,
						CoordinatorEpoch:      rr.CoordinatorEpoch,
						CurrentTxnStartOffset: rr.CurrentTxnStartOffset,
					}
					drs[dr.ProducerID] = dr
				}
			}
		}
		return nil
	})
}

// DescribedTransaction contains data from a describe transactions response for
// a single transactional ID.
type DescribedTransaction struct {
	Coordinator    int32  // Coordinator is the coordinator broker for this transactional ID.
	TxnID          string // TxnID is the name of this transactional ID.
	State          string // State is the state this transaction is in (Empty, Ongoing, PrepareCommit, PrepareAbort, CompleteCommit, CompleteAbort, Dead, PrepareEpochFence).
	TimeoutMillis  int32  // TimeoutMillis is the timeout of this transaction in milliseconds.
	StartTimestamp int64  // StartTimestamp is millisecond when this transaction started.
	ProducerID     int64  // ProducerID is the ID in use by the transactional ID.
	ProducerEpoch  int16  // ProducerEpoch is the epoch associated with the produce rID.

	// Topics is the set of partitions in the transaction, if active. When
	// preparing to commit or abort, this includes only partitions which do
	// not have markers. This does not include topics the user is not
	// authorized to describe.
	Topics TopicsSet

	Err error // Err is non-nil if the transaction could not be described.
}

// DescribedTransactions contains information from a describe transactions
// response.
type DescribedTransactions map[string]DescribedTransaction

// Sorted returns all described transactions sorted by transactional ID.
func (ds DescribedTransactions) Sorted() []DescribedTransaction {
	s := make([]DescribedTransaction, 0, len(ds))
	for _, d := range ds {
		s = append(s, d)
	}
	sort.Slice(s, func(i, j int) bool { return s[i].TxnID < s[j].TxnID })
	return s
}

// Each calls fn for each described transaction.
func (ds DescribedTransactions) Each(fn func(DescribedTransaction)) {
	for _, d := range ds {
		fn(d)
	}
}

// On calls fn for the transactional ID if it exists, returning the transaction
// and the error returned from fn. If fn is nil, this simply returns the
// transaction.
//
// The fn is given a shallow copy of the transaction. This function returns the
// copy as well; any modifications within fn are modifications on the returned
// copy.  Modifications on a described transaction's inner fields are persisted
// to the original map (because slices are pointers).
//
// If the transaction does not exist, this returns
// kerr.TransactionalIDNotFound.
func (rs DescribedTransactions) On(txnID string, fn func(*DescribedTransaction) error) (DescribedTransaction, error) {
	if len(rs) > 0 {
		r, ok := rs[txnID]
		if ok {
			if fn == nil {
				return r, nil
			}
			return r, fn(&r)
		}
	}
	return DescribedTransaction{}, kerr.TransactionalIDNotFound
}

// TransactionalIDs returns a sorted list of all transactional IDs.
func (ds DescribedTransactions) TransactionalIDs() []string {
	all := make([]string, 0, len(ds))
	for t := range ds {
		all = append(all, t)
	}
	sort.Strings(all)
	return all
}

// DescribeTransactions describes either all transactional IDs specified, or
// all transactional IDs in the cluster if none are specified.
//
// This may return *ShardErrors or *AuthError.
//
// If no transactional IDs are specified and this method first lists
// transactional IDs, and listing IDs returns a *ShardErrors, this function
// describes all successfully listed IDs and appends the list shard errors to
// any describe shard errors.
//
// If only one ID is described, there will be at most one request issued and
// there is no need to deeply inspect the error.
func (cl *Client) DescribeTransactions(ctx context.Context, txnIDs ...string) (DescribedTransactions, error) {
	var seList *ShardErrors
	if len(txnIDs) == 0 {
		listed, err := cl.ListTransactions(ctx, nil, nil)
		switch {
		case err == nil:
		case errors.As(err, &seList):
		default:
			return nil, err
		}
		txnIDs = listed.TransactionalIDs()
		if len(txnIDs) == 0 {
			return nil, err
		}
	}

	req := kmsg.NewPtrDescribeTransactionsRequest()
	req.TransactionalIDs = txnIDs

	shards := cl.cl.RequestSharded(ctx, req)
	described := make(DescribedTransactions)
	err := shardErrEachBroker(req, shards, func(b BrokerDetail, kr kmsg.Response) error {
		resp := kr.(*kmsg.DescribeTransactionsResponse)
		for _, rt := range resp.TransactionStates {
			if err := maybeAuthErr(rt.ErrorCode); err != nil {
				return err
			}
			t := DescribedTransaction{
				Coordinator:    b.NodeID,
				TxnID:          rt.TransactionalID,
				State:          rt.State,
				TimeoutMillis:  rt.TimeoutMillis,
				StartTimestamp: rt.StartTimestamp,
				ProducerID:     rt.ProducerID,
				ProducerEpoch:  rt.ProducerEpoch,
				Err:            kerr.ErrorForCode(rt.ErrorCode),
			}
			for _, rtt := range rt.Topics {
				t.Topics.Add(rtt.Topic, rtt.Partitions...)
			}
			described[t.TxnID] = t // txnID lives on one coordinator, no need to exist-check
		}
		return nil
	})

	var seDesc *ShardErrors
	switch {
	case err == nil:
		return described, seList.into()
	case errors.As(err, &seDesc):
		if seList != nil {
			seDesc.Errs = append(seList.Errs, seDesc.Errs...)
		}
		return described, seDesc.into()
	default:
		return nil, err
	}
}

// ListedTransaction contains data from a list transactions response for a
// single transactional ID.
type ListedTransaction struct {
	Coordinator int32  // Coordinator the coordinator broker for this transactional ID.
	TxnID       string // TxnID is the name of this transactional ID.
	ProducerID  int64  // ProducerID is the producer ID for this transaction.
	State       string // State is the state this transaction is in (Empty, Ongoing, PrepareCommit, PrepareAbort, CompleteCommit, CompleteAbort, Dead, PrepareEpochFence).
}

// ListedTransactions contains information from a list transactions response.
type ListedTransactions map[string]ListedTransaction

// Sorted returns all transactions sorted by transactional ID.
func (ls ListedTransactions) Sorted() []ListedTransaction {
	s := make([]ListedTransaction, 0, len(ls))
	for _, l := range ls {
		s = append(s, l)
	}
	sort.Slice(s, func(i, j int) bool { return s[i].TxnID < s[j].TxnID })
	return s
}

// Each calls fn for each listed transaction.
func (ls ListedTransactions) Each(fn func(ListedTransaction)) {
	for _, l := range ls {
		fn(l)
	}
}

// TransactionalIDs returns a sorted list of all transactional IDs.
func (ls ListedTransactions) TransactionalIDs() []string {
	all := make([]string, 0, len(ls))
	for t := range ls {
		all = append(all, t)
	}
	sort.Strings(all)
	return all
}

// ListTransactions returns all transactions and their states in the cluster.
// Filter states can be used to return transactions only in the requested
// states. By default, this returns all transactions you have DESCRIBE access
// to. Producer IDs can be specified to filter for transactions from the given
// producer.
//
// This may return *ShardErrors or *AuthError.
func (cl *Client) ListTransactions(ctx context.Context, producerIDs []int64, filterStates []string) (ListedTransactions, error) {
	req := kmsg.NewPtrListTransactionsRequest()
	req.ProducerIDFilters = producerIDs
	req.StateFilters = filterStates
	shards := cl.cl.RequestSharded(ctx, req)
	list := make(ListedTransactions)
	return list, shardErrEachBroker(req, shards, func(b BrokerDetail, kr kmsg.Response) error {
		resp := kr.(*kmsg.ListTransactionsResponse)
		if err := maybeAuthErr(resp.ErrorCode); err != nil {
			return err
		}
		if err := kerr.ErrorForCode(resp.ErrorCode); err != nil {
			return err
		}
		for _, t := range resp.TransactionStates {
			list[t.TransactionalID] = ListedTransaction{ // txnID lives on one coordinator, no need to exist-check
				Coordinator: b.NodeID,
				TxnID:       t.TransactionalID,
				ProducerID:  t.ProducerID,
				State:       t.TransactionState,
			}
		}
		return nil
	})
}

// TxnMarkers marks the end of a partition: the producer ID / epoch doing the
// writing, whether this is a commit, the coordinator epoch of the broker we
// are writing to (for fencing), and the topics and partitions that we are
// writing this abort or commit for.
//
// This is a very low level admin request and should likely be built from data
// in a DescribeProducers response. See KIP-664 if you are trying to use this.
type TxnMarkers struct {
	ProducerID       int64     // ProducerID is the ID to write markers for.
	ProducerEpoch    int16     // ProducerEpoch is the epoch to write markers for.
	Commit           bool      // Commit is true if we are committing, false if we are aborting.
	CoordinatorEpoch int32     // CoordinatorEpoch is the epoch of the transactional coordinator we are writing to; this is used for fencing.
	Topics           TopicsSet // Topics are topics and partitions to write markers for.
}

// TxnMarkersPartitionResponse is a response to a topic's partition within a
// single marker written.
type TxnMarkersPartitionResponse struct {
	NodeID     int32  // NodeID is the node that this marker was written to.
	ProducerID int64  // ProducerID corresponds to the PID in the write marker request.
	Topic      string // Topic is the topic being responded to.
	Partition  int32  // Partition is the partition being responded to.
	Err        error  // Err is non-nil if the WriteTxnMarkers request for this pid/topic/partition failed.
}

// TxnMarkersPartitionResponses contains per-partition responses to a
// WriteTxnMarkers request.
type TxnMarkersPartitionResponses map[int32]TxnMarkersPartitionResponse

// Sorted returns all partitions sorted by partition.
func (ps TxnMarkersPartitionResponses) Sorted() []TxnMarkersPartitionResponse {
	var all []TxnMarkersPartitionResponse
	ps.Each(func(p TxnMarkersPartitionResponse) {
		all = append(all, p)
	})
	sort.Slice(all, func(i, j int) bool {
		l, r := all[i], all[j]
		return l.Partition < r.Partition
	})
	return all
}

// Each calls fn for each partition.
func (ps TxnMarkersPartitionResponses) Each(fn func(TxnMarkersPartitionResponse)) {
	for _, p := range ps {
		fn(p)
	}
}

// TxnMarkersTopicResponse is a response to a topic within a single marker
// written.
type TxnMarkersTopicResponse struct {
	ProducerID int64                        // ProducerID corresponds to the PID in the write marker request.
	Topic      string                       // Topic is the topic being responded to.
	Partitions TxnMarkersPartitionResponses // Partitions are the responses for partitions in this marker.
}

// TxnMarkersTopicResponses contains per-topic responses to a WriteTxnMarkers
// request.
type TxnMarkersTopicResponses map[string]TxnMarkersTopicResponse

// Sorted returns all topics sorted by topic.
func (ts TxnMarkersTopicResponses) Sorted() []TxnMarkersTopicResponse {
	var all []TxnMarkersTopicResponse
	ts.Each(func(t TxnMarkersTopicResponse) {
		all = append(all, t)
	})
	sort.Slice(all, func(i, j int) bool {
		l, r := all[i], all[j]
		return l.Topic < r.Topic
	})
	return all
}

// SortedPartitions returns all topics sorted by topic then partition.
func (ts TxnMarkersTopicResponses) SortedPartitions() []TxnMarkersPartitionResponse {
	var all []TxnMarkersPartitionResponse
	ts.EachPartition(func(p TxnMarkersPartitionResponse) {
		all = append(all, p)
	})
	sort.Slice(all, func(i, j int) bool {
		l, r := all[i], all[j]
		return l.Topic < r.Topic || l.Topic == r.Topic && l.Partition < r.Partition
	})
	return all
}

// Each calls fn for each topic.
func (ts TxnMarkersTopicResponses) Each(fn func(TxnMarkersTopicResponse)) {
	for _, t := range ts {
		fn(t)
	}
}

// EachPartition calls fn for every partition in all topics.
func (ts TxnMarkersTopicResponses) EachPartition(fn func(TxnMarkersPartitionResponse)) {
	for _, t := range ts {
		for _, p := range t.Partitions {
			fn(p)
		}
	}
}

// TxnMarkersResponse is a response for a single marker written.
type TxnMarkersResponse struct {
	ProducerID int64                    // ProducerID corresponds to the PID in the write marker request.
	Topics     TxnMarkersTopicResponses // Topics contains the topics that markers were written for, for this ProducerID.
}

// TxnMarkersResponse contains per-partition-ID responses to a WriteTxnMarkers
// request.
type TxnMarkersResponses map[int64]TxnMarkersResponse

// Sorted returns all markers sorted by producer ID.
func (ms TxnMarkersResponses) Sorted() []TxnMarkersResponse {
	var all []TxnMarkersResponse
	ms.Each(func(m TxnMarkersResponse) {
		all = append(all, m)
	})
	sort.Slice(all, func(i, j int) bool {
		l, r := all[i], all[j]
		return l.ProducerID < r.ProducerID
	})
	return all
}

// SortedTopics returns all marker topics sorted by producer ID then topic.
func (ms TxnMarkersResponses) SortedTopics() []TxnMarkersTopicResponse {
	var all []TxnMarkersTopicResponse
	ms.EachTopic(func(t TxnMarkersTopicResponse) {
		all = append(all, t)
	})
	sort.Slice(all, func(i, j int) bool {
		l, r := all[i], all[j]
		return l.ProducerID < r.ProducerID || l.ProducerID == r.ProducerID && l.Topic < r.Topic
	})
	return all
}

// SortedPartitions returns all marker topic partitions sorted by producer ID
// then topic then partition.
func (ms TxnMarkersResponses) SortedPartitions() []TxnMarkersPartitionResponse {
	var all []TxnMarkersPartitionResponse
	ms.EachPartition(func(p TxnMarkersPartitionResponse) {
		all = append(all, p)
	})
	sort.Slice(all, func(i, j int) bool {
		l, r := all[i], all[j]
		return l.ProducerID < r.ProducerID || l.ProducerID == r.ProducerID && l.Topic < r.Topic || l.Topic == r.Topic && l.Partition < r.Partition
	})
	return all
}

// Each calls fn for each marker response.
func (ms TxnMarkersResponses) Each(fn func(TxnMarkersResponse)) {
	for _, m := range ms {
		fn(m)
	}
}

// EachTopic calls fn for every topic in all marker responses.
func (ms TxnMarkersResponses) EachTopic(fn func(TxnMarkersTopicResponse)) {
	for _, m := range ms {
		for _, t := range m.Topics {
			fn(t)
		}
	}
}

// EachPartition calls fn for every partition in all topics in all marker
// responses.
func (ms TxnMarkersResponses) EachPartition(fn func(TxnMarkersPartitionResponse)) {
	for _, m := range ms {
		for _, t := range m.Topics {
			for _, p := range t.Partitions {
				fn(p)
			}
		}
	}
}

// WriteTxnMarkers writes transaction markers to brokers. This is an advanced
// admin way to close out open transactions. See KIP-664 for more details.
//
// This may return *ShardErrors or *AuthError.
func (cl *Client) WriteTxnMarkers(ctx context.Context, markers ...TxnMarkers) (TxnMarkersResponses, error) {
	req := kmsg.NewPtrWriteTxnMarkersRequest()
	for _, m := range markers {
		rm := kmsg.NewWriteTxnMarkersRequestMarker()
		rm.ProducerID = m.ProducerID
		rm.ProducerEpoch = m.ProducerEpoch
		rm.Committed = m.Commit
		rm.CoordinatorEpoch = m.CoordinatorEpoch
		for t, ps := range m.Topics {
			rt := kmsg.NewWriteTxnMarkersRequestMarkerTopic()
			rt.Topic = t
			for p := range ps {
				rt.Partitions = append(rt.Partitions, p)
			}
			rm.Topics = append(rm.Topics, rt)
		}
		req.Markers = append(req.Markers, rm)
	}
	shards := cl.cl.RequestSharded(ctx, req)
	rs := make(TxnMarkersResponses)
	return rs, shardErrEachBroker(req, shards, func(b BrokerDetail, kr kmsg.Response) error {
		resp := kr.(*kmsg.WriteTxnMarkersResponse)
		for _, rm := range resp.Markers {
			m, exists := rs[rm.ProducerID] // partitions are spread around, our marker could be split: we need to check existence
			if !exists {
				m = TxnMarkersResponse{
					ProducerID: rm.ProducerID,
					Topics:     make(TxnMarkersTopicResponses),
				}
				rs[rm.ProducerID] = m
			}
			for _, rt := range rm.Topics {
				t, exists := m.Topics[rt.Topic]
				if !exists { // same thought
					t = TxnMarkersTopicResponse{
						ProducerID: rm.ProducerID,
						Topic:      rt.Topic,
						Partitions: make(TxnMarkersPartitionResponses),
					}
					m.Topics[rt.Topic] = t
				}
				for _, rp := range rt.Partitions {
					if err := maybeAuthErr(rp.ErrorCode); err != nil {
						return err
					}
					t.Partitions[rp.Partition] = TxnMarkersPartitionResponse{ // one partition globally, no need to exist-check
						NodeID:     b.NodeID,
						ProducerID: rm.ProducerID,
						Topic:      rt.Topic,
						Partition:  rp.Partition,
						Err:        kerr.ErrorForCode(rp.ErrorCode),
					}
				}
			}
		}
		return nil
	})
}
