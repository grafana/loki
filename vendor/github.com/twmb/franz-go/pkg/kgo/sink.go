package kgo

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"hash/crc32"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/twmb/franz-go/pkg/kbin"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
)

type sink struct {
	cl     *Client // our owning client, for cfg, metadata triggering, context, etc.
	nodeID int32   // the node ID of the broker this sink belongs to

	// inflightSem controls the number of concurrent produce requests.  We
	// start with a limit of 1, which covers Kafka v0.11.0. On the first
	// response, we check what version was set in the request. If it is at
	// least 4, which 1.0 introduced, we upgrade the sem size.
	inflightSem    atomic.Value
	produceVersion atomicI32 // negative is unset, positive is version

	drainState workLoop

	// seqRespsMu, guarded by seqRespsMu, contains responses that must
	// be handled sequentially. These responses are handled asynchronously,
	// but sequentially.
	seqResps ringSeqResp

	backoffMu   sync.Mutex // guards the following
	needBackoff bool
	backoffSeq  uint32 // prevents pile on failures

	// consecutiveFailures is incremented every backoff and cleared every
	// successful response. For simplicity, if we have a good response
	// following an error response before the error response's backoff
	// occurs, the backoff is not cleared.
	consecutiveFailures atomicU32

	recBufsMu    sync.Mutex // guards the following
	recBufs      []*recBuf  // contains all partition records for batch building
	recBufsStart int        // incremented every req to avoid large batch starvation
}

type seqResp struct {
	resp    kmsg.Response
	err     error
	done    chan struct{}
	br      *broker
	promise func(*broker, kmsg.Response, error)
}

func (cl *Client) newSink(nodeID int32) *sink {
	s := &sink{
		cl:     cl,
		nodeID: nodeID,
	}
	s.produceVersion.Store(-1)
	maxInflight := 1
	if cl.cfg.disableIdempotency {
		maxInflight = cl.cfg.maxProduceInflight
	}
	s.inflightSem.Store(make(chan struct{}, maxInflight))
	return s
}

// createReq returns a produceRequest from currently buffered records
// and whether there are more records to create more requests immediately.
func (s *sink) createReq(id int64, epoch int16) (*produceRequest, *kmsg.AddPartitionsToTxnRequest, bool) {
	req := &produceRequest{
		txnID:   s.cl.cfg.txnID,
		acks:    s.cl.cfg.acks.val,
		timeout: int32(s.cl.cfg.produceTimeout.Milliseconds()),
		batches: make(seqRecBatches, 5),

		producerID:    id,
		producerEpoch: epoch,

		hasHook:    s.cl.producer.hasHookBatchWritten,
		compressor: s.cl.compressor,

		wireLength:      s.cl.baseProduceRequestLength(), // start length with no topics
		wireLengthLimit: s.cl.cfg.maxBrokerWriteBytes,
	}
	txnBuilder := txnReqBuilder{
		txnID: req.txnID,
		id:    id,
		epoch: epoch,
	}

	var moreToDrain bool

	s.recBufsMu.Lock()
	defer s.recBufsMu.Unlock()

	recBufsIdx := s.recBufsStart
	for i := 0; i < len(s.recBufs); i++ {
		recBuf := s.recBufs[recBufsIdx]
		recBufsIdx = (recBufsIdx + 1) % len(s.recBufs)

		recBuf.mu.Lock()
		if recBuf.failing || len(recBuf.batches) == recBuf.batchDrainIdx || recBuf.inflightOnSink != nil && recBuf.inflightOnSink != s || recBuf.inflight != 0 && !recBuf.okOnSink {
			recBuf.mu.Unlock()
			continue
		}

		batch := recBuf.batches[recBuf.batchDrainIdx]
		if added := req.tryAddBatch(s.produceVersion.Load(), recBuf, batch); !added {
			recBuf.mu.Unlock()
			moreToDrain = true
			continue
		}

		recBuf.inflightOnSink = s
		recBuf.inflight++

		recBuf.batchDrainIdx++
		recBuf.seq = incrementSequence(recBuf.seq, int32(len(batch.records)))
		moreToDrain = moreToDrain || recBuf.tryStopLingerForDraining()
		recBuf.mu.Unlock()

		txnBuilder.add(recBuf)
	}

	// We could have lost our only record buffer just before we grabbed the
	// lock above, so we have to check there are recBufs.
	if len(s.recBufs) > 0 {
		s.recBufsStart = (s.recBufsStart + 1) % len(s.recBufs)
	}
	return req, txnBuilder.req, moreToDrain
}

func incrementSequence(sequence, increment int32) int32 {
	if sequence > math.MaxInt32-increment {
		return increment - (math.MaxInt32 - sequence) - 1
	}

	return sequence + increment
}

type txnReqBuilder struct {
	txnID       *string
	req         *kmsg.AddPartitionsToTxnRequest
	id          int64
	epoch       int16
	addedTopics map[string]int // topic => index into req
}

func (t *txnReqBuilder) add(rb *recBuf) {
	if t.txnID == nil {
		return
	}
	if rb.addedToTxn.Swap(true) {
		return
	}
	if t.req == nil {
		req := kmsg.NewPtrAddPartitionsToTxnRequest()
		req.TransactionalID = *t.txnID
		req.ProducerID = t.id
		req.ProducerEpoch = t.epoch
		t.req = req
		t.addedTopics = make(map[string]int, 10)
	}
	idx, exists := t.addedTopics[rb.topic]
	if !exists {
		idx = len(t.req.Topics)
		t.addedTopics[rb.topic] = idx
		reqTopic := kmsg.NewAddPartitionsToTxnRequestTopic()
		reqTopic.Topic = rb.topic
		t.req.Topics = append(t.req.Topics, reqTopic)
	}
	t.req.Topics[idx].Partitions = append(t.req.Topics[idx].Partitions, rb.partition)
}

func (s *sink) maybeDrain() {
	if s.cl.cfg.manualFlushing && s.cl.producer.flushing.Load() == 0 {
		return
	}
	if s.drainState.maybeBegin() {
		go s.drain()
	}
}

func (s *sink) maybeBackoff() {
	s.backoffMu.Lock()
	backoff := s.needBackoff
	s.backoffMu.Unlock()

	if !backoff {
		return
	}
	defer s.clearBackoff()

	s.cl.triggerUpdateMetadata(false, "opportunistic load during sink backoff") // as good a time as any

	tries := int(s.consecutiveFailures.Add(1))
	after := time.NewTimer(s.cl.cfg.retryBackoff(tries))
	defer after.Stop()

	select {
	case <-after.C:
	case <-s.cl.ctx.Done():
	case <-s.anyCtx().Done():
	}
}

func (s *sink) maybeTriggerBackoff(seq uint32) {
	s.backoffMu.Lock()
	defer s.backoffMu.Unlock()
	if seq == s.backoffSeq {
		s.needBackoff = true
	}
}

func (s *sink) clearBackoff() {
	s.backoffMu.Lock()
	defer s.backoffMu.Unlock()
	s.backoffSeq++
	s.needBackoff = false
}

// drain drains buffered records and issues produce requests.
//
// This function is harmless if there are no records that need draining.
// We rely on that to not worry about accidental triggers of this function.
func (s *sink) drain() {
	again := true
	for again {
		s.maybeBackoff()

		sem := s.inflightSem.Load().(chan struct{})
		select {
		case sem <- struct{}{}:
		case <-s.cl.ctx.Done():
			s.drainState.hardFinish()
			return
		}

		again = s.drainState.maybeFinish(s.produce(sem))
	}
}

// Returns the first context encountered ranging across all records.
// This does not use defers to make it clear at the return that all
// unlocks are called in proper order. Ideally, do not call this func
// due to lock intensity.
func (s *sink) anyCtx() context.Context {
	s.recBufsMu.Lock()
	for _, recBuf := range s.recBufs {
		recBuf.mu.Lock()
		if len(recBuf.batches) > 0 {
			batch0 := recBuf.batches[0]
			batch0.mu.Lock()
			if batch0.canFailFromLoadErrs && len(batch0.records) > 0 {
				r0 := batch0.records[0]
				if rctx := r0.cancelingCtx(); rctx != nil {
					batch0.mu.Unlock()
					recBuf.mu.Unlock()
					s.recBufsMu.Unlock()
					return rctx
				}
			}
			batch0.mu.Unlock()
		}
		recBuf.mu.Unlock()
	}
	s.recBufsMu.Unlock()
	return context.Background()
}

func (s *sink) produce(sem <-chan struct{}) bool {
	var produced bool
	defer func() {
		if !produced {
			<-sem
		}
	}()

	// We could have been triggered from a metadata update even though the
	// user is not producing at all. If we have no buffered records, let's
	// avoid potentially creating a producer ID.
	if s.cl.BufferedProduceRecords() == 0 {
		return false
	}

	// producerID can fail from:
	// - retry failure
	// - auth failure
	// - transactional: a produce failure that failed the producer ID
	// - AddPartitionsToTxn failure (see just below)
	// - some head-of-line context failure
	//
	// All but the first error is fatal. Recovery may be possible with
	// EndTransaction in specific cases, but regardless, all buffered
	// records must fail.
	//
	// NOTE: we init the producer ID before creating a request to ensure we
	// are always using the latest id/epoch with the proper sequence
	// numbers. (i.e., resetAllSequenceNumbers && producerID logic combo).
	//
	// For the first-discovered-record-head-of-line context, we want to
	// avoid looking it up if possible (which is why producerID takes a
	// ctxFn). If we do use one, we want to be sure that the
	// context.Canceled error is from *that* context rather than the client
	// context or something else. So, we go through some special care to
	// track setting the ctx / looking up if it is canceled.
	var holCtxMu sync.Mutex
	var holCtx context.Context
	ctxFn := func() context.Context {
		holCtxMu.Lock()
		defer holCtxMu.Unlock()
		holCtx = s.anyCtx()
		return holCtx
	}
	isHolCtxDone := func() bool {
		holCtxMu.Lock()
		defer holCtxMu.Unlock()
		if holCtx == nil {
			return false
		}
		select {
		case <-holCtx.Done():
			return true
		default:
		}
		return false
	}

	id, epoch, err := s.cl.producerID(ctxFn)
	if err != nil {
		var pe *errProducerIDLoadFail
		switch {
		case errors.As(err, &pe):
			if errors.Is(pe.err, context.Canceled) && isHolCtxDone() {
				// Some head-of-line record in a partition had a context cancelation.
				// We look for any partition with HOL cancelations and fail them all.
				s.cl.cfg.logger.Log(LogLevelInfo, "the first record in some partition(s) had a context cancelation; failing all relevant partitions", "broker", logID(s.nodeID))
				s.recBufsMu.Lock()
				defer s.recBufsMu.Unlock()
				for _, recBuf := range s.recBufs {
					recBuf.mu.Lock()
					var failAll bool
					if len(recBuf.batches) > 0 {
						batch0 := recBuf.batches[0]
						batch0.mu.Lock()
						if batch0.canFailFromLoadErrs && len(batch0.records) > 0 {
							r0 := batch0.records[0]
							if rctx := r0.cancelingCtx(); rctx != nil {
								select {
								case <-rctx.Done():
									failAll = true // we must not call failAllRecords here, because failAllRecords locks batches!
								default:
								}
							}
						}
						batch0.mu.Unlock()
					}
					if failAll {
						recBuf.failAllRecords(err)
					}
					recBuf.mu.Unlock()
				}
				return true
			}
			s.cl.bumpRepeatedLoadErr(err)
			s.cl.cfg.logger.Log(LogLevelWarn, "unable to load producer ID, bumping client's buffered record load errors by 1 and retrying")
			return true // whatever caused our produce, we did nothing, so keep going
		case errors.Is(err, ErrClientClosed):
			s.cl.failBufferedRecords(err)
		default:
			s.cl.cfg.logger.Log(LogLevelError, "fatal InitProducerID error, failing all buffered records", "broker", logID(s.nodeID), "err", err)
			s.cl.failBufferedRecords(err)
		}
		return false
	}

	if !s.cl.producer.maybeAddInflight() { // must do before marking recBufs on a txn
		return false
	}
	defer func() {
		if !produced {
			s.cl.producer.decInflight()
		}
	}()

	// NOTE: we create the req AFTER getting our producer ID!
	//
	// If a prior response caused errReloadProducerID, then calling
	// producerID() sets needSeqReset, and creating the request resets
	// sequence numbers. We need to have that logic occur before we create
	// the request, otherwise we will create a request with the old
	// sequence numbers using our new producer ID, which will then again
	// fail with OOOSN.
	req, txnReq, moreToDrain := s.createReq(id, epoch)
	if len(req.batches) == 0 { // everything was failing or lingering
		return moreToDrain
	}

	if txnReq != nil {
		// txnReq can fail from:
		// - retry failure
		// - auth failure
		// - producer id mapping / epoch errors
		// The latter case can potentially recover with the kip logic
		// we have defined in EndTransaction. Regardless, on failure
		// here, all buffered records must fail.
		// We do not need to clear the addedToTxn flag for any recBuf
		// it was set on, since producer id recovery resets the flag.
		batchesStripped, err := s.doTxnReq(req, txnReq)
		if err != nil {
			switch {
			case isRetryableBrokerErr(err) || isDialNonTimeoutErr(err):
				s.cl.bumpRepeatedLoadErr(err)
				s.cl.cfg.logger.Log(LogLevelWarn, "unable to AddPartitionsToTxn due to retryable broker err, bumping client's buffered record load errors by 1 and retrying", "err", err)
				s.cl.triggerUpdateMetadata(false, "attempting to refresh broker list due to failed AddPartitionsToTxn requests")
				return moreToDrain || len(req.batches) > 0 // nothing stripped if request-issuing error
			default:
				// Note that err can be InvalidProducerEpoch, which is
				// potentially recoverable in EndTransaction.
				//
				// We do not fail all buffered records here,
				// because that can lead to undesirable behavior
				// with produce request vs. end txn (KAFKA-12671)
				s.cl.failProducerID(id, epoch, err)
				s.cl.cfg.logger.Log(LogLevelError, "fatal AddPartitionsToTxn error, failing all buffered records (it is possible the client can recover after EndTransaction)", "broker", logID(s.nodeID), "err", err)
			}
			return false
		}

		// If we stripped everything, ensure we backoff to force a
		// metadata load. If not everything was stripped, we issue our
		// request and ensure we will retry a producing until
		// everything is stripped (and we eventually back off).
		if batchesStripped {
			moreToDrain = true
			if len(req.batches) == 0 {
				s.maybeTriggerBackoff(s.backoffSeq)
			}
		}
	}

	if len(req.batches) == 0 { // txn req could have removed some partitions to retry later (unknown topic, etc.)
		return moreToDrain
	}

	req.backoffSeq = s.backoffSeq // safe to read outside mu since we are in drain loop

	produced = true

	batches := req.batches.sliced()
	s.doSequenced(req, func(br *broker, resp kmsg.Response, err error) {
		s.handleReqResp(br, req, resp, err)
		s.cl.producer.decInflight()
		batches.eachOwnerLocked((*recBatch).decInflight)
		<-sem
	})
	return moreToDrain
}

// With handleSeqResps below, this function ensures that all request responses
// are handled in order. We use this guarantee while in handleReqResp below.
func (s *sink) doSequenced(
	req kmsg.Request,
	promise func(*broker, kmsg.Response, error),
) {
	wait := &seqResp{
		done:    make(chan struct{}),
		promise: promise,
	}

	// We can NOT use any record context. If we do, we force the request to
	// fail while also force the batch to be unfailable (due to no
	// response),
	br, err := s.cl.brokerOrErr(s.cl.ctx, s.nodeID, errUnknownBroker)
	if err != nil {
		wait.err = err
		close(wait.done)
	} else {
		br.do(s.cl.ctx, req, func(resp kmsg.Response, err error) {
			wait.resp = resp
			wait.err = err
			close(wait.done)
		})
		wait.br = br
	}

	if first := s.seqResps.push(wait); first {
		go s.handleSeqResps(wait)
	}
}

// Ensures that all request responses are processed in order.
func (s *sink) handleSeqResps(wait *seqResp) {
	var more bool
start:
	<-wait.done
	wait.promise(wait.br, wait.resp, wait.err)

	wait, more = s.seqResps.dropPeek()
	if more {
		goto start
	}
}

// Issues an AddPartitionsToTxnRequest before a produce request for all
// partitions that need to be added to a transaction.
func (s *sink) doTxnReq(
	req *produceRequest,
	txnReq *kmsg.AddPartitionsToTxnRequest,
) (stripped bool, err error) {
	// If we return an unretryable error, then we have to reset everything
	// to not be in the transaction and begin draining at the start.
	//
	// These batches must be the first in their recBuf, because we would
	// not be trying to add them to a partition if they were not.
	defer func() {
		if err != nil {
			req.batches.eachOwnerLocked(seqRecBatch.removeFromTxn)
		}
	}()
	// We do NOT let record context cancelations fail this request: doing
	// so would put the transactional ID in an unknown state. This is
	// similar to the warning we give in the txn.go file, but the
	// difference there is the user knows explicitly at the function call
	// that canceling the context will opt them into invalid state.
	err = s.cl.doWithConcurrentTransactions(s.cl.ctx, "AddPartitionsToTxn", func() error {
		stripped, err = s.issueTxnReq(req, txnReq)
		return err
	})
	return stripped, err
}

// Removing a batch from the transaction means we will not be issuing it
// inflight, and that it was not added to the txn and that we need to reset the
// drain index.
func (b *recBatch) removeFromTxn() {
	b.owner.addedToTxn.Store(false)
	b.owner.resetBatchDrainIdx()
	b.decInflight()
}

func (s *sink) issueTxnReq(
	req *produceRequest,
	txnReq *kmsg.AddPartitionsToTxnRequest,
) (stripped bool, fatalErr error) {
	resp, err := txnReq.RequestWith(s.cl.ctx, s.cl)
	if err != nil {
		return false, err
	}

	for _, topic := range resp.Topics {
		topicBatches, ok := req.batches[topic.Topic]
		if !ok {
			s.cl.cfg.logger.Log(LogLevelError, "broker replied with topic in AddPartitionsToTxnResponse that was not in request", "topic", topic.Topic)
			continue
		}
		for _, partition := range topic.Partitions {
			if err := kerr.ErrorForCode(partition.ErrorCode); err != nil {
				// OperationNotAttempted is set for all partitions that are authorized
				// if any partition is unauthorized _or_ does not exist. We simply remove
				// unattempted partitions and treat them as retryable.
				if !kerr.IsRetriable(err) && !errors.Is(err, kerr.OperationNotAttempted) {
					fatalErr = err // auth err, etc
					continue
				}

				batch, ok := topicBatches[partition.Partition]
				if !ok {
					s.cl.cfg.logger.Log(LogLevelError, "broker replied with partition in AddPartitionsToTxnResponse that was not in request", "topic", topic.Topic, "partition", partition.Partition)
					continue
				}

				// We are stripping this retryable-err batch from the request,
				// so we must reset that it has been added to the txn.
				batch.owner.mu.Lock()
				batch.removeFromTxn()
				batch.owner.mu.Unlock()

				stripped = true

				delete(topicBatches, partition.Partition)
			}
			if len(topicBatches) == 0 {
				delete(req.batches, topic.Topic)
			}
		}
	}
	return stripped, fatalErr
}

// firstRespCheck is effectively a sink.Once. On the first response, if the
// used request version is at least 4, we upgrade our inflight sem.
//
// Starting on version 4, Kafka allowed five inflight requests while
// maintaining idempotency. Before, only one was allowed.
//
// We go through an atomic because drain can be waiting on the sem (with
// capacity one). We store four here, meaning new drain loops will load the
// higher capacity sem without read/write pointer racing a current loop.
//
// This logic does mean that we will never use the full potential 5 in flight
// outside of a small window during the store, but some pages in the Kafka
// confluence basically show that more than two in flight has marginal benefit
// anyway (although that may be due to their Java API).
//
//	https://cwiki.apache.org/confluence/display/KAFKA/An+analysis+of+the+impact+of+max.in.flight.requests.per.connection+and+acks+on+Producer+performance
//	https://issues.apache.org/jira/browse/KAFKA-5494
func (s *sink) firstRespCheck(idempotent bool, version int16) {
	if s.produceVersion.Load() < 0 {
		s.produceVersion.Store(int32(version))
		if idempotent && version >= 4 {
			s.inflightSem.Store(make(chan struct{}, 4))
		}
	}
}

// handleReqClientErr is called when the client errors before receiving a
// produce response.
func (s *sink) handleReqClientErr(req *produceRequest, err error) {
	switch {
	default:
		s.cl.cfg.logger.Log(LogLevelWarn, "random error while producing, requeueing unattempted request", "broker", logID(s.nodeID), "err", err)
		fallthrough

	case errors.Is(err, errUnknownBroker),
		isDialNonTimeoutErr(err),
		isRetryableBrokerErr(err):
		updateMeta := !isRetryableBrokerErr(err)
		if updateMeta {
			s.cl.cfg.logger.Log(LogLevelInfo, "produce request failed, triggering metadata update", "broker", logID(s.nodeID), "err", err)
		}
		s.handleRetryBatches(req.batches, nil, req.backoffSeq, updateMeta, false, "failed produce request triggered metadata update")

	case errors.Is(err, ErrClientClosed):
		s.cl.failBufferedRecords(ErrClientClosed)
	}
}

// No acks mean no response. The following block is basically an extremely
// condensed version of the logic in handleReqResp.
func (s *sink) handleReqRespNoack(b *bytes.Buffer, debug bool, req *produceRequest) {
	if debug {
		fmt.Fprintf(b, "noack ")
	}
	for topic, partitions := range req.batches {
		if debug {
			fmt.Fprintf(b, "%s[", topic)
		}
		for partition, batch := range partitions {
			batch.owner.mu.Lock()
			if batch.isOwnersFirstBatch() {
				if debug {
					fmt.Fprintf(b, "%d{0=>%d}, ", partition, len(batch.records))
				}
				s.cl.finishBatch(batch.recBatch, req.producerID, req.producerEpoch, partition, 0, nil)
			} else if debug {
				fmt.Fprintf(b, "%d{skipped}, ", partition)
			}
			batch.owner.mu.Unlock()
		}
		if debug {
			if bytes.HasSuffix(b.Bytes(), []byte(", ")) {
				b.Truncate(b.Len() - 2)
			}
			b.WriteString("], ")
		}
	}
}

func (s *sink) handleReqResp(br *broker, req *produceRequest, resp kmsg.Response, err error) {
	if err != nil {
		s.handleReqClientErr(req, err)
		return
	}
	s.firstRespCheck(req.idempotent(), req.version)
	s.consecutiveFailures.Store(0)
	defer req.metrics.hook(&s.cl.cfg, br) // defer to end so that non-written batches are removed

	var b *bytes.Buffer
	debug := s.cl.cfg.logger.Level() >= LogLevelDebug
	if debug {
		b = bytes.NewBuffer(make([]byte, 0, 128))
		defer func() {
			update := b.String()
			update = strings.TrimSuffix(update, ", ")
			s.cl.cfg.logger.Log(LogLevelDebug, "produced", "broker", logID(s.nodeID), "to", update)
		}()
	}

	if req.acks == 0 {
		s.handleReqRespNoack(b, debug, req)
		return
	}

	var kmove kip951move
	var reqRetry seqRecBatches // handled at the end

	kresp := resp.(*kmsg.ProduceResponse)
	for i := range kresp.Topics {
		rt := &kresp.Topics[i]
		topic := rt.Topic
		partitions, ok := req.batches[topic]
		if !ok {
			s.cl.cfg.logger.Log(LogLevelError, "broker erroneously replied with topic in produce request that we did not produce to", "broker", logID(s.nodeID), "topic", topic)
			delete(req.metrics, topic)
			continue // should not hit this
		}

		if debug {
			fmt.Fprintf(b, "%s[", topic)
		}

		tmetrics := req.metrics[topic]
		for j := range rt.Partitions {
			rp := &rt.Partitions[j]
			partition := rp.Partition
			batch, ok := partitions[partition]
			if !ok {
				s.cl.cfg.logger.Log(LogLevelError, "broker erroneously replied with partition in produce request that we did not produce to", "broker", logID(s.nodeID), "topic", rt.Topic, "partition", partition)
				delete(tmetrics, partition)
				continue // should not hit this
			}
			delete(partitions, partition)

			retry, didProduce := s.handleReqRespBatch(
				b,
				&kmove,
				kresp,
				topic,
				rp,
				batch,
				req.producerID,
				req.producerEpoch,
			)
			if retry {
				reqRetry.addSeqBatch(topic, partition, batch)
			}
			if !didProduce {
				delete(tmetrics, partition)
			}
		}

		if debug {
			if bytes.HasSuffix(b.Bytes(), []byte(", ")) {
				b.Truncate(b.Len() - 2)
			}
			b.WriteString("], ")
		}

		if len(partitions) == 0 {
			delete(req.batches, topic)
		}
	}

	if len(req.batches) > 0 {
		s.cl.cfg.logger.Log(LogLevelError, "broker did not reply to all topics / partitions in the produce request! reenqueuing missing partitions", "broker", logID(s.nodeID))
		s.handleRetryBatches(req.batches, nil, 0, true, false, "broker did not reply to all topics in produce request")
	}
	if len(reqRetry) > 0 {
		s.handleRetryBatches(reqRetry, &kmove, 0, true, true, "produce request had retry batches")
	}
}

func (s *sink) handleReqRespBatch(
	b *bytes.Buffer,
	kmove *kip951move,
	resp *kmsg.ProduceResponse,
	topic string,
	rp *kmsg.ProduceResponseTopicPartition,
	batch seqRecBatch,
	producerID int64,
	producerEpoch int16,
) (retry, didProduce bool) {
	batch.owner.mu.Lock()
	defer batch.owner.mu.Unlock()

	nrec := len(batch.records)

	debug := b != nil
	if debug {
		fmt.Fprintf(b, "%d{", rp.Partition)
	}

	// We only ever operate on the first batch in a record buf. Batches
	// work sequentially; if this is not the first batch then an error
	// happened and this later batch is no longer a part of a seq chain.
	if !batch.isOwnersFirstBatch() {
		if debug {
			if err := kerr.ErrorForCode(rp.ErrorCode); err == nil {
				if nrec > 0 {
					fmt.Fprintf(b, "skipped@%d=>%d}, ", rp.BaseOffset, rp.BaseOffset+int64(nrec))
				} else {
					fmt.Fprintf(b, "skipped@%d}, ", rp.BaseOffset)
				}
			} else {
				if nrec > 0 {
					fmt.Fprintf(b, "skipped@%d,%d(%s)}, ", rp.BaseOffset, nrec, err)
				} else {
					fmt.Fprintf(b, "skipped@%d(%s)}, ", rp.BaseOffset, err)
				}
			}
		}
		return false, false
	}

	// Since we have received a response and we are the first batch, we can
	// at this point re-enable failing from load errors.
	//
	// We do not need a lock since the owner is locked.
	batch.canFailFromLoadErrs = true

	// By default, we assume we errored. Non-error updates this back
	// to true.
	batch.owner.okOnSink = false

	if moving := kmove.maybeAddProducePartition(resp, rp, batch.owner); moving {
		if debug {
			fmt.Fprintf(b, "move:%d:%d@%d,%d}, ", rp.CurrentLeader.LeaderID, rp.CurrentLeader.LeaderEpoch, rp.BaseOffset, nrec)
		}
		batch.owner.failing = true
		return true, false
	}

	err := kerr.ErrorForCode(rp.ErrorCode)
	failUnknown := batch.owner.checkUnknownFailLimit(err)
	switch {
	case kerr.IsRetriable(err) &&
		!failUnknown &&
		err != kerr.CorruptMessage &&
		batch.tries < s.cl.cfg.recordRetries:

		if debug {
			fmt.Fprintf(b, "retrying@%d,%d(%s)}, ", rp.BaseOffset, nrec, err)
		}
		return true, false

	case err == kerr.OutOfOrderSequenceNumber,
		err == kerr.UnknownProducerID,
		err == kerr.InvalidProducerIDMapping,
		err == kerr.InvalidProducerEpoch:

		// OOOSN always means data loss 1.0+ and is ambiguous prior.
		// We assume the worst and only continue if requested.
		//
		// UnknownProducerID was introduced to allow some form of safe
		// handling, but KIP-360 demonstrated that resetting sequence
		// numbers is fundamentally unsafe, so we treat it like OOOSN.
		//
		// InvalidMapping is similar to UnknownProducerID, but occurs
		// when the txnal coordinator timed out our transaction.
		//
		// 2.5
		// =====
		// 2.5 introduced some behavior to potentially safely reset
		// the sequence numbers by bumping an epoch (see KIP-360).
		//
		// For the idempotent producer, the solution is to fail all
		// buffered records and then let the client user reset things
		// with the understanding that they cannot guard against
		// potential dups / reordering at that point. Realistically,
		// that's no better than a config knob that allows the user
		// to continue (our stopOnDataLoss flag), so for the idempotent
		// producer, if stopOnDataLoss is false, we just continue.
		//
		// For the transactional producer, we always fail the producerID.
		// EndTransaction will trigger recovery if possible.
		//
		// 2.7
		// =====
		// InvalidProducerEpoch became retryable in 2.7. Prior, it
		// was ambiguous (timeout? fenced?). Now, InvalidProducerEpoch
		// is only returned on produce, and then we can recover on other
		// txn coordinator requests, which have PRODUCER_FENCED vs
		// TRANSACTION_TIMED_OUT.

		if s.cl.cfg.txnID != nil || s.cl.cfg.stopOnDataLoss {
			s.cl.cfg.logger.Log(LogLevelInfo, "batch errored, failing the producer ID",
				"broker", logID(s.nodeID),
				"topic", topic,
				"partition", rp.Partition,
				"producer_id", producerID,
				"producer_epoch", producerEpoch,
				"err", err,
			)
			s.cl.failProducerID(producerID, producerEpoch, err)

			s.cl.finishBatch(batch.recBatch, producerID, producerEpoch, rp.Partition, rp.BaseOffset, err)
			if debug {
				fmt.Fprintf(b, "fatal@%d,%d(%s)}, ", rp.BaseOffset, nrec, err)
			}
			return false, false
		}
		if s.cl.cfg.onDataLoss != nil {
			s.cl.cfg.onDataLoss(topic, rp.Partition)
		}

		// For OOOSN, and UnknownProducerID
		//
		// The only recovery is to fail the producer ID, which ensures
		// that all batches reset sequence numbers and use a new producer
		// ID on the next batch.
		//
		// For InvalidProducerIDMapping && InvalidProducerEpoch,
		//
		// We should not be here, since this error occurs in the
		// context of transactions, which are caught above.
		s.cl.cfg.logger.Log(LogLevelInfo, fmt.Sprintf("batch errored with %s, failing the producer ID and resetting all sequence numbers", err.(*kerr.Error).Message),
			"broker", logID(s.nodeID),
			"topic", topic,
			"partition", rp.Partition,
			"producer_id", producerID,
			"producer_epoch", producerEpoch,
			"err", err,
		)

		// After we fail here, any new produce (even new ones
		// happening concurrent with this function) will load
		// a new epoch-bumped producer ID and all first-batches
		// will reset sequence numbers appropriately.
		s.cl.failProducerID(producerID, producerEpoch, errReloadProducerID)
		if debug {
			fmt.Fprintf(b, "resetting@%d,%d(%s)}, ", rp.BaseOffset, nrec, err)
		}
		return true, false

	case err == kerr.DuplicateSequenceNumber: // ignorable, but we should not get
		s.cl.cfg.logger.Log(LogLevelInfo, "received unexpected duplicate sequence number, ignoring and treating batch as successful",
			"broker", logID(s.nodeID),
			"topic", topic,
			"partition", rp.Partition,
		)
		err = nil
		fallthrough
	default:
		if err != nil {
			s.cl.cfg.logger.Log(LogLevelInfo, "batch in a produce request failed",
				"broker", logID(s.nodeID),
				"topic", topic,
				"partition", rp.Partition,
				"err", err,
				"err_is_retryable", kerr.IsRetriable(err),
				"max_retries_reached", !failUnknown && batch.tries >= s.cl.cfg.recordRetries,
			)
		} else {
			batch.owner.okOnSink = true
		}
		s.cl.finishBatch(batch.recBatch, producerID, producerEpoch, rp.Partition, rp.BaseOffset, err)
		didProduce = err == nil
		if debug {
			if err != nil {
				fmt.Fprintf(b, "err@%d,%d(%s)}, ", rp.BaseOffset, nrec, err)
			} else {
				fmt.Fprintf(b, "%d=>%d}, ", rp.BaseOffset, rp.BaseOffset+int64(nrec))
			}
		}
	}
	return false, didProduce // no retry
}

// finishBatch removes a batch from its owning record buffer and finishes all
// records in the batch.
//
// This is safe even if the owning recBuf migrated sinks, since we are
// finishing based off the status of an inflight req from the original sink.
func (cl *Client) finishBatch(batch *recBatch, producerID int64, producerEpoch int16, partition int32, baseOffset int64, err error) {
	recBuf := batch.owner

	if err != nil {
		// We know that Kafka replied this batch is a failure. We can
		// fail this batch and all batches in this partition.
		// This will keep sequence numbers correct.
		recBuf.failAllRecords(err)
		return
	}

	// We know the batch made it to Kafka successfully without error.
	// We remove this batch and finish all records appropriately.
	finished := len(batch.records)
	recBuf.batch0Seq = incrementSequence(recBuf.batch0Seq, int32(finished))
	recBuf.buffered.Add(-int64(finished))
	recBuf.batches[0] = nil
	recBuf.batches = recBuf.batches[1:]
	recBuf.batchDrainIdx--

	batch.mu.Lock()
	records, attrs := batch.records, batch.attrs
	batch.records = nil
	batch.mu.Unlock()

	cl.producer.promiseBatch(batchPromise{
		baseOffset: baseOffset,
		pid:        producerID,
		epoch:      producerEpoch,
		// A recBuf.attrs is updated when appending to be written. For
		// v0 && v1 produce requests, we set bit 8 in the attrs
		// corresponding to our own RecordAttr's bit 8 being no
		// timestamp type. Thus, we can directly convert the batch
		// attrs to our own RecordAttrs.
		attrs:     RecordAttrs{uint8(attrs)},
		partition: partition,
		recs:      records,
	})
}

// handleRetryBatches sets any first-buf-batch to failing and triggers a
// metadata that will eventually clear the failing state and re-drain.
//
// If idempotency is disabled, if a batch is timed out or hit the retry limit,
// we fail it and anything after it.
func (s *sink) handleRetryBatches(
	retry seqRecBatches,
	kmove *kip951move,
	backoffSeq uint32,
	updateMeta bool, // if we should maybe update the metadata
	canFail bool, // if records can fail if they are at limits
	why string,
) {
	logger := s.cl.cfg.logger
	debug := logger.Level() >= LogLevelDebug
	var needsMetaUpdate bool
	var shouldBackoff bool
	if kmove != nil {
		defer kmove.maybeBeginMove(s.cl)
	}
	var numRetryBatches, numMoveBatches int
	retry.eachOwnerLocked(func(batch seqRecBatch) {
		numRetryBatches++
		if !batch.isOwnersFirstBatch() {
			if debug {
				logger.Log(LogLevelDebug, "retry batch is not the first batch in the owner, skipping result",
					"topic", batch.owner.topic,
					"partition", batch.owner.partition,
				)
			}
			return
		}

		// If the request failed due to a concurrent metadata update
		// moving partitions to a different sink (or killing the sink
		// this partition was on), we can just reset the drain index
		// and trigger draining now the new sink. There is no reason
		// to backoff on this sink nor trigger a metadata update.
		if batch.owner.sink != s {
			if debug {
				logger.Log(LogLevelDebug, "transitioned sinks while a request was inflight, retrying immediately on new sink without backoff",
					"topic", batch.owner.topic,
					"partition", batch.owner.partition,
					"old_sink", s.nodeID,
					"new_sink", batch.owner.sink.nodeID,
				)
			}
			batch.owner.resetBatchDrainIdx()
			return
		}

		if canFail || s.cl.cfg.disableIdempotency {
			if err := batch.maybeFailErr(&s.cl.cfg); err != nil {
				batch.owner.failAllRecords(err)
				return
			}
		}

		batch.owner.resetBatchDrainIdx()

		// Now that the batch drain index is reset, if this retry is
		// caused from a moving batch, return early. We do  not need
		// to backoff nor do we need to trigger a metadata update.
		if kmove.hasRecBuf(batch.owner) {
			numMoveBatches++
			return
		}

		// If our first batch (seq == 0) fails with unknown topic, we
		// retry immediately. Kafka can reply with valid metadata
		// immediately after a topic was created, before the leaders
		// actually know they are leader.
		unknownAndFirstBatch := batch.owner.unknownFailures == 1 && batch.owner.seq == 0

		if unknownAndFirstBatch {
			shouldBackoff = true
			return
		}
		if updateMeta {
			batch.owner.failing = true
			needsMetaUpdate = true
		}
	})

	if debug {
		logger.Log(LogLevelDebug, "retry batches processed",
			"wanted_metadata_update", updateMeta,
			"triggering_metadata_update", needsMetaUpdate,
			"should_backoff", shouldBackoff,
		)
	}

	// If we do want to metadata update, we only do so if any batch was the
	// first batch in its buf / not concurrently failed.
	if needsMetaUpdate {
		s.cl.triggerUpdateMetadata(true, why)
		return
	}

	// We could not need a metadata update for two reasons:
	//
	//   * our request died when being issued
	//
	//   * we would update metadata, but what failed was the first batch
	//     produced and the error was unknown topic / partition.
	//
	// In either of these cases, we should backoff a little bit to avoid
	// spin looping.
	//
	// If neither of these cases are true, then we entered wanting a
	// metadata update, but the batches either were not the first batch, or
	// the batches were concurrently failed.
	//
	// If all partitions are moving, we do not need to backoff nor drain.
	if shouldBackoff || (!updateMeta && numRetryBatches != numMoveBatches) {
		s.maybeTriggerBackoff(backoffSeq)
		s.maybeDrain()
	}
}

// addRecBuf adds a new record buffer to be drained to a sink and clears the
// buffer's failing state.
func (s *sink) addRecBuf(add *recBuf) {
	s.recBufsMu.Lock()
	add.recBufsIdx = len(s.recBufs)
	s.recBufs = append(s.recBufs, add)
	s.recBufsMu.Unlock()

	add.clearFailing()
}

// removeRecBuf removes a record buffer from a sink.
func (s *sink) removeRecBuf(rm *recBuf) {
	s.recBufsMu.Lock()
	defer s.recBufsMu.Unlock()

	if rm.recBufsIdx != len(s.recBufs)-1 {
		s.recBufs[rm.recBufsIdx], s.recBufs[len(s.recBufs)-1] = s.recBufs[len(s.recBufs)-1], nil
		s.recBufs[rm.recBufsIdx].recBufsIdx = rm.recBufsIdx
	} else {
		s.recBufs[rm.recBufsIdx] = nil // do not let this removal hang around
	}

	s.recBufs = s.recBufs[:len(s.recBufs)-1]
	if s.recBufsStart == len(s.recBufs) {
		s.recBufsStart = 0
	}
}

// recBuf is a buffer of records being produced to a partition and being
// drained by a sink. This is only not drained if the partition has a load
// error and thus does not a have a sink to be drained into.
type recBuf struct {
	cl *Client // for cfg, record finishing

	topic     string
	partition int32

	// The number of bytes we can buffer in a batch for this particular
	// topic/partition. This may be less than the configured
	// maxRecordBatchBytes because of produce request overhead.
	maxRecordBatchBytes int32

	// addedToTxn, for transactions only, signifies whether this partition
	// has been added to the transaction yet or not.
	addedToTxn atomicBool

	// For LoadTopicPartitioner partitioning; atomically tracks the number
	// of records buffered in total on this recBuf.
	buffered atomicI64

	mu sync.Mutex // guards r/w access to all fields below

	// sink is who is currently draining us. This can be modified
	// concurrently during a metadata update.
	//
	// The first set to a non-nil sink is done without a mutex.
	//
	// Since only metadata updates can change the sink, metadata updates
	// also read this without a mutex.
	sink *sink
	// recBufsIdx is our index into our current sink's recBufs field.
	// This exists to aid in removing the buffer from the sink.
	recBufsIdx int

	// A concurrent metadata update can move a recBuf from one sink to
	// another while requests are inflight on the original sink. We do not
	// want to allow new requests to start on the new sink until they all
	// finish on the old, because with some pathological request order
	// finishing, we would allow requests to finish out of order:
	// handleSeqResps works per sink, not across sinks.
	inflightOnSink *sink
	// We only want to allow more than 1 inflight on a sink *if* we are
	// currently receiving successful responses. Unimportantly, this allows
	// us to save resources if the broker is having a problem or just
	// recovered from one. Importantly, we work around an edge case in
	// Kafka. Kafka will accept the first produce request for a pid/epoch
	// with *any* sequence number. Say we sent two requests inflight. The
	// first request Kafka replies to with NOT_LEADER_FOR_PARTITION, the
	// second, the broker finished setting up and accepts. The broker now
	// has the second request but not the first, we will retry both
	// requests and receive OOOSN, and the broker has logs out of order.
	// By only allowing more than one inflight if we have seen an ok
	// response, we largely eliminate risk of this problem. See #223 for
	// more details.
	okOnSink bool
	// Inflight tracks the number of requests inflight using batches from
	// this recBuf. Every time this hits zero, if the batchDrainIdx is not
	// at the end, we clear inflightOnSink and trigger the *current* sink
	// to drain.
	inflight uint8

	topicPartitionData // updated in metadata migrateProductionTo (same spot sink is updated)

	// seq is used for the seq in each record batch. It is incremented when
	// produce requests are made and can be reset on errors to batch0Seq.
	//
	// If idempotency is disabled, we just use "0" for the first sequence
	// when encoding our payload.
	//
	// This is also used to check the first batch produced (disregarding
	// seq resets) -- see handleRetryBatches.
	seq int32
	// batch0Seq is the seq of the batch at batchDrainIdx 0. If we reset
	// the drain index, we reset seq with this number. If we successfully
	// finish batch 0, we bump this.
	batch0Seq int32
	// If we need to reset sequence numbers, we set needSeqReset, and then
	// when we use the **first** batch, we reset sequences to 0.
	needSeqReset bool

	// batches is our list of buffered records. Batches are appended as the
	// final batch crosses size thresholds or as drain freezes batches from
	// further modification.
	//
	// Most functions in a sink only operate on a batch if the batch is the
	// first batch in a buffer. This is necessary to ensure that all
	// records are truly finished without error in order.
	batches []*recBatch
	// batchDrainIdx is where the next batch will drain from. We only
	// remove from the head of batches when a batch is finished.
	// This is read while buffering and modified in a few places.
	batchDrainIdx int

	// If we fail with UNKNOWN_TOPIC_OR_PARTITION, we bump this and fail
	// all records once this exceeds the config's unknown topic fail limit.
	// If we ever see a different error (or no error), this is reset.
	unknownFailures int64

	// lingering is a timer that avoids starting maybeDrain until expiry,
	// allowing for more records to be buffered in a single batch.
	//
	// Note that if something else starts a drain, if the first batch of
	// this buffer fits into the request, it will be used.
	//
	// This is on recBuf rather than Sink to avoid some complicated
	// interactions of triggering the sink to loop or not. Ideally, with
	// the sticky partition hashers, we will only have a few partitions
	// lingering and that this is on a RecBuf should not matter.
	lingering *time.Timer

	// failing is set when we encounter a temporary partition error during
	// producing, such as UnknownTopicOrPartition (signifying the partition
	// moved to a different broker).
	//
	// It is always cleared on metadata update.
	failing bool

	// Only possibly set in PurgeTopics, this is used to fail anything that
	// was in the process of being buffered.
	purged bool
}

// bufferRecord usually buffers a record, but does not if abortOnNewBatch is
// true and if this function would create a new batch.
//
// This returns whether the promised record was processed or not (buffered or
// immediately errored).
func (recBuf *recBuf) bufferRecord(pr promisedRec, abortOnNewBatch bool) bool {
	recBuf.mu.Lock()
	defer recBuf.mu.Unlock()

	// We truncate to milliseconds to avoid some accumulated rounding error
	// problems (see IBM/sarama#1455)
	if pr.Timestamp.IsZero() {
		pr.Timestamp = time.Now()
	}
	pr.Timestamp = pr.Timestamp.Truncate(time.Millisecond)
	pr.Partition = recBuf.partition // set now, for the hook below

	if recBuf.purged {
		recBuf.cl.producer.promiseRecord(pr, errPurged)
		return true
	}

	var (
		newBatch       = true
		onDrainBatch   = recBuf.batchDrainIdx == len(recBuf.batches)
		produceVersion = recBuf.sink.produceVersion.Load()
	)

	if !onDrainBatch {
		batch := recBuf.batches[len(recBuf.batches)-1]
		appended, _ := batch.tryBuffer(pr, produceVersion, recBuf.maxRecordBatchBytes, false)
		newBatch = !appended
	}

	if newBatch {
		newBatch := recBuf.newRecordBatch()
		appended, aborted := newBatch.tryBuffer(pr, produceVersion, recBuf.maxRecordBatchBytes, abortOnNewBatch)

		switch {
		case aborted: // not processed
			return false
		case appended: // we return true below
		default: // processed as failure
			recBuf.cl.producer.promiseRecord(pr, kerr.MessageTooLarge)
			return true
		}

		recBuf.batches = append(recBuf.batches, newBatch)
	}

	if recBuf.cl.cfg.linger == 0 {
		if onDrainBatch {
			recBuf.sink.maybeDrain()
		}
	} else {
		// With linger, if this is a new batch but not the first, we
		// stop lingering and begin draining. The drain loop will
		// restart our linger once this buffer has one batch left.
		if newBatch && !onDrainBatch ||
			// If this is the first batch, try lingering; if
			// we cannot, we are being flushed and must drain.
			onDrainBatch && !recBuf.lockedMaybeStartLinger() {
			recBuf.lockedStopLinger()
			recBuf.sink.maybeDrain()
		}
	}

	recBuf.buffered.Add(1)

	if recBuf.cl.producer.hooks != nil && len(recBuf.cl.producer.hooks.partitioned) > 0 {
		for _, h := range recBuf.cl.producer.hooks.partitioned {
			h.OnProduceRecordPartitioned(pr.Record, recBuf.sink.nodeID)
		}
	}

	return true
}

// Stops lingering, potentially restarting it, and returns whether there is
// more to drain.
//
// If lingering, if there are more than one batches ready, there is definitely
// more to drain and we should not linger. Otherwise, if we cannot restart
// lingering, then we are flushing and also indicate there is more to drain.
func (recBuf *recBuf) tryStopLingerForDraining() bool {
	recBuf.lockedStopLinger()
	canLinger := recBuf.cl.cfg.linger == 0
	moreToDrain := !canLinger && len(recBuf.batches) > recBuf.batchDrainIdx ||
		canLinger && (len(recBuf.batches) > recBuf.batchDrainIdx+1 ||
			len(recBuf.batches) == recBuf.batchDrainIdx+1 && !recBuf.lockedMaybeStartLinger())
	return moreToDrain
}

// Begins a linger timer unless the producer is being flushed.
func (recBuf *recBuf) lockedMaybeStartLinger() bool {
	if recBuf.cl.producer.flushing.Load() > 0 || recBuf.cl.producer.blocked.Load() > 0 {
		return false
	}
	recBuf.lingering = time.AfterFunc(recBuf.cl.cfg.linger, recBuf.sink.maybeDrain)
	return true
}

func (recBuf *recBuf) lockedStopLinger() {
	if recBuf.lingering != nil {
		recBuf.lingering.Stop()
		recBuf.lingering = nil
	}
}

func (recBuf *recBuf) unlingerAndManuallyDrain() {
	recBuf.mu.Lock()
	defer recBuf.mu.Unlock()
	recBuf.lockedStopLinger()
	recBuf.sink.maybeDrain()
}

// bumpRepeatedLoadErr is provided to bump a buffer's number of consecutive
// load errors during metadata updates.
//
// Partition load errors are generally temporary (leader/listener/replica not
// available), and this try bump is not expected to do much. If for some reason
// a partition errors for a long time and we are not idempotent, this function
// drops all buffered records.
func (recBuf *recBuf) bumpRepeatedLoadErr(err error) {
	recBuf.mu.Lock()
	defer recBuf.mu.Unlock()
	if len(recBuf.batches) == 0 {
		return
	}
	batch0 := recBuf.batches[0]
	batch0.tries++

	// We need to lock the batch as well because there could be a buffered
	// request about to be written. Writing requests only grabs the batch
	// mu, not the recBuf mu.
	batch0.mu.Lock()
	var (
		canFail        = !recBuf.cl.idempotent() || batch0.canFailFromLoadErrs // we can only fail if we are not idempotent or if we have no outstanding requests
		batch0Fail     = batch0.maybeFailErr(&recBuf.cl.cfg) != nil            // timeout, retries, or aborting
		netErr         = isRetryableBrokerErr(err) || isDialNonTimeoutErr(err) // we can fail if this is *not* a network error
		retryableKerr  = kerr.IsRetriable(err)                                 // we fail if this is not a retryable kerr,
		isUnknownLimit = recBuf.checkUnknownFailLimit(err)                     // or if it is, but it is UnknownTopicOrPartition and we are at our limit

		willFail = canFail && (batch0Fail || !netErr && (!retryableKerr || retryableKerr && isUnknownLimit))
	)
	batch0.isFailingFromLoadErr = willFail
	batch0.mu.Unlock()

	recBuf.cl.cfg.logger.Log(LogLevelWarn, "produce partition load error, bumping error count on first stored batch",
		"broker", logID(recBuf.sink.nodeID),
		"topic", recBuf.topic,
		"partition", recBuf.partition,
		"err", err,
		"can_fail", canFail,
		"batch0_should_fail", batch0Fail,
		"is_network_err", netErr,
		"is_retryable_kerr", retryableKerr,
		"is_unknown_limit", isUnknownLimit,
		"will_fail", willFail,
	)

	if willFail {
		recBuf.failAllRecords(err)
	}
}

// Called locked, if err is an unknown error, bumps our limit, otherwise resets
// it. This returns if we have reached or exceeded the limit.
func (recBuf *recBuf) checkUnknownFailLimit(err error) bool {
	if errors.Is(err, kerr.UnknownTopicOrPartition) {
		recBuf.unknownFailures++
	} else {
		recBuf.unknownFailures = 0
	}
	return recBuf.cl.cfg.maxUnknownFailures >= 0 && recBuf.unknownFailures > recBuf.cl.cfg.maxUnknownFailures
}

// failAllRecords fails all buffered records in this recBuf.
// This is used anywhere where we have to fail and remove an entire batch,
// if we just removed the one batch, the seq num chain would be broken.
//
//   - from fatal InitProducerID or AddPartitionsToTxn
//   - from client closing
//   - if not idempotent && hit retry / timeout limit
//   - if batch fails fatally when producing
func (recBuf *recBuf) failAllRecords(err error) {
	recBuf.lockedStopLinger()
	for _, batch := range recBuf.batches {
		// We need to guard our clearing of records against a
		// concurrent produceRequest's write, which can have this batch
		// buffered wile we are failing.
		//
		// We do not need to worry about concurrent recBuf
		// modifications to this batch because the recBuf is already
		// locked.
		batch.mu.Lock()
		records := batch.records
		batch.records = nil
		batch.mu.Unlock()

		recBuf.cl.producer.promiseBatch(batchPromise{
			recs: records,
			err:  err,
		})
	}
	recBuf.resetBatchDrainIdx()
	recBuf.buffered.Store(0)
	recBuf.batches = nil
}

// clearFailing clears a buffer's failing state if it is failing.
//
// This is called when a buffer is added to a sink (to clear a failing state
// from migrating buffers between sinks) or when a metadata update sees the
// sink is still on the same source.
func (recBuf *recBuf) clearFailing() {
	recBuf.mu.Lock()
	defer recBuf.mu.Unlock()

	recBuf.failing = false
	if len(recBuf.batches) != recBuf.batchDrainIdx {
		recBuf.sink.maybeDrain()
	}
}

func (recBuf *recBuf) resetBatchDrainIdx() {
	recBuf.seq = recBuf.batch0Seq
	recBuf.batchDrainIdx = 0
}

// promisedRec ties a record with the callback that will be called once
// a batch is finally written and receives a response.
type promisedRec struct {
	ctx     context.Context
	promise func(*Record, error)
	*Record
}

func (pr promisedRec) cancelingCtx() context.Context {
	if pr.ctx.Done() != nil {
		return pr.ctx
	}
	if pr.Context.Done() != nil {
		return pr.Context
	}
	return nil
}

// recBatch is the type used for buffering records before they are written.
type recBatch struct {
	owner *recBuf // who owns us

	tries int64 // if this was sent before and is thus now immutable

	// We can only fail a batch if we have never issued it, or we have
	// issued it and have received a response. If we do not receive a
	// response, we cannot know whether we actually wrote bytes that Kafka
	// processed or not. So, we set this to false every time we issue a
	// request with this batch, and then reset it to true whenever we
	// process a response.
	canFailFromLoadErrs bool
	// If we are going to fail the batch in bumpRepeatedLoadErr, we need to
	// set this bool to true. There could be a concurrent request about to
	// be written. See more comments below where this is used.
	isFailingFromLoadErr bool

	wireLength   int32 // tracks total size this batch would currently encode as, including length prefix
	v1wireLength int32 // same as wireLength, but for message set v1

	attrs             int16 // updated during apending; read and converted to RecordAttrs on success
	firstTimestamp    int64 // since unix epoch, in millis
	maxTimestampDelta int64

	mu      sync.Mutex    // guards appendTo's reading of records against failAllRecords emptying it
	records []promisedRec // record w/ length, ts calculated
}

// Returns an error if the batch should fail.
func (b *recBatch) maybeFailErr(cfg *cfg) error {
	if len(b.records) > 0 {
		r0 := &b.records[0]
		select {
		case <-r0.ctx.Done():
			return r0.ctx.Err()
		case <-r0.Context.Done():
			return r0.Context.Err()
		default:
		}
	}
	switch {
	case b.isTimedOut(cfg.recordTimeout):
		return ErrRecordTimeout
	case b.tries >= cfg.recordRetries:
		return ErrRecordRetries
	case b.owner.cl.producer.isAborting():
		return ErrAborting
	}
	return nil
}

func (b *recBatch) v0wireLength() int32 { return b.v1wireLength - 8 } // no timestamp
func (b *recBatch) batchLength() int32  { return b.wireLength - 4 }   // no length prefix
func (b *recBatch) flexibleWireLength() int32 { // uvarint length prefix
	batchLength := b.batchLength()
	return int32(kbin.UvarintLen(uvar32(batchLength))) + batchLength
}

// appendRecord saves a new record to a batch.
//
// This is called under the owning recBuf's mu, meaning records cannot be
// concurrently modified by failing. This batch cannot actively be used
// in a request, so we do not need to worry about a concurrent read.
func (b *recBatch) appendRecord(pr promisedRec, nums recordNumbers) {
	b.wireLength += nums.wireLength()
	b.v1wireLength += messageSet1Length(pr.Record)
	if len(b.records) == 0 {
		b.firstTimestamp = pr.Timestamp.UnixNano() / 1e6
	} else if nums.tsDelta > b.maxTimestampDelta {
		b.maxTimestampDelta = nums.tsDelta
	}
	b.records = append(b.records, pr)
}

// newRecordBatch returns a new record batch for a topic and partition.
func (recBuf *recBuf) newRecordBatch() *recBatch {
	const recordBatchOverhead = 4 + // array len
		8 + // firstOffset
		4 + // batchLength
		4 + // partitionLeaderEpoch
		1 + // magic
		4 + // crc
		2 + // attributes
		4 + // lastOffsetDelta
		8 + // firstTimestamp
		8 + // maxTimestamp
		8 + // producerID
		2 + // producerEpoch
		4 + // seq
		4 // record array length
	return &recBatch{
		owner:      recBuf,
		records:    recBuf.cl.prsPool.get()[:0],
		wireLength: recordBatchOverhead,

		canFailFromLoadErrs: true, // until we send this batch, we can fail it
	}
}

type prsPool struct{ p *sync.Pool }

func newPrsPool() prsPool {
	return prsPool{
		p: &sync.Pool{New: func() any { r := make([]promisedRec, 10); return &r }},
	}
}

func (p prsPool) get() []promisedRec  { return (*p.p.Get().(*[]promisedRec))[:0] }
func (p prsPool) put(s []promisedRec) { p.p.Put(&s) }

// isOwnersFirstBatch returns if the batch in a recBatch is the first batch in
// a records. We only ever want to update batch / buffer logic if the batch is
// the first in the buffer.
func (b *recBatch) isOwnersFirstBatch() bool {
	return len(b.owner.batches) > 0 && b.owner.batches[0] == b
}

// Returns whether the first record in a batch is past the limit.
func (b *recBatch) isTimedOut(limit time.Duration) bool {
	if limit == 0 {
		return false
	}
	return time.Since(b.records[0].Timestamp) > limit
}

// Decrements the inflight count for this batch.
//
// If the inflight count hits zero, this potentially re-triggers a drain on the
// *current* sink. A concurrent metadata update could have moved the recBuf to
// a different sink; that sink will not drain this recBuf until all requests on
// the old sink are finished.
//
// This is always called in the produce request path, not anywhere else (i.e.
// not failAllRecords). We want inflight decrementing to be the last thing that
// happens always for every request. It does not matter if the records were
// independently failed: from the request issuing perspective, the batch is
// still inflight.
func (b *recBatch) decInflight() {
	recBuf := b.owner
	recBuf.inflight--
	if recBuf.inflight != 0 {
		return
	}
	recBuf.inflightOnSink = nil
	if recBuf.batchDrainIdx != len(recBuf.batches) {
		recBuf.sink.maybeDrain()
	}
}

////////////////////
// produceRequest //
////////////////////

// produceRequest is a kmsg.Request that is used when we want to
// flush our buffered records.
//
// It is the same as kmsg.ProduceRequest, but with a custom AppendTo.
type produceRequest struct {
	version int16

	backoffSeq uint32

	txnID   *string
	acks    int16
	timeout int32
	batches seqRecBatches

	producerID    int64
	producerEpoch int16

	// Initialized in AppendTo, metrics tracks uncompressed & compressed
	// sizes (in byteS) of each batch.
	//
	// We use this in handleReqResp for the OnProduceHook.
	metrics produceMetrics
	hasHook bool

	compressor *compressor

	// wireLength is initially the size of sending a produce request,
	// including the request header, with no topics. We start with the
	// non-flexible size because it is strictly larger than flexible, but
	// we use the proper flexible numbers when calculating.
	wireLength      int32
	wireLengthLimit int32
}

type produceMetrics map[string]map[int32]ProduceBatchMetrics

func (p produceMetrics) hook(cfg *cfg, br *broker) {
	if len(p) == 0 {
		return
	}
	var hooks []HookProduceBatchWritten
	cfg.hooks.each(func(h Hook) {
		if h, ok := h.(HookProduceBatchWritten); ok {
			hooks = append(hooks, h)
		}
	})
	if len(hooks) == 0 {
		return
	}
	go func() {
		for _, h := range hooks {
			for topic, partitions := range p {
				for partition, metrics := range partitions {
					h.OnProduceBatchWritten(br.meta, topic, partition, metrics)
				}
			}
		}
	}()
}

func (p *produceRequest) idempotent() bool { return p.producerID >= 0 }

func (p *produceRequest) tryAddBatch(produceVersion int32, recBuf *recBuf, batch *recBatch) bool {
	batchWireLength, flexible := batch.wireLengthForProduceVersion(produceVersion)
	batchWireLength += 4 // int32 partition prefix

	if partitions, exists := p.batches[recBuf.topic]; !exists {
		lt := int32(len(recBuf.topic))
		if flexible {
			batchWireLength += uvarlen(len(recBuf.topic)) + lt + 1 // compact string len, topic, compact array len for 1 item
		} else {
			batchWireLength += 2 + lt + 4 // string len, topic, partition array len
		}
	} else if flexible {
		// If the topic exists and we are flexible, adding this
		// partition may increase the length of our size prefix.
		lastPartitionsLen := uvarlen(len(partitions))
		newPartitionsLen := uvarlen(len(partitions) + 1)
		batchWireLength += (newPartitionsLen - lastPartitionsLen)
	}
	// If we are flexible but do not know it yet, adding partitions may
	// increase our length prefix. Since we are pessimistically assuming
	// non-flexible, we have 200mil partitions to add before we have to
	// worry about hitting 5 bytes vs. the non-flexible 4. We do not worry.

	if p.wireLength+batchWireLength > p.wireLengthLimit {
		return false
	}

	if recBuf.batches[0] == batch {
		if !p.idempotent() || batch.canFailFromLoadErrs {
			if err := batch.maybeFailErr(&batch.owner.cl.cfg); err != nil {
				recBuf.failAllRecords(err)
				return false
			}
		}
		if recBuf.needSeqReset {
			recBuf.needSeqReset = false
			recBuf.seq = 0
			recBuf.batch0Seq = 0
		}
	}

	batch.tries++
	p.wireLength += batchWireLength
	p.batches.addBatch(
		recBuf.topic,
		recBuf.partition,
		recBuf.seq,
		batch,
	)
	return true
}

// seqRecBatch: a recBatch with a sequence number.
type seqRecBatch struct {
	seq int32
	*recBatch
}

type seqRecBatches map[string]map[int32]seqRecBatch

func (rbs *seqRecBatches) addBatch(topic string, part, seq int32, batch *recBatch) {
	if *rbs == nil {
		*rbs = make(seqRecBatches, 5)
	}
	topicBatches, exists := (*rbs)[topic]
	if !exists {
		topicBatches = make(map[int32]seqRecBatch, 1)
		(*rbs)[topic] = topicBatches
	}
	topicBatches[part] = seqRecBatch{seq, batch}
}

func (rbs *seqRecBatches) addSeqBatch(topic string, part int32, batch seqRecBatch) {
	if *rbs == nil {
		*rbs = make(seqRecBatches, 5)
	}
	topicBatches, exists := (*rbs)[topic]
	if !exists {
		topicBatches = make(map[int32]seqRecBatch, 1)
		(*rbs)[topic] = topicBatches
	}
	topicBatches[part] = batch
}

func (rbs seqRecBatches) each(fn func(seqRecBatch)) {
	for _, partitions := range rbs {
		for _, batch := range partitions {
			fn(batch)
		}
	}
}

func (rbs seqRecBatches) eachOwnerLocked(fn func(seqRecBatch)) {
	rbs.each(func(batch seqRecBatch) {
		batch.owner.mu.Lock()
		defer batch.owner.mu.Unlock()
		fn(batch)
	})
}

func (rbs seqRecBatches) sliced() recBatches {
	var batches []*recBatch
	for _, partitions := range rbs {
		for _, batch := range partitions {
			batches = append(batches, batch.recBatch)
		}
	}
	return batches
}

type recBatches []*recBatch

func (bs recBatches) eachOwnerLocked(fn func(*recBatch)) {
	for _, b := range bs {
		b.owner.mu.Lock()
		fn(b)
		b.owner.mu.Unlock()
	}
}

//////////////
// COUNTING // - this section is all about counting how bytes lay out on the wire
//////////////

// Returns the non-flexible base produce request length (the request header and
// the request itself with no topics).
//
// See the large comment on maxRecordBatchBytesForTopic for why we always use
// non-flexible (in short: it is strictly larger).
func (cl *Client) baseProduceRequestLength() int32 {
	const messageRequestOverhead int32 = 4 + // int32 length prefix
		2 + // int16 key
		2 + // int16 version
		4 + // int32 correlation ID
		2 // int16 client ID len (always non flexible)
		// empty tag section skipped; see below

	const produceRequestBaseOverhead int32 = 2 + // int16 transactional ID len (flexible or not, since we cap at 16382)
		2 + // int16 acks
		4 + // int32 timeout
		4 // int32 topics non-flexible array length
		// empty tag section skipped; see below

	baseLength := messageRequestOverhead + produceRequestBaseOverhead
	if cl.cfg.id != nil {
		baseLength += int32(len(*cl.cfg.id))
	}
	if cl.cfg.txnID != nil {
		baseLength += int32(len(*cl.cfg.txnID))
	}
	return baseLength
}

// Returns the maximum size a record batch can be for this given topic, such
// that if just a **single partition** is fully stuffed with records and we
// only encode that one partition, we will not overflow our configured limits.
//
// The maximum topic length is 249, which has a 2 byte prefix for flexible or
// non-flexible.
//
// Non-flexible versions will have a 4 byte length topic array prefix, a 4 byte
// length partition array prefix. and a 4 byte records array length prefix.
//
// Flexible versions would have a 1 byte length topic array prefix, a 1 byte
// length partition array prefix, up to 5 bytes for the records array length
// prefix, and three empty tag sections resulting in 3 bytes (produce request
// struct, topic struct, partition struct). As well, for the request header
// itself, we have an additional 1 byte tag section (that we currently keep
// empty).
//
// Thus in the worst case, we have 14 bytes of prefixes for non-flexible vs.
// 11 bytes for flexible. We default to the more limiting size: non-flexible.
func (cl *Client) maxRecordBatchBytesForTopic(topic string) int32 {
	minOnePartitionBatchLength := cl.baseProduceRequestLength() +
		2 + // int16 topic string length prefix length
		int32(len(topic)) +
		4 + // int32 partitions array length
		4 + // partition int32 encoding length
		4 // int32 record bytes array length

	wireLengthLimit := cl.cfg.maxBrokerWriteBytes

	recordBatchLimit := wireLengthLimit - minOnePartitionBatchLength
	if cfgLimit := cl.cfg.maxRecordBatchBytes; cfgLimit < recordBatchLimit {
		recordBatchLimit = cfgLimit
	}
	return recordBatchLimit
}

func messageSet0Length(r *Record) int32 {
	const length = 4 + // array len
		8 + // offset
		4 + // size
		4 + // crc
		1 + // magic
		1 + // attributes
		4 + // key array bytes len
		4 // value array bytes len
	return length + int32(len(r.Key)) + int32(len(r.Value))
}

func messageSet1Length(r *Record) int32 {
	return messageSet0Length(r) + 8 // timestamp
}

// Returns the numbers for a record if it were added to the record batch.
func (b *recBatch) calculateRecordNumbers(r *Record) recordNumbers {
	tsMillis := r.Timestamp.UnixNano() / 1e6
	tsDelta := tsMillis - b.firstTimestamp

	// If this is to be the first record in the batch, then our timestamp
	// delta is actually 0.
	if len(b.records) == 0 {
		tsDelta = 0
	}

	offsetDelta := int32(len(b.records)) // since called before adding record, delta is the current end

	l := 1 + // attributes, int8 unused
		kbin.VarlongLen(tsDelta) +
		kbin.VarintLen(offsetDelta) +
		kbin.VarintLen(int32(len(r.Key))) +
		len(r.Key) +
		kbin.VarintLen(int32(len(r.Value))) +
		len(r.Value) +
		kbin.VarintLen(int32(len(r.Headers))) // varint array len headers

	for _, h := range r.Headers {
		l += kbin.VarintLen(int32(len(h.Key))) +
			len(h.Key) +
			kbin.VarintLen(int32(len(h.Value))) +
			len(h.Value)
	}

	return recordNumbers{
		lengthField: int32(l),
		tsDelta:     tsDelta,
	}
}

func uvar32(l int32) uint32 { return 1 + uint32(l) }
func uvarlen(l int) int32   { return int32(kbin.UvarintLen(uvar32(int32(l)))) }

// recordNumbers tracks a few numbers for a record that is buffered.
type recordNumbers struct {
	lengthField int32 // the length field prefix of a record encoded on the wire
	tsDelta     int64 // the ms delta of when the record was added against the first timestamp
}

// wireLength is the wire length of a record including its length field prefix.
func (n recordNumbers) wireLength() int32 {
	return int32(kbin.VarintLen(n.lengthField)) + n.lengthField
}

func (b *recBatch) wireLengthForProduceVersion(v int32) (batchWireLength int32, flexible bool) {
	batchWireLength = b.wireLength

	// If we do not yet know the produce version, we default to the largest
	// size. Our request building sizes will always be an overestimate.
	if v < 0 {
		v1BatchWireLength := b.v1wireLength
		if v1BatchWireLength > batchWireLength {
			batchWireLength = v1BatchWireLength
		}
		flexibleBatchWireLength := b.flexibleWireLength()
		if flexibleBatchWireLength > batchWireLength {
			batchWireLength = flexibleBatchWireLength
		}
	} else {
		switch v {
		case 0, 1:
			batchWireLength = b.v0wireLength()
		case 2:
			batchWireLength = b.v1wireLength
		case 3, 4, 5, 6, 7, 8:
			batchWireLength = b.wireLength
		default:
			batchWireLength = b.flexibleWireLength()
			flexible = true
		}
	}

	return
}

func (b *recBatch) tryBuffer(pr promisedRec, produceVersion, maxBatchBytes int32, abortOnNewBatch bool) (appended, aborted bool) {
	nums := b.calculateRecordNumbers(pr.Record)

	batchWireLength, _ := b.wireLengthForProduceVersion(produceVersion)
	newBatchLength := batchWireLength + nums.wireLength()

	if b.tries != 0 || newBatchLength > maxBatchBytes {
		return false, false
	}
	if abortOnNewBatch {
		return false, true
	}
	b.appendRecord(pr, nums)
	pr.setLengthAndTimestampDelta(
		nums.lengthField,
		nums.tsDelta,
	)
	return true, false
}

//////////////
// ENCODING // - this section is all about actually writing a produce request
//////////////

func (*produceRequest) Key() int16           { return 0 }
func (*produceRequest) MaxVersion() int16    { return 10 }
func (p *produceRequest) SetVersion(v int16) { p.version = v }
func (p *produceRequest) GetVersion() int16  { return p.version }
func (p *produceRequest) IsFlexible() bool   { return p.version >= 9 }
func (p *produceRequest) AppendTo(dst []byte) []byte {
	flexible := p.IsFlexible()

	if p.hasHook {
		p.metrics = make(map[string]map[int32]ProduceBatchMetrics)
	}

	if p.version >= 3 {
		if flexible {
			dst = kbin.AppendCompactNullableString(dst, p.txnID)
		} else {
			dst = kbin.AppendNullableString(dst, p.txnID)
		}
	}

	dst = kbin.AppendInt16(dst, p.acks)
	dst = kbin.AppendInt32(dst, p.timeout)
	if flexible {
		dst = kbin.AppendCompactArrayLen(dst, len(p.batches))
	} else {
		dst = kbin.AppendArrayLen(dst, len(p.batches))
	}

	for topic, partitions := range p.batches {
		if flexible {
			dst = kbin.AppendCompactString(dst, topic)
			dst = kbin.AppendCompactArrayLen(dst, len(partitions))
		} else {
			dst = kbin.AppendString(dst, topic)
			dst = kbin.AppendArrayLen(dst, len(partitions))
		}

		var tmetrics map[int32]ProduceBatchMetrics
		if p.hasHook {
			tmetrics = make(map[int32]ProduceBatchMetrics)
			p.metrics[topic] = tmetrics
		}

		for partition, batch := range partitions {
			dst = kbin.AppendInt32(dst, partition)
			batch.mu.Lock()
			if batch.records == nil || batch.isFailingFromLoadErr { // concurrent failAllRecords OR concurrent bumpRepeatedLoadErr
				if flexible {
					dst = kbin.AppendCompactNullableBytes(dst, nil)
				} else {
					dst = kbin.AppendNullableBytes(dst, nil)
				}
				batch.mu.Unlock()
				continue
			}
			batch.canFailFromLoadErrs = false // we are going to write this batch: the response status is now unknown
			var pmetrics ProduceBatchMetrics
			if p.version < 3 {
				dst, pmetrics = batch.appendToAsMessageSet(dst, uint8(p.version), p.compressor)
			} else {
				dst, pmetrics = batch.appendTo(dst, p.version, p.producerID, p.producerEpoch, p.txnID != nil, p.compressor)
			}
			batch.mu.Unlock()
			if p.hasHook {
				tmetrics[partition] = pmetrics
			}
			if flexible {
				dst = append(dst, 0)
			}
		}
		if flexible {
			dst = append(dst, 0)
		}
	}
	if flexible {
		dst = append(dst, 0)
	}

	return dst
}

func (*produceRequest) ReadFrom([]byte) error {
	panic("unreachable -- the client never uses ReadFrom on its internal produceRequest")
}

func (p *produceRequest) ResponseKind() kmsg.Response {
	r := kmsg.NewPtrProduceResponse()
	r.Version = p.version
	return r
}

func (b seqRecBatch) appendTo(
	in []byte,
	version int16,
	producerID int64,
	producerEpoch int16,
	transactional bool,
	compressor *compressor,
) (dst []byte, m ProduceBatchMetrics) { // named return so that our defer for flexible versions can modify it
	flexible := version >= 9
	dst = in
	nullableBytesLen := b.wireLength - 4 // NULLABLE_BYTES leading length, minus itself
	nullableBytesLenAt := len(dst)       // in case compression adjusting
	dst = kbin.AppendInt32(dst, nullableBytesLen)

	// With flexible versions, the array length prefix can be anywhere from
	// 1 byte long to 5 bytes long (covering up to 268MB).
	//
	// We have to add our initial understanding of the array length as a
	// uvarint, but if compressing shrinks what that length would encode
	// as, we have to shift everything down.
	if flexible {
		dst = dst[:nullableBytesLenAt]
		batchLength := b.batchLength()
		dst = kbin.AppendUvarint(dst, uvar32(batchLength)) // compact array non-null prefix
		batchAt := len(dst)
		defer func() {
			batch := dst[batchAt:]
			if int32(len(batch)) == batchLength { // we did not compress: simply return
				return
			}

			// We *only* could have shrunk the batch bytes, so our
			// append here will not overwrite anything we need to
			// keep.
			newDst := kbin.AppendUvarint(dst[:nullableBytesLenAt], uvar32(int32(len(batch))))

			// If our append did not shorten the length prefix, we
			// can just return the prior dst, otherwise we have to
			// shift the batch itself down on newDst.
			if len(newDst) != batchAt {
				dst = append(newDst, batch...)
			}
		}()
	}

	// Below here, we append the actual record batch, which cannot be
	// flexible. Everything encodes properly; flexible adjusting is done in
	// the defer just above.

	dst = kbin.AppendInt64(dst, 0) // firstOffset, defined as zero for producing

	batchLen := nullableBytesLen - 8 - 4 // length of what follows this field (so, minus what came before and ourself)
	batchLenAt := len(dst)               // in case compression adjusting
	dst = kbin.AppendInt32(dst, batchLen)

	dst = kbin.AppendInt32(dst, -1) // partitionLeaderEpoch, unused in clients
	dst = kbin.AppendInt8(dst, 2)   // magic, defined as 2 for records v0.11.0+

	crcStart := len(dst)           // fill at end
	dst = kbin.AppendInt32(dst, 0) // reserved crc

	attrsAt := len(dst) // in case compression adjusting
	b.attrs = 0
	if transactional {
		b.attrs |= 0x0010 // bit 5 is the "is transactional" bit
	}
	dst = kbin.AppendInt16(dst, b.attrs)
	dst = kbin.AppendInt32(dst, int32(len(b.records)-1)) // lastOffsetDelta
	dst = kbin.AppendInt64(dst, b.firstTimestamp)
	dst = kbin.AppendInt64(dst, b.firstTimestamp+b.maxTimestampDelta)

	seq := b.seq
	if producerID < 0 { // a negative producer ID means we are not using idempotence
		seq = 0
	}
	dst = kbin.AppendInt64(dst, producerID)
	dst = kbin.AppendInt16(dst, producerEpoch)
	dst = kbin.AppendInt32(dst, seq)

	dst = kbin.AppendArrayLen(dst, len(b.records))
	recordsAt := len(dst)
	for i, pr := range b.records {
		dst = pr.appendTo(dst, int32(i))
	}

	toCompress := dst[recordsAt:]
	m.NumRecords = len(b.records)
	m.UncompressedBytes = len(toCompress)
	m.CompressedBytes = m.UncompressedBytes

	if compressor != nil {
		w := byteBuffers.Get().(*bytes.Buffer)
		defer byteBuffers.Put(w)
		w.Reset()

		compressed, codec := compressor.compress(w, toCompress, version)
		if compressed != nil && // nil would be from an error
			len(compressed) < len(toCompress) {
			// our compressed was shorter: copy over
			copy(dst[recordsAt:], compressed)
			dst = dst[:recordsAt+len(compressed)]
			m.CompressedBytes = len(compressed)
			m.CompressionType = uint8(codec)

			// update the few record batch fields we already wrote
			savings := int32(len(toCompress) - len(compressed))
			nullableBytesLen -= savings
			batchLen -= savings
			b.attrs |= int16(codec)
			if !flexible {
				kbin.AppendInt32(dst[:nullableBytesLenAt], nullableBytesLen)
			}
			kbin.AppendInt32(dst[:batchLenAt], batchLen)
			kbin.AppendInt16(dst[:attrsAt], b.attrs)
		}
	}

	kbin.AppendInt32(dst[:crcStart], int32(crc32.Checksum(dst[crcStart+4:], crc32c)))

	return dst, m
}

func (pr promisedRec) appendTo(dst []byte, offsetDelta int32) []byte {
	length, tsDelta := pr.lengthAndTimestampDelta()
	dst = kbin.AppendVarint(dst, length)
	dst = kbin.AppendInt8(dst, 0) // attributes, currently unused
	dst = kbin.AppendVarlong(dst, tsDelta)
	dst = kbin.AppendVarint(dst, offsetDelta)
	dst = kbin.AppendVarintBytes(dst, pr.Key)
	dst = kbin.AppendVarintBytes(dst, pr.Value)
	dst = kbin.AppendVarint(dst, int32(len(pr.Headers)))
	for _, h := range pr.Headers {
		dst = kbin.AppendVarintString(dst, h.Key)
		dst = kbin.AppendVarintBytes(dst, h.Value)
	}
	return dst
}

func (b seqRecBatch) appendToAsMessageSet(dst []byte, version uint8, compressor *compressor) ([]byte, ProduceBatchMetrics) {
	var m ProduceBatchMetrics

	nullableBytesLenAt := len(dst)
	dst = append(dst, 0, 0, 0, 0) // nullable bytes len
	for i, pr := range b.records {
		_, tsDelta := pr.lengthAndTimestampDelta()
		dst = appendMessageTo(
			dst,
			version,
			0,
			int64(i),
			b.firstTimestamp+tsDelta,
			pr.Record,
		)
	}

	b.attrs = 0

	// Produce request v0 and v1 uses message set v0, which does not have
	// timestamps. We set bit 8 in our attrs which corresponds with our own
	// kgo.RecordAttrs's bit. The attrs field is unused in a sink / recBuf
	// outside of the appending functions or finishing records; if we use
	// more bits in our internal RecordAttrs, the below will need to
	// change.
	if version == 0 || version == 1 {
		b.attrs |= 0b1000_0000
	}

	toCompress := dst[nullableBytesLenAt+4:] // skip nullable bytes leading prefix
	m.NumRecords = len(b.records)
	m.UncompressedBytes = len(toCompress)
	m.CompressedBytes = m.UncompressedBytes

	if compressor != nil {
		w := byteBuffers.Get().(*bytes.Buffer)
		defer byteBuffers.Put(w)
		w.Reset()

		compressed, codec := compressor.compress(w, toCompress, int16(version))
		inner := &Record{Value: compressed}
		wrappedLength := messageSet0Length(inner)
		if version == 2 {
			wrappedLength += 8 // timestamp
		}

		if compressed != nil && int(wrappedLength) < len(toCompress) {
			m.CompressedBytes = int(wrappedLength)
			m.CompressionType = uint8(codec)

			b.attrs |= int16(codec)

			dst = appendMessageTo(
				dst[:nullableBytesLenAt+4],
				version,
				int8(codec),
				int64(len(b.records)-1),
				b.firstTimestamp,
				inner,
			)
		}
	}

	kbin.AppendInt32(dst[:nullableBytesLenAt], int32(len(dst[nullableBytesLenAt+4:])))
	return dst, m
}

func appendMessageTo(
	dst []byte,
	version uint8,
	attributes int8,
	offset int64,
	timestamp int64,
	r *Record,
) []byte {
	magic := version >> 1
	dst = kbin.AppendInt64(dst, offset)
	msgSizeStart := len(dst)
	dst = append(dst, 0, 0, 0, 0)
	crc32Start := len(dst)
	dst = append(dst,
		0, 0, 0, 0,
		magic,
		byte(attributes))
	if magic == 1 {
		dst = kbin.AppendInt64(dst, timestamp)
	}
	dst = kbin.AppendNullableBytes(dst, r.Key)
	dst = kbin.AppendNullableBytes(dst, r.Value)
	kbin.AppendInt32(dst[:crc32Start], int32(crc32.ChecksumIEEE(dst[crc32Start+4:])))
	kbin.AppendInt32(dst[:msgSizeStart], int32(len(dst[msgSizeStart+4:])))
	return dst
}
