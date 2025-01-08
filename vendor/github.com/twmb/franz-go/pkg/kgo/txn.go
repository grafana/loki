package kgo

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/twmb/franz-go/pkg/kmsg"

	"github.com/twmb/franz-go/pkg/kerr"
)

func ctx2fn(ctx context.Context) func() context.Context { return func() context.Context { return ctx } }

// TransactionEndTry is simply a named bool.
type TransactionEndTry bool

const (
	// TryAbort attempts to end a transaction with an abort.
	TryAbort TransactionEndTry = false

	// TryCommit attempts to end a transaction with a commit.
	TryCommit TransactionEndTry = true
)

// GroupTransactSession abstracts away the proper way to begin and end a
// transaction when consuming in a group, modifying records, and producing
// (EOS).
//
// If you are running Kafka 2.5+, it is strongly recommended that you also use
// RequireStableFetchOffsets. See that config option's documentation for more
// details.
type GroupTransactSession struct {
	cl *Client

	failMu sync.Mutex

	revoked   bool
	revokedCh chan struct{} // closed once when revoked is set; reset after End
	lost      bool
	lostCh    chan struct{} // closed once when lost is set; reset after End
}

// NewGroupTransactSession is exactly the same as NewClient, but wraps the
// client's OnPartitionsRevoked / OnPartitionsLost to ensure that transactions
// are correctly aborted whenever necessary so as to properly provide EOS.
//
// When ETLing in a group in a transaction, if a rebalance happens before the
// transaction is ended, you either (a) must block the rebalance from finishing
// until you are done producing, and then commit before unblocking, or (b)
// allow the rebalance to happen, but abort any work you did.
//
// The problem with (a) is that if your ETL work loop is slow, you run the risk
// of exceeding the rebalance timeout and being kicked from the group. You will
// try to commit, and depending on the Kafka version, the commit may even be
// erroneously successful (pre Kafka 2.5). This will lead to duplicates.
//
// Instead, for safety, a GroupTransactSession favors (b). If a rebalance
// occurs at any time before ending a transaction with a commit, this will
// abort the transaction.
//
// This leaves the risk that ending the transaction itself exceeds the
// rebalance timeout, but this is just one request with no cpu logic. With a
// proper rebalance timeout, this single request will not fail and the commit
// will succeed properly.
//
// If this client detects you are talking to a pre-2.5 cluster, OR if you have
// not enabled RequireStableFetchOffsets, the client will sleep for 200ms after
// a successful commit to allow Kafka's txn markers to propagate. This is not
// foolproof in the event of some extremely unlikely communication patterns and
// **potentially** could allow duplicates. See this repo's transaction's doc
// for more details.
func NewGroupTransactSession(opts ...Opt) (*GroupTransactSession, error) {
	s := &GroupTransactSession{
		revokedCh: make(chan struct{}),
		lostCh:    make(chan struct{}),
	}

	var noGroup error

	// We append one option, which will get applied last. Because it is
	// applied last, we can execute some logic and override some existing
	// options.
	opts = append(opts, groupOpt{func(cfg *cfg) {
		if cfg.group == "" {
			cfg.seedBrokers = nil // force a validation error
			noGroup = errors.New("missing required group")
			return
		}

		userRevoked := cfg.onRevoked
		cfg.onRevoked = func(ctx context.Context, cl *Client, rev map[string][]int32) {
			s.failMu.Lock()
			defer s.failMu.Unlock()
			if s.revoked {
				return
			}

			if cl.consumer.g.cooperative.Load() && len(rev) == 0 && !s.revoked {
				cl.cfg.logger.Log(LogLevelInfo, "transact session in on_revoke with nothing to revoke; allowing next commit")
			} else {
				cl.cfg.logger.Log(LogLevelInfo, "transact session in on_revoke; aborting next commit if we are currently in a transaction")
				s.revoked = true
				close(s.revokedCh)
			}

			if userRevoked != nil {
				userRevoked(ctx, cl, rev)
			}
		}

		userLost := cfg.onLost
		cfg.onLost = func(ctx context.Context, cl *Client, lost map[string][]int32) {
			s.failMu.Lock()
			defer s.failMu.Unlock()
			if s.lost {
				return
			}

			cl.cfg.logger.Log(LogLevelInfo, "transact session in on_lost; aborting next commit if we are currently in a transaction")
			s.lost = true
			close(s.lostCh)

			if userLost != nil {
				userLost(ctx, cl, lost)
			} else if userRevoked != nil {
				userRevoked(ctx, cl, lost)
			}
		}
	}})

	cl, err := NewClient(opts...)
	if err != nil {
		if noGroup != nil {
			err = noGroup
		}
		return nil, err
	}
	s.cl = cl
	return s, nil
}

// Client returns the underlying client that this transact session wraps. This
// can be useful for functions that require a client, such as raw requests. The
// returned client should not be used to manage transactions (leave that to the
// GroupTransactSession).
func (s *GroupTransactSession) Client() *Client {
	return s.cl
}

// Close is a wrapper around Client.Close, with the exact same semantics.
// Refer to that function's documentation.
//
// This function must be called to leave the group before shutting down.
func (s *GroupTransactSession) Close() {
	s.cl.Close()
}

// AllowRebalance is a wrapper around Client.AllowRebalance, with the exact
// same semantics. Refer to that function's documentation.
func (s *GroupTransactSession) AllowRebalance() {
	s.cl.AllowRebalance()
}

// CloseAllowingRebalance is a wrapper around Client.CloseAllowingRebalance,
// with the exact same semantics. Refer to that function's documentation.
func (s *GroupTransactSession) CloseAllowingRebalance() {
	s.cl.CloseAllowingRebalance()
}

// PollFetches is a wrapper around Client.PollFetches, with the exact same
// semantics. Refer to that function's documentation.
//
// It is invalid to call PollFetches concurrently with Begin or End.
func (s *GroupTransactSession) PollFetches(ctx context.Context) Fetches {
	return s.cl.PollFetches(ctx)
}

// PollRecords is a wrapper around Client.PollRecords, with the exact same
// semantics. Refer to that function's documentation.
//
// It is invalid to call PollRecords concurrently with Begin or End.
func (s *GroupTransactSession) PollRecords(ctx context.Context, maxPollRecords int) Fetches {
	return s.cl.PollRecords(ctx, maxPollRecords)
}

// ProduceSync is a wrapper around Client.ProduceSync, with the exact same
// semantics. Refer to that function's documentation.
//
// It is invalid to call ProduceSync concurrently with Begin or End.
func (s *GroupTransactSession) ProduceSync(ctx context.Context, rs ...*Record) ProduceResults {
	return s.cl.ProduceSync(ctx, rs...)
}

// Produce is a wrapper around Client.Produce, with the exact same semantics.
// Refer to that function's documentation.
//
// It is invalid to call Produce concurrently with Begin or End.
func (s *GroupTransactSession) Produce(ctx context.Context, r *Record, promise func(*Record, error)) {
	s.cl.Produce(ctx, r, promise)
}

// TryProduce is a wrapper around Client.TryProduce, with the exact same
// semantics. Refer to that function's documentation.
//
// It is invalid to call TryProduce concurrently with Begin or End.
func (s *GroupTransactSession) TryProduce(ctx context.Context, r *Record, promise func(*Record, error)) {
	s.cl.TryProduce(ctx, r, promise)
}

// Begin begins a transaction, returning an error if the client has no
// transactional id or is already in a transaction. Begin must be called
// before producing records in a transaction.
func (s *GroupTransactSession) Begin() error {
	s.cl.cfg.logger.Log(LogLevelInfo, "beginning transact session")
	return s.cl.BeginTransaction()
}

func (s *GroupTransactSession) failed() bool {
	return s.revoked || s.lost
}

// End ends a transaction, committing if commit is true, if the group did not
// rebalance since the transaction began, and if committing offsets is
// successful. If any of these conditions are false, this aborts. This flushes
// or aborts depending on `commit`.
//
// This returns whether the transaction committed or any error that occurred.
// No returned error is retryable. Either the transactional ID has entered a
// failed state, or the client retried so much that the retry limit was hit,
// and odds are you should not continue. While a context is allowed, canceling
// it will likely leave the client in an invalid state. Canceling should only
// be done if you want to shut down.
func (s *GroupTransactSession) End(ctx context.Context, commit TransactionEndTry) (committed bool, err error) {
	defer func() {
		s.failMu.Lock()
		s.revoked = false
		s.revokedCh = make(chan struct{})
		s.lost = false
		s.lostCh = make(chan struct{})
		s.failMu.Unlock()
	}()

	switch commit {
	case TryCommit:
		if err := s.cl.Flush(ctx); err != nil {
			return false, err // we do not abort below, because an error here is ctx closing
		}
	case TryAbort:
		if err := s.cl.AbortBufferedRecords(ctx); err != nil {
			return false, err // same
		}
	}

	wantCommit := bool(commit)

	s.failMu.Lock()
	failed := s.failed()

	precommit := s.cl.CommittedOffsets()
	postcommit := s.cl.UncommittedOffsets()
	s.failMu.Unlock()

	var hasAbortableCommitErr bool
	var commitErr error
	var g *groupConsumer

	kip447 := false
	if wantCommit && !failed {
		isAbortableCommitErr := func(err error) bool {
			// ILLEGAL_GENERATION: rebalance began and completed
			// before we committed.
			//
			// REBALANCE_IN_PREGRESS: rebalance began, abort.
			//
			// COORDINATOR_NOT_AVAILABLE,
			// COORDINATOR_LOAD_IN_PROGRESS,
			// NOT_COORDINATOR: request failed too many times
			//
			// CONCURRENT_TRANSACTIONS: Kafka not harmonized,
			// we can just abort.
			//
			// UNKNOWN_SERVER_ERROR: technically should not happen,
			// but we can just abort. Redpanda returns this in
			// certain versions.
			switch {
			case errors.Is(err, kerr.IllegalGeneration),
				errors.Is(err, kerr.RebalanceInProgress),
				errors.Is(err, kerr.CoordinatorNotAvailable),
				errors.Is(err, kerr.CoordinatorLoadInProgress),
				errors.Is(err, kerr.NotCoordinator),
				errors.Is(err, kerr.ConcurrentTransactions),
				errors.Is(err, kerr.UnknownServerError),
				errors.Is(err, kerr.TransactionAbortable):
				return true
			}
			return false
		}

		var commitErrs []string

		committed := make(chan struct{})
		g = s.cl.commitTransactionOffsets(ctx, postcommit,
			func(_ *kmsg.TxnOffsetCommitRequest, resp *kmsg.TxnOffsetCommitResponse, err error) {
				defer close(committed)
				if err != nil {
					if isAbortableCommitErr(err) {
						hasAbortableCommitErr = true
						return
					}
					commitErrs = append(commitErrs, err.Error())
					return
				}
				kip447 = resp.Version >= 3

				for _, t := range resp.Topics {
					for _, p := range t.Partitions {
						if err := kerr.ErrorForCode(p.ErrorCode); err != nil {
							if isAbortableCommitErr(err) {
								hasAbortableCommitErr = true
							} else {
								commitErrs = append(commitErrs, fmt.Sprintf("topic %s partition %d: %v", t.Topic, p.Partition, err))
							}
						}
					}
				}
			},
		)
		<-committed

		if len(commitErrs) > 0 {
			commitErr = fmt.Errorf("unable to commit transaction offsets: %s", strings.Join(commitErrs, ", "))
		}
	}

	// Now that we have committed our offsets, before we allow them to be
	// used, we force a heartbeat. By forcing a heartbeat, if there is no
	// error, then we know we have up to RebalanceTimeout to write our
	// EndTxnRequest without a problem.
	//
	// We should not be booted from the group if we receive an ok
	// heartbeat, meaning that, as mentioned, we should be able to end the
	// transaction safely.
	var okHeartbeat bool
	if g != nil && commitErr == nil {
		waitHeartbeat := make(chan struct{})
		var heartbeatErr error
		select {
		case g.heartbeatForceCh <- func(err error) {
			defer close(waitHeartbeat)
			heartbeatErr = err
		}:
			select {
			case <-waitHeartbeat:
				okHeartbeat = heartbeatErr == nil
			case <-s.revokedCh:
			case <-s.lostCh:
			}
		case <-s.revokedCh:
		case <-s.lostCh:
		}
	}

	s.failMu.Lock()

	// If we know we are KIP-447 and the user is requiring stable, we can
	// unlock immediately because Kafka will itself block a rebalance
	// fetching offsets from outstanding transactions.
	//
	// If either of these are false, we spin up a goroutine that sleeps for
	// 200ms before unlocking to give Kafka a chance to avoid some odd race
	// that would permit duplicates (i.e., what KIP-447 is preventing).
	//
	// This 200ms is not perfect but it should be well enough time on a
	// stable cluster. On an unstable cluster, I still expect clients to be
	// slower than intra-cluster communication, but there is a risk.
	if kip447 && s.cl.cfg.requireStable {
		defer s.failMu.Unlock()
	} else {
		defer func() {
			if committed {
				s.cl.cfg.logger.Log(LogLevelDebug, "sleeping 200ms before allowing a rebalance to continue to give the brokers a chance to write txn markers and avoid duplicates")
				go func() {
					time.Sleep(200 * time.Millisecond)
					s.failMu.Unlock()
				}()
			} else {
				s.failMu.Unlock()
			}
		}()
	}

	tryCommit := !s.failed() && commitErr == nil && !hasAbortableCommitErr && okHeartbeat
	willTryCommit := wantCommit && tryCommit

	s.cl.cfg.logger.Log(LogLevelInfo, "transaction session ending",
		"was_failed", s.failed(),
		"want_commit", wantCommit,
		"can_try_commit", tryCommit,
		"will_try_commit", willTryCommit,
	)

	// We have a few potential retryable errors from EndTransaction.
	// OperationNotAttempted will be returned at most once.
	//
	// UnknownServerError should not be returned, but some brokers do:
	// technically this is fatal, but there is no downside to retrying
	// (even retrying a commit) and seeing if we are successful or if we
	// get a better error.
	var tries int
retry:
	endTxnErr := s.cl.EndTransaction(ctx, TransactionEndTry(willTryCommit))
	tries++
	if endTxnErr != nil && tries < 10 {
		switch {
		case errors.Is(endTxnErr, kerr.OperationNotAttempted):
			s.cl.cfg.logger.Log(LogLevelInfo, "end transaction with commit not attempted; retrying as abort")
			willTryCommit = false
			goto retry

		case errors.Is(endTxnErr, kerr.TransactionAbortable):
			s.cl.cfg.logger.Log(LogLevelInfo, "end transaction returned TransactionAbortable; retrying as abort")
			willTryCommit = false
			goto retry

		case errors.Is(endTxnErr, kerr.UnknownServerError):
			s.cl.cfg.logger.Log(LogLevelInfo, "end transaction with commit unknown server error; retrying")
			after := time.NewTimer(s.cl.cfg.retryBackoff(tries))
			select {
			case <-after.C: // context canceled; we will see when we retry
			case <-s.cl.ctx.Done():
				after.Stop()
			}
			goto retry
		}
	}

	if !willTryCommit || endTxnErr != nil {
		currentCommit := s.cl.CommittedOffsets()
		s.cl.cfg.logger.Log(LogLevelInfo, "transact session resetting to current committed state (potentially after a rejoin)",
			"tried_commit", willTryCommit,
			"commit_err", endTxnErr,
			"state_precommit", precommit,
			"state_currently_committed", currentCommit,
		)
		s.cl.setOffsets(currentCommit, false)
	} else if willTryCommit && endTxnErr == nil {
		s.cl.cfg.logger.Log(LogLevelInfo, "transact session successful, setting to newly committed state",
			"tried_commit", willTryCommit,
			"postcommit", postcommit,
		)
		s.cl.setOffsets(postcommit, false)
	}

	switch {
	case commitErr != nil && endTxnErr == nil:
		return false, commitErr

	case commitErr == nil && endTxnErr != nil:
		return false, endTxnErr

	case commitErr != nil && endTxnErr != nil:
		return false, endTxnErr

	default: // both errs nil
		committed = willTryCommit
		return willTryCommit, nil
	}
}

// BeginTransaction sets the client to a transactional state, erroring if there
// is no transactional ID, or if the producer is currently in a fatal
// (unrecoverable) state, or if the client is already in a transaction.
//
// This must not be called concurrently with other client functions.
func (cl *Client) BeginTransaction() error {
	if cl.cfg.txnID == nil {
		return errNotTransactional
	}

	cl.producer.txnMu.Lock()
	defer cl.producer.txnMu.Unlock()

	if cl.producer.inTxn {
		return errors.New("invalid attempt to begin a transaction while already in a transaction")
	}

	needRecover, didRecover, err := cl.maybeRecoverProducerID(context.Background())
	if needRecover && !didRecover {
		cl.cfg.logger.Log(LogLevelInfo, "unable to begin transaction due to unrecoverable producer id error", "err", err)
		return fmt.Errorf("producer ID has a fatal, unrecoverable error, err: %w", err)
	}

	cl.producer.inTxn = true
	cl.producer.producingTxn.Store(true) // allow produces for txns now
	cl.cfg.logger.Log(LogLevelInfo, "beginning transaction", "transactional_id", *cl.cfg.txnID)

	return nil
}

// EndBeginTxnHow controls the safety of how EndAndBeginTransaction executes.
type EndBeginTxnHow uint8

const (
	// EndBeginTxnSafe ensures a "safe" execution of EndAndBeginTransaction
	// at the expense of speed. This option blocks all produce requests and
	// only resumes produce requests when onEnd finishes. Note that some
	// produce requests may have finished successfully and records that
	// were a part of a transaction may have their promises waiting to be
	// called: not all promises are guaranteed to be called.
	EndBeginTxnSafe EndBeginTxnHow = iota

	// EndBeginTxnUnsafe opts for less safe EndAndBeginTransaction flow to
	// achieve higher throughput. This option allows produce requests to
	// continue while EndTxn actually commits. This is unsafe because a
	// produce request itself only half begins a transaction. Internally,
	// AddPartitionsToTxn actually begins a transaction. If your
	// application dies before the client is able to successfully issue
	// AddPartitionsToTxn, then a transaction will have partially begun
	// within Kafka: the partial transaction will prevent the partition
	// from being consumable past where the transaction begun, and the
	// transaction will not timeout. You will have to restart your
	// application with the SAME transactional ID and produce to all the
	// same partitions to ensure to resume the transaction and unstick the
	// partitions.
	//
	// Also note: this option does not work on all broker implementations.
	// This relies on Kafka internals. Some brokers (notably Redpanda) are
	// more strict with enforcing transaction correctness and this option
	// cannot be used and will cause errors.
	//
	// Deprecated: Kafka 3.6 removed support for the hacky behavior that
	// this option was abusing. Thus, as of Kafka 3.6, this option does not
	// work against Kafka. This option also has never worked for Redpanda
	// because Redpanda always strictly validated that partitions were a
	// part of a transaction. Later versions of Kafka and Redpanda will
	// remove the need for AddPartitionsToTxn at all and thus this option
	// ultimately will be unnecessary anyway.
	EndBeginTxnUnsafe
)

// EndAndBeginTransaction is a combination of EndTransaction and
// BeginTransaction, and relaxes the restriction that the client must have no
// buffered records. This function does not flush nor abort any buffered
// records. It is ok to concurrently produce while this function executes.
//
// This function has different safety guarantees which are up to the user to
// decide. See the documentation on EndBeginTxnHow for which you would like to
// choose.
//
// The onEnd function is called with your input context and the result of
// EndTransaction. Promises are paused while onEnd executes. If onEnd returns
// an error, BeginTransaction is not called and this function returns the
// result of onEnd. Otherwise, this function returns the result of
// BeginTransaction. See the documentation on EndTransaction and
// BeginTransaction for further details. It is invalid to call this function
// more than once at a time, and it is invalid to call concurrent with
// EndTransaction or BeginTransaction.
func (cl *Client) EndAndBeginTransaction(
	ctx context.Context,
	how EndBeginTxnHow,
	commit TransactionEndTry,
	onEnd func(context.Context, error) error,
) (rerr error) {
	if g := cl.consumer.g; g != nil {
		return errors.New("cannot use EndAndBeginTransaction with EOS")
	}

	cl.producer.txnMu.Lock()
	defer cl.producer.txnMu.Unlock()

	// From BeginTransaction: if we return with no error, we begin.  Unlike
	// BeginTransaction, we do not error if in a transaction, because we
	// expect to be in one.
	defer func() {
		if rerr == nil {
			needRecover, didRecover, err := cl.maybeRecoverProducerID(ctx)
			if needRecover && !didRecover {
				cl.cfg.logger.Log(LogLevelInfo, "unable to begin transaction due to unrecoverable producer id error", "err", err)
				rerr = fmt.Errorf("producer ID has a fatal, unrecoverable error, err: %w", err)
				return
			}
			cl.producer.inTxn = true
			cl.cfg.logger.Log(LogLevelInfo, "beginning transaction", "transactional_id", *cl.cfg.txnID)
		}
	}()

	// If end/beginning safely, we have to pause AddPartitionsToTxn and
	// ProduceRequest, and we only resume after the user's onEnd has been
	// called.
	if how == EndBeginTxnSafe {
		if err := cl.producer.pause(ctx); err != nil {
			return err
		}
		defer cl.producer.resume()
	}

	// Before BeginTransaction, we block promises & call onEnd with whatever
	// the return error is.
	cl.producer.promisesMu.Lock()
	var promisesUnblocked bool
	unblockPromises := func() {
		if promisesUnblocked {
			return
		}
		promisesUnblocked = true
		defer cl.producer.promisesMu.Unlock()
		rerr = onEnd(ctx, rerr)
	}
	defer unblockPromises()

	if !cl.producer.inTxn {
		return nil
	}

	var anyAdded bool
	var readd map[string][]int32
	for topic, parts := range cl.producer.topics.load() {
		for i, part := range parts.load().partitions {
			if part.records.addedToTxn.Swap(false) {
				if how == EndBeginTxnUnsafe {
					if readd == nil {
						readd = make(map[string][]int32)
					}
					readd[topic] = append(readd[topic], int32(i))
				}
				anyAdded = true
			}
		}
	}
	anyAdded = anyAdded || cl.producer.readded

	// EndTxn when no txn was started returns INVALID_TXN_STATE.
	if !anyAdded {
		cl.cfg.logger.Log(LogLevelDebug, "no records were produced during the commit; thus no transaction was began; ending without doing anything")
		return nil
	}

	// From EndTransaction: if the pid has an error, we may try to recover.
	id, epoch, err := cl.producerID(ctx2fn(ctx))
	if err != nil {
		if commit {
			return kerr.OperationNotAttempted
		}
		if _, didRecover, _ := cl.maybeRecoverProducerID(ctx); didRecover {
			return nil
		}
	}
	cl.cfg.logger.Log(LogLevelInfo, "ending transaction",
		"transactional_id", *cl.cfg.txnID,
		"producer_id", id,
		"epoch", epoch,
		"commit", commit,
	)
	cl.producer.readded = false
	err = cl.doWithConcurrentTransactions(ctx, "EndTxn", func() error {
		req := kmsg.NewPtrEndTxnRequest()
		req.TransactionalID = *cl.cfg.txnID
		req.ProducerID = id
		req.ProducerEpoch = epoch
		req.Commit = bool(commit)
		resp, err := req.RequestWith(ctx, cl)
		if err != nil {
			return err
		}

		// When ending a transaction, if the user is using unsafe mode,
		// there is a logic race where the user can actually end before
		// AddPartitionsToTxn is issued. This should be rare and is
		// most likely only to happen whenever a new transaction is
		// starting from a not-in-transaction state (i.e., the first
		// transaction). If we see InvalidTxnState in unsafe mode, we
		// assume that a transaction was not actually begun and we
		// return success.
		//
		// In Kafka, InvalidTxnState is also returned when producing
		// non-transactional records from a producer that is currently
		// in a transaction.
		//
		// All other cases it is returned is in EndTxn:
		//   * state == CompleteCommit and EndTxn != commit
		//   * state == CompleteAbort and EndTxn != abort
		//   * state == PrepareCommit and EndTxn != commit (otherwise, returns concurrent transactions)
		//   * state == PrepareAbort and EndTxn != abort (otherwise, returns concurrent transactions)
		//   * state == Empty
		//
		// This basically guards against the final case, all others are
		// Kafka internal state transitioning and we should never hit
		// them.
		if how == EndBeginTxnUnsafe && resp.ErrorCode == kerr.InvalidTxnState.Code {
			return nil
		}
		return kerr.ErrorForCode(resp.ErrorCode)
	})
	var ke *kerr.Error
	if errors.As(err, &ke) && !ke.Retriable {
		cl.failProducerID(id, epoch, err)
	}
	if err != nil || how != EndBeginTxnUnsafe {
		return err
	}
	unblockPromises()

	// If we are end/beginning unsafely, then we need to re-add all
	// partitions to a new transaction immediately. Timing makes it
	// impossible to know what was truly added before EndTxn, so we
	// pessimistically assume that every partition must be re-added.
	//
	// We track readd before the txn and swap those to un-added, but we
	// also need to track anything that is newly added that raced with our
	// EndTxn.  We swap before the txn to ensure that *eventually*,
	// partitions will be tracked as not in a transaction if people stop
	// producing.
	//
	// We do this before the user callback because we *need* to start a new
	// transaction within Kafka to ensure there will be a timeout. Per the
	// unsafe aspect, the client could die or this request could error and
	// there could be a stranded txn within Kafka's ProducerStateManager,
	// but ideally the user will reconnect with the same txnal id.
	cl.producer.readded = true
	return cl.doWithConcurrentTransactions(ctx, "AddPartitionsToTxn", func() error {
		req := kmsg.NewPtrAddPartitionsToTxnRequest()
		req.TransactionalID = *cl.cfg.txnID
		req.ProducerID = id
		req.ProducerEpoch = epoch

		for topic, parts := range cl.producer.topics.load() {
			for i, part := range parts.load().partitions {
				if part.records.addedToTxn.Load() {
					readd[topic] = append(readd[topic], int32(i))
				}
			}
		}

		ps := make(map[int32]struct{})
		for topic, parts := range readd {
			t := kmsg.NewAddPartitionsToTxnRequestTopic()
			t.Topic = topic
			for _, part := range parts {
				ps[part] = struct{}{}
			}
			for p := range ps {
				t.Partitions = append(t.Partitions, p)
				delete(ps, p)
			}
			if len(t.Partitions) > 0 {
				req.Topics = append(req.Topics, t)
			}
		}

		resp, err := req.RequestWith(ctx, cl)
		if err != nil {
			return err
		}

		for i := range resp.Topics {
			t := &resp.Topics[i]
			for j := range t.Partitions {
				p := &t.Partitions[j]
				if err := kerr.ErrorForCode(p.ErrorCode); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// AbortBufferedRecords fails all unflushed records with ErrAborted and waits
// for there to be no buffered records.
//
// This accepts a context to quit the wait early, but quitting the wait may
// lead to an invalid state and should only be used if you are quitting your
// application. This function waits to abort records at safe points: if records
// are known to not be in flight. This function is safe to call multiple times
// concurrently, and safe to call concurrent with Flush.
//
// NOTE: This aborting record waits until all inflight requests have known
// responses. The client must wait to ensure no duplicate sequence number
// issues. For more details, and for an immediate alternative, check the
// documentation on UnsafeAbortBufferedRecords.
func (cl *Client) AbortBufferedRecords(ctx context.Context) error {
	cl.producer.aborting.Add(1)
	defer cl.producer.aborting.Add(-1)

	cl.cfg.logger.Log(LogLevelInfo, "producer state set to aborting; continuing to wait via flushing")
	defer cl.cfg.logger.Log(LogLevelDebug, "aborted buffered records")

	// We must clear unknown topics ourselves, because flush just waits
	// like normal.
	p := &cl.producer
	p.unknownTopicsMu.Lock()
	for _, unknown := range p.unknownTopics {
		select {
		case unknown.fatal <- ErrAborting:
		default:
		}
	}
	p.unknownTopicsMu.Unlock()

	// Setting the aborting state allows records to fail before
	// or after produce requests; thus, now we just flush.
	return cl.Flush(ctx)
}

// UnsafeAbortBufferedRecords fails all unflushed records with ErrAborted and
// waits for there to be no buffered records. This function does NOT wait for
// any inflight produce requests to finish, meaning topics in the client may be
// in an invalid state and producing to an invalid-state topic may cause the
// client to enter a fatal failed state. If you want to produce to topics that
// were unsafely aborted, it is recommended to use PurgeTopicsFromClient to
// forcefully reset the topics before producing to them again.
//
// When producing with idempotency enabled or with transactions, every record
// has a sequence number. The client must wait for inflight requests to have
// responses before failing a record, otherwise the client cannot know if a
// sequence number was seen by the broker and tracked or not seen by the broker
// and not tracked. By unsafely aborting, the client forcefully abandons all
// records, and producing to the topics again may re-use a sequence number and
// cause internal errors.
func (cl *Client) UnsafeAbortBufferedRecords() {
	cl.failBufferedRecords(ErrAborting)
}

// EndTransaction ends a transaction and resets the client's internal state to
// not be in a transaction.
//
// Flush and CommitOffsetsForTransaction must be called before this function;
// this function does not flush and does not itself ensure that all buffered
// records are flushed. If no record yet has caused a partition to be added to
// the transaction, this function does nothing and returns nil. Alternatively,
// AbortBufferedRecords should be called before aborting a transaction to
// ensure that any buffered records not yet flushed will not be a part of a new
// transaction.
//
// If the producer ID has an error and you are trying to commit, this will
// return with kerr.OperationNotAttempted. If this happened, retry
// EndTransaction with TryAbort. If this returns kerr.TransactionAbortable, you
// can retry with TryAbort. No other error is retryable, and you should not
// retry with TryAbort.
//
// If records failed with UnknownProducerID and your Kafka version is at least
// 2.5, then aborting here will potentially allow the client to recover for
// more production.
//
// Note that canceling the context will likely leave the client in an
// undesirable state, because canceling the context may cancel the in-flight
// EndTransaction request, making it impossible to know whether the commit or
// abort was successful. It is recommended to not cancel the context.
func (cl *Client) EndTransaction(ctx context.Context, commit TransactionEndTry) error {
	cl.producer.txnMu.Lock()
	defer cl.producer.txnMu.Unlock()

	if !cl.producer.inTxn {
		return nil
	}
	cl.producer.inTxn = false

	cl.producer.producingTxn.Store(false) // forbid any new produces while ending txn

	// anyAdded tracks if any partitions were added to this txn, because
	// any partitions written to triggers AddPartitionToTxn, which triggers
	// the txn to actually begin within Kafka.
	//
	// If we consumed at all but did not produce, the transaction ending
	// issues AddOffsetsToTxn, which internally adds a __consumer_offsets
	// partition to the transaction. Thus, if we added offsets, then we
	// also produced.
	var anyAdded bool
	if g := cl.consumer.g; g != nil {
		// We do not lock because we expect commitTransactionOffsets to
		// be called *before* ending a transaction.
		if g.offsetsAddedToTxn {
			g.offsetsAddedToTxn = false
			anyAdded = true
		}
	} else {
		cl.cfg.logger.Log(LogLevelDebug, "transaction ending, no group loaded; this must be a producer-only transaction, not consume-modify-produce EOS")
	}

	// After the flush, no records are being produced to, and we can set
	// addedToTxn to false outside of any mutex.
	for _, parts := range cl.producer.topics.load() {
		for _, part := range parts.load().partitions {
			anyAdded = part.records.addedToTxn.Swap(false) || anyAdded
		}
	}

	// If the user previously used EndAndBeginTransaction with
	// EndBeginTxnUnsafe, we may have to end a transaction even though
	// nothing may be in it.
	anyAdded = anyAdded || cl.producer.readded

	// If no partition was added to a transaction, then we have nothing to commit.
	//
	// Note that anyAdded is true if the producer ID was failed, meaning we will
	// get to the potential recovery logic below if necessary.
	if !anyAdded {
		cl.cfg.logger.Log(LogLevelDebug, "no records were produced during the commit; thus no transaction was began; ending without doing anything")
		return nil
	}

	id, epoch, err := cl.producerID(ctx2fn(ctx))
	if err != nil {
		if commit {
			return kerr.OperationNotAttempted
		}

		// If we recovered the producer ID, we return early, since
		// there is no reason to issue an abort now that the id is
		// different. Otherwise, we issue our EndTxn which will likely
		// fail, but that is ok, we will just return error.
		_, didRecover, _ := cl.maybeRecoverProducerID(ctx)
		if didRecover {
			return nil
		}
	}

	cl.cfg.logger.Log(LogLevelInfo, "ending transaction",
		"transactional_id", *cl.cfg.txnID,
		"producer_id", id,
		"epoch", epoch,
		"commit", commit,
	)

	cl.producer.readded = false
	err = cl.doWithConcurrentTransactions(ctx, "EndTxn", func() error {
		req := kmsg.NewPtrEndTxnRequest()
		req.TransactionalID = *cl.cfg.txnID
		req.ProducerID = id
		req.ProducerEpoch = epoch
		req.Commit = bool(commit)
		resp, err := req.RequestWith(ctx, cl)
		if err != nil {
			return err
		}
		return kerr.ErrorForCode(resp.ErrorCode)
	})

	// If the returned error is still a Kafka error, this is fatal and we
	// need to fail our producer ID we loaded above.
	//
	// UNKNOWN_SERVER_ERROR can theoretically be returned (not all brokers
	// do). This technically is fatal, but we do not really know whether it
	// is. We can just return this error and let the caller decide to
	// continue, if the caller does continue, we will try something and
	// eventually then receive our proper transactional error, if any.
	var ke *kerr.Error
	if errors.As(err, &ke) && !ke.Retriable && ke.Code != kerr.UnknownServerError.Code {
		cl.failProducerID(id, epoch, err)
	}

	return err
}

// This returns if it is necessary to recover the producer ID (it has an
// error), whether it is possible to recover, and, if not, the error.
//
// We call this when beginning a transaction or when ending with an abort.
func (cl *Client) maybeRecoverProducerID(ctx context.Context) (necessary, did bool, err error) {
	cl.producer.mu.Lock()
	defer cl.producer.mu.Unlock()

	id, epoch, err := cl.producerID(ctx2fn(ctx))
	if err == nil {
		return false, false, nil
	}

	var ke *kerr.Error
	if ok := errors.As(err, &ke); !ok {
		return true, false, err
	}

	kip360 := cl.producer.idVersion >= 3 && (errors.Is(ke, kerr.UnknownProducerID) || errors.Is(ke, kerr.InvalidProducerIDMapping))
	kip588 := cl.producer.idVersion >= 4 && errors.Is(ke, kerr.InvalidProducerEpoch /* || err == kerr.TransactionTimedOut when implemented in Kafka */)

	recoverable := kip360 || kip588
	if !recoverable {
		return true, false, err // fatal, unrecoverable
	}

	// Storing errReloadProducerID will reset sequence numbers as appropriate
	// when the producer ID is reloaded successfully.
	cl.producer.id.Store(&producerID{
		id:    id,
		epoch: epoch,
		err:   errReloadProducerID,
	})
	return true, true, nil
}

// If a transaction is begun too quickly after finishing an old transaction,
// Kafka may still be finalizing its commit / abort and will return a
// concurrent transactions error. We handle that by retrying for a bit.
func (cl *Client) doWithConcurrentTransactions(ctx context.Context, name string, fn func() error) error {
	start := time.Now()
	tries := 0
	backoff := cl.cfg.txnBackoff

start:
	err := fn()
	if errors.Is(err, kerr.ConcurrentTransactions) {
		// The longer we are stalled, the more we enforce a minimum
		// backoff.
		since := time.Since(start)
		switch {
		case since > time.Second:
			if backoff < 200*time.Millisecond {
				backoff = 200 * time.Millisecond
			}
		case since > 5*time.Second/2:
			if backoff < 500*time.Millisecond {
				backoff = 500 * time.Millisecond
			}
		case since > 5*time.Second:
			if backoff < time.Second {
				backoff = time.Second
			}
		}

		tries++
		cl.cfg.logger.Log(LogLevelDebug, fmt.Sprintf("%s failed with CONCURRENT_TRANSACTIONS, which may be because we ended a txn and began producing in a new txn too quickly; backing off and retrying", name),
			"backoff", backoff,
			"since_request_tries_start", time.Since(start),
			"tries", tries,
		)
		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			cl.cfg.logger.Log(LogLevelError, fmt.Sprintf("abandoning %s retry due to request ctx quitting", name))
			return err
		case <-cl.ctx.Done():
			cl.cfg.logger.Log(LogLevelError, fmt.Sprintf("abandoning %s retry due to client ctx quitting", name))
			return err
		}
		goto start
	}
	return err
}

////////////////////////////////////////////////////////////////////////////////////////////
// TRANSACTIONAL COMMITTING                                                               //
// MOSTLY DUPLICATED CODE DUE TO NO GENERICS AND BECAUSE THE TYPES ARE SLIGHTLY DIFFERENT //
////////////////////////////////////////////////////////////////////////////////////////////

// commitTransactionOffsets is exactly like CommitOffsets, but specifically for
// use with transactional consuming and producing.
//
// Since this function is a gigantic footgun if not done properly, we hide this
// and only allow transaction sessions to commit.
//
// Unlike CommitOffsets, we do not update the group's uncommitted map. We leave
// that to the calling code to do properly with SetOffsets depending on whether
// an eventual abort happens or not.
func (cl *Client) commitTransactionOffsets(
	ctx context.Context,
	uncommitted map[string]map[int32]EpochOffset,
	onDone func(*kmsg.TxnOffsetCommitRequest, *kmsg.TxnOffsetCommitResponse, error),
) *groupConsumer {
	cl.cfg.logger.Log(LogLevelDebug, "in commitTransactionOffsets", "with", uncommitted)
	defer cl.cfg.logger.Log(LogLevelDebug, "left commitTransactionOffsets")

	if cl.cfg.txnID == nil {
		onDone(nil, nil, errNotTransactional)
		return nil
	}

	// Before committing, ensure we are at least in a transaction. We
	// unlock the producer txnMu before committing to allow EndTransaction
	// to go through, even though that could cut off our commit.
	cl.producer.txnMu.Lock()
	var unlockedTxn bool
	unlockTxn := func() {
		if !unlockedTxn {
			cl.producer.txnMu.Unlock()
		}
		unlockedTxn = true
	}
	defer unlockTxn()
	if !cl.producer.inTxn {
		onDone(nil, nil, errNotInTransaction)
		return nil
	}

	g := cl.consumer.g
	if g == nil {
		onDone(kmsg.NewPtrTxnOffsetCommitRequest(), kmsg.NewPtrTxnOffsetCommitResponse(), errNotGroup)
		return nil
	}

	req, err := g.prepareTxnOffsetCommit(ctx, uncommitted)
	if err != nil {
		onDone(req, kmsg.NewPtrTxnOffsetCommitResponse(), err)
		return g
	}
	if len(req.Topics) == 0 {
		onDone(kmsg.NewPtrTxnOffsetCommitRequest(), kmsg.NewPtrTxnOffsetCommitResponse(), nil)
		return g
	}

	if !g.offsetsAddedToTxn {
		if err := cl.addOffsetsToTxn(ctx, g.cfg.group); err != nil {
			if onDone != nil {
				onDone(nil, nil, err)
			}
			return g
		}
		g.offsetsAddedToTxn = true
	}

	unlockTxn()

	if err := g.waitJoinSyncMu(ctx); err != nil {
		onDone(kmsg.NewPtrTxnOffsetCommitRequest(), kmsg.NewPtrTxnOffsetCommitResponse(), err)
		return nil
	}
	unblockJoinSync := func(req *kmsg.TxnOffsetCommitRequest, resp *kmsg.TxnOffsetCommitResponse, err error) {
		g.noCommitDuringJoinAndSync.RUnlock()
		onDone(req, resp, err)
	}
	g.mu.Lock()
	defer g.mu.Unlock()

	g.commitTxn(ctx, req, unblockJoinSync)
	return g
}

// Ties a transactional producer to a group. Since this requires a producer ID,
// this initializes one if it is not yet initialized. This would only be the
// case if trying to commit before any records have been sent.
func (cl *Client) addOffsetsToTxn(ctx context.Context, group string) error {
	id, epoch, err := cl.producerID(ctx2fn(ctx))
	if err != nil {
		return err
	}

	err = cl.doWithConcurrentTransactions(ctx, "AddOffsetsToTxn", func() error { // committing offsets without producing causes a transaction to begin within Kafka
		cl.cfg.logger.Log(LogLevelInfo, "issuing AddOffsetsToTxn",
			"txn", *cl.cfg.txnID,
			"producerID", id,
			"producerEpoch", epoch,
			"group", group,
		)
		req := kmsg.NewPtrAddOffsetsToTxnRequest()
		req.TransactionalID = *cl.cfg.txnID
		req.ProducerID = id
		req.ProducerEpoch = epoch
		req.Group = group
		resp, err := req.RequestWith(ctx, cl)
		if err != nil {
			return err
		}
		return kerr.ErrorForCode(resp.ErrorCode)
	})

	// If the returned error is still a Kafka error, this is fatal and we
	// need to fail our producer ID we created just above.
	//
	// We special case UNKNOWN_SERVER_ERROR, because we do not really know
	// if this is fatal. If it is, we will catch it later on a better
	// error. Some brokers send this when things fail internally, we can
	// just abort our commit and see if things are still bad in
	// EndTransaction.
	var ke *kerr.Error
	if errors.As(err, &ke) && !ke.Retriable && ke.Code != kerr.UnknownServerError.Code {
		cl.failProducerID(id, epoch, err)
	}

	return err
}

// commitTxn is ALMOST EXACTLY THE SAME as commit, but changed for txn types
// and we avoid updateCommitted. We avoid updating because we manually
// SetOffsets when ending the transaction.
func (g *groupConsumer) commitTxn(ctx context.Context, req *kmsg.TxnOffsetCommitRequest, onDone func(*kmsg.TxnOffsetCommitRequest, *kmsg.TxnOffsetCommitResponse, error)) {
	if onDone == nil { // note we must always call onDone
		onDone = func(_ *kmsg.TxnOffsetCommitRequest, _ *kmsg.TxnOffsetCommitResponse, _ error) {}
	}

	if g.commitCancel != nil {
		g.commitCancel() // cancel any prior commit
	}
	priorCancel := g.commitCancel
	priorDone := g.commitDone

	// Unlike the non-txn consumer, we use the group context for
	// transaction offset committing. We want to quit when the group is
	// left, and we are not committing when leaving. We rely on proper
	// usage of the GroupTransactSession API to issue commits, so there is
	// no reason not to use the group context here.
	commitCtx, commitCancel := context.WithCancel(g.ctx) // enable ours to be canceled and waited for
	commitDone := make(chan struct{})

	g.commitCancel = commitCancel
	g.commitDone = commitDone

	if ctx.Done() != nil {
		go func() {
			select {
			case <-ctx.Done():
				commitCancel()
			case <-commitCtx.Done():
			}
		}()
	}

	go func() {
		defer close(commitDone) // allow future commits to continue when we are done
		defer commitCancel()
		if priorDone != nil {
			select {
			case <-priorDone:
			default:
				g.cl.cfg.logger.Log(LogLevelDebug, "canceling prior txn offset commit to issue another")
				priorCancel()
				<-priorDone // wait for any prior request to finish
			}
		}
		g.cl.cfg.logger.Log(LogLevelDebug, "issuing txn offset commit", "uncommitted", req)

		var resp *kmsg.TxnOffsetCommitResponse
		var err error
		if len(req.Topics) > 0 {
			resp, err = req.RequestWith(commitCtx, g.cl)
		}
		if err != nil {
			onDone(req, nil, err)
			return
		}
		onDone(req, resp, nil)
	}()
}

func (g *groupConsumer) prepareTxnOffsetCommit(ctx context.Context, uncommitted map[string]map[int32]EpochOffset) (*kmsg.TxnOffsetCommitRequest, error) {
	req := kmsg.NewPtrTxnOffsetCommitRequest()

	// We're now generating the producerID before addOffsetsToTxn.
	// We will not make this request until after addOffsetsToTxn, but it's possible to fail here due to a failed producerID.
	id, epoch, err := g.cl.producerID(ctx2fn(ctx))
	if err != nil {
		return req, err
	}

	req.TransactionalID = *g.cl.cfg.txnID
	req.Group = g.cfg.group
	req.ProducerID = id
	req.ProducerEpoch = epoch
	memberID, generation := g.memberGen.load()
	req.Generation = generation
	req.MemberID = memberID
	req.InstanceID = g.cfg.instanceID

	for topic, partitions := range uncommitted {
		reqTopic := kmsg.NewTxnOffsetCommitRequestTopic()
		reqTopic.Topic = topic
		for partition, eo := range partitions {
			reqPartition := kmsg.NewTxnOffsetCommitRequestTopicPartition()
			reqPartition.Partition = partition
			reqPartition.Offset = eo.Offset
			reqPartition.LeaderEpoch = eo.Epoch
			reqPartition.Metadata = &req.MemberID
			reqTopic.Partitions = append(reqTopic.Partitions, reqPartition)
		}
		req.Topics = append(req.Topics, reqTopic)
	}

	if fn, ok := ctx.Value(txnCommitContextFn).(func(*kmsg.TxnOffsetCommitRequest) error); ok {
		if err := fn(req); err != nil {
			return req, err
		}
	}
	return req, nil
}
