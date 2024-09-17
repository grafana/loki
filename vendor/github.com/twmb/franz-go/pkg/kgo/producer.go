package kgo

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
)

type producer struct {
	inflight atomicI64 // high 16: # waiters, low 48: # inflight

	// mu and c are used for flush and drain notifications; mu is used for
	// a few other tight locks.
	mu sync.Mutex
	c  *sync.Cond

	bufferedRecords int64
	bufferedBytes   int64

	cl *Client

	topicsMu sync.Mutex // locked to prevent concurrent updates; reads are always atomic
	topics   *topicsPartitions

	// Hooks exist behind a pointer because likely they are not used.
	// We only take up one byte vs. 6.
	hooks *struct {
		buffered    []HookProduceRecordBuffered
		partitioned []HookProduceRecordPartitioned
		unbuffered  []HookProduceRecordUnbuffered
	}

	hasHookBatchWritten bool

	// unknownTopics buffers all records for topics that are not loaded.
	// The map is to a pointer to a slice for reasons documented in
	// waitUnknownTopic.
	unknownTopicsMu sync.Mutex
	unknownTopics   map[string]*unknownTopicProduces

	id           atomic.Value
	producingTxn atomicBool

	// We must have a producer field for flushing; we cannot just have a
	// field on recBufs that is toggled on flush. If we did, then a new
	// recBuf could be created and records sent to while we are flushing.
	flushing     atomicI32 // >0 if flushing, can Flush many times concurrently
	blocked      atomicI32 // >0 if over max recs or bytes
	blockedBytes int64

	aborting atomicI32 // >0 if aborting, can abort many times concurrently

	idMu      sync.Mutex
	idVersion int16

	batchPromises ringBatchPromise
	promisesMu    sync.Mutex

	txnMu sync.Mutex
	inTxn bool

	// If using EndBeginTxnUnsafe, and any partitions are actually produced
	// to, we issue an AddPartitionsToTxn at the end to re-add them to a
	// new transaction. We have to due to logic races: the broker may not
	// have handled the produce requests yet, and we want to ensure a new
	// transaction is started.
	//
	// If the user stops producing, we want to ensure that our restarted
	// transaction is actually ended. Thus, we set readded whenever we have
	// partitions we actually restart. We issue EndTxn and reset readded in
	// EndAndBegin; if nothing more was produced to, we ensure we finish
	// the started txn.
	readded bool
}

// BufferedProduceRecords returns the number of records currently buffered for
// producing within the client.
//
// This can be used as a gauge to determine how far behind the client is for
// flushing records produced by your client (which can help determine network /
// cluster health).
func (cl *Client) BufferedProduceRecords() int64 {
	cl.producer.mu.Lock()
	defer cl.producer.mu.Unlock()
	return cl.producer.bufferedRecords + int64(cl.producer.blocked.Load())
}

// BufferedProduceBytes returns the number of bytes currently buffered for
// producing within the client. This is the sum of all keys, values, and header
// keys/values. See the related [BufferedProduceRecords] for more information.
func (cl *Client) BufferedProduceBytes() int64 {
	cl.producer.mu.Lock()
	defer cl.producer.mu.Unlock()
	return cl.producer.bufferedBytes + cl.producer.blockedBytes
}

type unknownTopicProduces struct {
	buffered []promisedRec
	wait     chan error // retryable errors
	fatal    chan error // must-signal quit errors; capacity 1
}

func (p *producer) init(cl *Client) {
	p.cl = cl
	p.topics = newTopicsPartitions()
	p.unknownTopics = make(map[string]*unknownTopicProduces)
	p.idVersion = -1
	p.id.Store(&producerID{
		id:    -1,
		epoch: -1,
		err:   errReloadProducerID,
	})
	p.c = sync.NewCond(&p.mu)

	inithooks := func() {
		if p.hooks == nil {
			p.hooks = &struct {
				buffered    []HookProduceRecordBuffered
				partitioned []HookProduceRecordPartitioned
				unbuffered  []HookProduceRecordUnbuffered
			}{}
		}
	}

	cl.cfg.hooks.each(func(h Hook) {
		if h, ok := h.(HookProduceRecordBuffered); ok {
			inithooks()
			p.hooks.buffered = append(p.hooks.buffered, h)
		}
		if h, ok := h.(HookProduceRecordPartitioned); ok {
			inithooks()
			p.hooks.partitioned = append(p.hooks.partitioned, h)
		}
		if h, ok := h.(HookProduceRecordUnbuffered); ok {
			inithooks()
			p.hooks.unbuffered = append(p.hooks.unbuffered, h)
		}
		if _, ok := h.(HookProduceBatchWritten); ok {
			p.hasHookBatchWritten = true
		}
	})
}

func (p *producer) purgeTopics(topics []string) {
	p.topicsMu.Lock()
	defer p.topicsMu.Unlock()

	p.unknownTopicsMu.Lock()
	for _, topic := range topics {
		if unknown, exists := p.unknownTopics[topic]; exists {
			delete(p.unknownTopics, topic)
			close(unknown.wait)
			p.promiseBatch(batchPromise{
				recs: unknown.buffered,
				err:  errPurged,
			})
		}
	}
	p.unknownTopicsMu.Unlock()

	toStore := p.topics.clone()
	defer p.topics.storeData(toStore)

	for _, topic := range topics {
		d := toStore.loadTopic(topic)
		if d == nil {
			continue
		}
		delete(toStore, topic)
		for _, p := range d.partitions {
			r := p.records

			// First we set purged, so that anything in the process
			// of being buffered will immediately fail when it goes
			// to buffer.
			r.mu.Lock()
			r.purged = true
			r.mu.Unlock()

			// Now we remove from the sink. When we do, the recBuf
			// is effectively abandonded. Any active produces may
			// finish before we fail the records; if they finish
			// after they will no longer belong in the batch, but
			// they may have been produced. This is the duplicate
			// risk a user runs when purging.
			r.sink.removeRecBuf(r)

			// Once abandonded, we now need to fail anything that
			// was buffered.
			go func() {
				r.mu.Lock()
				defer r.mu.Unlock()
				r.failAllRecords(errPurged)
			}()
		}
	}
}

func (p *producer) isAborting() bool { return p.aborting.Load() > 0 }

func noPromise(*Record, error) {}

// ProduceResult is the result of producing a record in a synchronous manner.
type ProduceResult struct {
	// Record is the produced record. It is always non-nil.
	//
	// If this record was produced successfully, its attrs / offset / id /
	// epoch / etc. fields are filled in on return if possible (i.e. when
	// producing with acks required).
	Record *Record

	// Err is a potential produce error. If this is non-nil, the record was
	// not produced successfully.
	Err error
}

// ProduceResults is a collection of produce results.
type ProduceResults []ProduceResult

// FirstErr returns the first erroring result, if any.
func (rs ProduceResults) FirstErr() error {
	for _, r := range rs {
		if r.Err != nil {
			return r.Err
		}
	}
	return nil
}

// First the first record and error in the produce results.
//
// This function is useful if you only passed one record to ProduceSync.
func (rs ProduceResults) First() (*Record, error) {
	return rs[0].Record, rs[0].Err
}

// ProduceSync is a synchronous produce. See the Produce documentation for an
// in depth description of how producing works.
//
// This function produces all records in one range loop and waits for them all
// to be produced before returning.
func (cl *Client) ProduceSync(ctx context.Context, rs ...*Record) ProduceResults {
	var (
		wg      sync.WaitGroup
		results = make(ProduceResults, 0, len(rs))
		promise = func(r *Record, err error) {
			results = append(results, ProduceResult{r, err})
			wg.Done()
		}
	)

	wg.Add(len(rs))
	for _, r := range rs {
		cl.Produce(ctx, r, promise)
	}
	wg.Wait()

	return results
}

// FirstErrPromise is a helper type to capture only the first failing error
// when producing a batch of records with this type's Promise function.
//
// This is useful for when you only care about any record failing, and can use
// that as a signal (i.e., to abort a batch). The AbortingFirstErrPromise
// function can be used to abort all records as soon as the first error is
// encountered. If you do not need to abort, you can use this type with no
// constructor.
//
// This is similar to using ProduceResult's FirstErr function.
type FirstErrPromise struct {
	wg   sync.WaitGroup
	once atomicBool
	err  error
	cl   *Client
}

// AbortingFirstErrPromise returns a FirstErrPromise that will call the
// client's AbortBufferedRecords function if an error is encountered.
//
// This can be used to quickly exit when any error is encountered, rather than
// waiting while flushing only to discover things errored.
func AbortingFirstErrPromise(cl *Client) *FirstErrPromise {
	return &FirstErrPromise{
		cl: cl,
	}
}

// Promise is a promise for producing that will store the first error
// encountered.
func (f *FirstErrPromise) promise(_ *Record, err error) {
	defer f.wg.Done()
	if err != nil && !f.once.Swap(true) {
		f.err = err
		if f.cl != nil {
			f.wg.Add(1)
			go func() {
				defer f.wg.Done()
				f.cl.AbortBufferedRecords(context.Background())
			}()
		}
	}
}

// Promise returns a promise for producing that will store the first error
// encountered.
//
// The returned promise must eventually be called, because a FirstErrPromise
// does not return from 'Err' until all promises are completed.
func (f *FirstErrPromise) Promise() func(*Record, error) {
	f.wg.Add(1)
	return f.promise
}

// Err waits for all promises to complete and then returns any stored error.
func (f *FirstErrPromise) Err() error {
	f.wg.Wait()
	return f.err
}

// TryProduce is similar to Produce, but rather than blocking if the client
// currently has MaxBufferedRecords or MaxBufferedBytes buffered, this fails
// immediately with ErrMaxBuffered. See the Produce documentation for more
// details.
func (cl *Client) TryProduce(
	ctx context.Context,
	r *Record,
	promise func(*Record, error),
) {
	cl.produce(ctx, r, promise, false)
}

// Produce sends a Kafka record to the topic in the record's Topic field,
// calling an optional `promise` with the record and a potential error when
// Kafka replies. For a synchronous produce, see ProduceSync. Records are
// produced in order per partition if the record is produced successfully.
// Successfully produced records will have their attributes, offset, and
// partition set before the promise is called. All promises are called serially
// (and should be relatively fast). If a record's timestamp is unset, this
// sets the timestamp to time.Now.
//
// If the topic field is empty, the client will use the DefaultProduceTopic; if
// that is also empty, the record is failed immediately. If the record is too
// large to fit in a batch on its own in a produce request, the record will be
// failed with immediately kerr.MessageTooLarge.
//
// If the client is configured to automatically flush the client currently has
// the configured maximum amount of records buffered, Produce will block. The
// context can be used to cancel waiting while records flush to make space. In
// contrast, if flushing is configured, the record will be failed immediately
// with ErrMaxBuffered (this same behavior can be had with TryProduce).
//
// Once a record is buffered into a batch, it can be canceled in three ways:
// canceling the context, the record timing out, or hitting the maximum
// retries. If any of these conditions are hit and it is currently safe to fail
// records, all buffered records for the relevant partition are failed. Only
// the first record's context in a batch is considered when determining whether
// the batch should be canceled. A record is not safe to fail if the client
// is idempotently producing and a request has been sent; in this case, the
// client cannot know if the broker actually processed the request (if so, then
// removing the records from the client will create errors the next time you
// produce).
//
// If the client is transactional and a transaction has not been begun, the
// promise is immediately called with an error corresponding to not being in a
// transaction.
func (cl *Client) Produce(
	ctx context.Context,
	r *Record,
	promise func(*Record, error),
) {
	cl.produce(ctx, r, promise, true)
}

func (cl *Client) produce(
	ctx context.Context,
	r *Record,
	promise func(*Record, error),
	block bool,
) {
	if ctx == nil {
		ctx = context.Background()
	}
	if r.Context == nil {
		r.Context = ctx
	}
	if promise == nil {
		promise = noPromise
	}
	if r.Topic == "" {
		r.Topic = cl.cfg.defaultProduceTopic
	}

	p := &cl.producer
	if p.hooks != nil && len(p.hooks.buffered) > 0 {
		for _, h := range p.hooks.buffered {
			h.OnProduceRecordBuffered(r)
		}
	}

	// We can now fail the rec after the buffered hook.
	if r.Topic == "" {
		p.promiseRecordBeforeBuf(promisedRec{ctx, promise, r}, errNoTopic)
		return
	}
	if cl.cfg.txnID != nil && !p.producingTxn.Load() {
		p.promiseRecordBeforeBuf(promisedRec{ctx, promise, r}, errNotInTransaction)
		return
	}

	userSize := r.userSize()
	if cl.cfg.maxBufferedBytes > 0 && userSize > cl.cfg.maxBufferedBytes {
		p.promiseRecordBeforeBuf(promisedRec{ctx, promise, r}, kerr.MessageTooLarge)
		return
	}

	// We have to grab the produce lock to check if this record will exceed
	// configured limits. We try to keep the logic tight since this is
	// effectively a global lock around producing.
	var (
		nextBufRecs, nextBufBytes int64
		overMaxRecs, overMaxBytes bool

		calcNums = func() {
			nextBufRecs = p.bufferedRecords + 1
			nextBufBytes = p.bufferedBytes + userSize
			overMaxRecs = nextBufRecs > cl.cfg.maxBufferedRecords
			overMaxBytes = cl.cfg.maxBufferedBytes > 0 && nextBufBytes > cl.cfg.maxBufferedBytes
		}
	)
	p.mu.Lock()
	calcNums()
	if overMaxRecs || overMaxBytes {
		if !block || cl.cfg.manualFlushing {
			p.mu.Unlock()
			p.promiseRecordBeforeBuf(promisedRec{ctx, promise, r}, ErrMaxBuffered)
			return
		}

		// Before we potentially unlinger, add that we are blocked to
		// ensure we do NOT start a linger anymore. We THEN wakeup
		// anything that is actively lingering. Note that blocked is
		// also used when finishing promises to see if we need to be
		// notified.
		p.blocked.Add(1)
		p.blockedBytes += userSize
		p.mu.Unlock()

		cl.cfg.logger.Log(LogLevelDebug, "blocking Produce because we are either over max buffered records or max buffered bytes",
			"over_max_records", overMaxRecs,
			"over_max_bytes", overMaxBytes,
		)

		cl.unlingerDueToMaxRecsBuffered()

		// We keep the lock when we exit. If we are flushing, we want
		// this blocked record to be produced before we return from
		// flushing. This blocked record will be accounted for in the
		// bufferedRecords addition below, after being removed from
		// blocked in the goroutine.
		wait := make(chan struct{})
		var quit bool
		go func() {
			defer close(wait)
			p.mu.Lock()
			calcNums()
			for !quit && (overMaxRecs || overMaxBytes) {
				p.c.Wait()
				calcNums()
			}
			p.blocked.Add(-1)
			p.blockedBytes -= userSize
		}()

		drainBuffered := func(err error) {
			p.mu.Lock()
			quit = true
			p.mu.Unlock()
			p.c.Broadcast() // wake the goroutine above
			<-wait
			p.mu.Unlock() // we wait for the goroutine to exit, then unlock again (since the goroutine leaves the mutex locked)
			p.promiseRecordBeforeBuf(promisedRec{ctx, promise, r}, err)
		}

		select {
		case <-wait:
			cl.cfg.logger.Log(LogLevelDebug, "Produce block awoken, we now have space to produce, continuing to partition and produce")
		case <-cl.ctx.Done():
			drainBuffered(ErrClientClosed)
			cl.cfg.logger.Log(LogLevelDebug, "client ctx canceled while blocked in Produce, returning")
			return
		case <-ctx.Done():
			drainBuffered(ctx.Err())
			cl.cfg.logger.Log(LogLevelDebug, "produce ctx canceled while blocked in Produce, returning")
			return
		}
	}
	p.bufferedRecords = nextBufRecs
	p.bufferedBytes = nextBufBytes
	p.mu.Unlock()

	cl.partitionRecord(promisedRec{ctx, promise, r})
}

type batchPromise struct {
	baseOffset int64
	pid        int64
	epoch      int16
	attrs      RecordAttrs
	beforeBuf  bool
	partition  int32
	recs       []promisedRec
	err        error
}

func (p *producer) promiseBatch(b batchPromise) {
	if first := p.batchPromises.push(b); first {
		go p.finishPromises(b)
	}
}

func (p *producer) promiseRecord(pr promisedRec, err error) {
	p.promiseBatch(batchPromise{recs: []promisedRec{pr}, err: err})
}

func (p *producer) promiseRecordBeforeBuf(pr promisedRec, err error) {
	p.promiseBatch(batchPromise{recs: []promisedRec{pr}, beforeBuf: true, err: err})
}

func (p *producer) finishPromises(b batchPromise) {
	cl := p.cl
	var more bool
start:
	p.promisesMu.Lock()
	for i, pr := range b.recs {
		pr.LeaderEpoch = 0
		pr.Offset = b.baseOffset + int64(i)
		pr.Partition = b.partition
		pr.ProducerID = b.pid
		pr.ProducerEpoch = b.epoch
		pr.Attrs = b.attrs
		cl.finishRecordPromise(pr, b.err, b.beforeBuf)
		b.recs[i] = promisedRec{}
	}
	p.promisesMu.Unlock()
	if cap(b.recs) > 4 {
		cl.prsPool.put(b.recs)
	}

	b, more = p.batchPromises.dropPeek()
	if more {
		goto start
	}
}

func (cl *Client) finishRecordPromise(pr promisedRec, err error, beforeBuffering bool) {
	p := &cl.producer

	if p.hooks != nil && len(p.hooks.unbuffered) > 0 {
		for _, h := range p.hooks.unbuffered {
			h.OnProduceRecordUnbuffered(pr.Record, err)
		}
	}

	// Capture user size before potential modification by the promise.
	//
	// We call the promise before finishing the flush notification,
	// allowing users of Flush to know all buf recs are done by the
	// time we notify flush below.
	userSize := pr.userSize()
	pr.promise(pr.Record, err)

	// If this record was never buffered, it's size was never accounted
	// for on any p field: return early.
	if beforeBuffering {
		return
	}

	// Keep the lock as tight as possible: the broadcast can come after.
	p.mu.Lock()
	p.bufferedBytes -= userSize
	p.bufferedRecords--
	broadcast := p.blocked.Load() > 0 || p.bufferedRecords == 0 && p.flushing.Load() > 0
	p.mu.Unlock()

	if broadcast {
		p.c.Broadcast()
	}
}

// partitionRecord loads the partitions for a topic and produce to them. If
// the topic does not currently exist, the record is buffered in unknownTopics
// for a metadata update to deal with.
func (cl *Client) partitionRecord(pr promisedRec) {
	parts, partsData := cl.partitionsForTopicProduce(pr)
	if parts == nil { // saved in unknownTopics
		return
	}
	cl.doPartitionRecord(parts, partsData, pr)
}

// doPartitionRecord is separate so that metadata updates that load unknown
// partitions can call this directly.
func (cl *Client) doPartitionRecord(parts *topicPartitions, partsData *topicPartitionsData, pr promisedRec) {
	if partsData.loadErr != nil && !kerr.IsRetriable(partsData.loadErr) {
		cl.producer.promiseRecord(pr, partsData.loadErr)
		return
	}

	parts.partsMu.Lock()
	defer parts.partsMu.Unlock()
	if parts.partitioner == nil {
		parts.partitioner = cl.cfg.partitioner.ForTopic(pr.Topic)
	}

	mapping := partsData.writablePartitions
	if parts.partitioner.RequiresConsistency(pr.Record) {
		mapping = partsData.partitions
	}
	if len(mapping) == 0 {
		cl.producer.promiseRecord(pr, errors.New("unable to partition record due to no usable partitions"))
		return
	}

	var pick int
	tlp, _ := parts.partitioner.(TopicBackupPartitioner)
	if tlp != nil {
		if parts.lb == nil {
			parts.lb = new(leastBackupInput)
		}
		parts.lb.mapping = mapping
		pick = tlp.PartitionByBackup(pr.Record, len(mapping), parts.lb)
	} else {
		pick = parts.partitioner.Partition(pr.Record, len(mapping))
	}
	if pick < 0 || pick >= len(mapping) {
		cl.producer.promiseRecord(pr, fmt.Errorf("invalid record partitioning choice of %d from %d available", pick, len(mapping)))
		return
	}

	partition := mapping[pick]

	onNewBatch, _ := parts.partitioner.(TopicPartitionerOnNewBatch)
	abortOnNewBatch := onNewBatch != nil
	processed := partition.records.bufferRecord(pr, abortOnNewBatch) // KIP-480
	if !processed {
		onNewBatch.OnNewBatch()

		if tlp != nil {
			parts.lb.mapping = mapping
			pick = tlp.PartitionByBackup(pr.Record, len(mapping), parts.lb)
		} else {
			pick = parts.partitioner.Partition(pr.Record, len(mapping))
		}

		if pick < 0 || pick >= len(mapping) {
			cl.producer.promiseRecord(pr, fmt.Errorf("invalid record partitioning choice of %d from %d available", pick, len(mapping)))
			return
		}
		partition = mapping[pick]
		partition.records.bufferRecord(pr, false) // KIP-480
	}
}

// ProducerID returns, loading if necessary, the current producer ID and epoch.
// This returns an error if the producer ID could not be loaded, if the
// producer ID has fatally errored, or if the context is canceled.
func (cl *Client) ProducerID(ctx context.Context) (int64, int16, error) {
	var (
		id    int64
		epoch int16
		err   error

		done = make(chan struct{})
	)

	go func() {
		defer close(done)
		id, epoch, err = cl.producerID(ctx2fn(ctx))
	}()

	select {
	case <-ctx.Done():
		return 0, 0, ctx.Err()
	case <-done:
		return id, epoch, err
	}
}

type producerID struct {
	id    int64
	epoch int16
	err   error
}

var errReloadProducerID = errors.New("producer id needs reloading")

// initProducerID initializes the client's producer ID for idempotent
// producing only (no transactions, which are more special). After the first
// load, this clears all buffered unknown topics.
func (cl *Client) producerID(ctxFn func() context.Context) (int64, int16, error) {
	p := &cl.producer

	id := p.id.Load().(*producerID)
	if errors.Is(id.err, errReloadProducerID) {
		p.idMu.Lock()
		defer p.idMu.Unlock()

		if id = p.id.Load().(*producerID); errors.Is(id.err, errReloadProducerID) {
			if cl.cfg.disableIdempotency {
				cl.cfg.logger.Log(LogLevelInfo, "skipping producer id initialization because the client was configured to disable idempotent writes")
				id = &producerID{
					id:    -1,
					epoch: -1,
					err:   nil,
				}
				p.id.Store(id)
			} else if cl.cfg.txnID == nil && id.id >= 0 && id.epoch < math.MaxInt16-1 {
				// For the idempotent producer, as specified in KIP-360,
				// if we had an ID, we can bump the epoch locally.
				// If we are at the max epoch, we will ask for a new ID.
				cl.resetAllProducerSequences()
				id = &producerID{
					id:    id.id,
					epoch: id.epoch + 1,
					err:   nil,
				}
				p.id.Store(id)
			} else {
				newID, keep := cl.doInitProducerID(ctxFn, id.id, id.epoch)
				if keep {
					id = newID
					// Whenever we have a new producer ID, we need
					// our sequence numbers to be 0. On the first
					// record produced, this will be true, but if
					// we were signaled to reset the producer ID,
					// then we definitely still need to reset here.
					cl.resetAllProducerSequences()
					p.id.Store(id)
				} else {
					// If we are not keeping the producer ID,
					// we will return our old ID but with a
					// static error that we can check or bubble
					// up where needed.
					id = &producerID{
						id:    id.id,
						epoch: id.epoch,
						err:   &errProducerIDLoadFail{newID.err},
					}
				}
			}
		}
	}

	return id.id, id.epoch, id.err
}

// As seen in KAFKA-12152, if we bump an epoch, we have to reset sequence nums
// for every partition. Otherwise, we will use a new id/epoch for a partition
// and trigger OOOSN errors.
//
// Pre 2.5, this function is only be called if it is acceptable to continue
// on data loss (idempotent producer with no StopOnDataLoss option).
//
// 2.5+, it is safe to call this if the producer ID can be reset (KIP-360),
// in EndTransaction.
func (cl *Client) resetAllProducerSequences() {
	for _, tp := range cl.producer.topics.load() {
		for _, p := range tp.load().partitions {
			p.records.mu.Lock()
			p.records.needSeqReset = true
			p.records.mu.Unlock()
		}
	}
}

func (cl *Client) failProducerID(id int64, epoch int16, err error) {
	p := &cl.producer

	// We do not lock the idMu when failing a producer ID, for two reasons.
	//
	// 1) With how we store below, we do not need to. We only fail if the
	// ID we are failing has not changed and if the ID we are failing has
	// not failed already. Failing outside the lock is the same as failing
	// within the lock.
	//
	// 2) Locking would cause a deadlock, because producerID locks
	// idMu=>recBuf.Mu, whereas we failing while locked within a recBuf in
	// sink.go.
	new := &producerID{
		id:    id,
		epoch: epoch,
		err:   err,
	}
	for {
		current := p.id.Load().(*producerID)
		if current.id != id || current.epoch != epoch {
			cl.cfg.logger.Log(LogLevelInfo, "ignoring a fail producer id request due to current id being different",
				"current_id", current.id,
				"current_epoch", current.epoch,
				"current_err", current.err,
				"fail_id", id,
				"fail_epoch", epoch,
				"fail_err", err,
			)
			return
		}
		if current.err != nil {
			cl.cfg.logger.Log(LogLevelInfo, "ignoring a fail producer id because our producer id has already been failed",
				"current_id", current.id,
				"current_epoch", current.epoch,
				"current_err", current.err,
				"fail_err", err,
			)
			return
		}
		if p.id.CompareAndSwap(current, new) {
			return
		}
	}
}

// doInitProducerID inits the idempotent ID and potentially the transactional
// producer epoch, returning whether to keep the result.
func (cl *Client) doInitProducerID(ctxFn func() context.Context, lastID int64, lastEpoch int16) (*producerID, bool) {
	cl.cfg.logger.Log(LogLevelInfo, "initializing producer id")
	req := kmsg.NewPtrInitProducerIDRequest()
	req.TransactionalID = cl.cfg.txnID
	req.ProducerID = lastID
	req.ProducerEpoch = lastEpoch
	if cl.cfg.txnID != nil {
		req.TransactionTimeoutMillis = int32(cl.cfg.txnTimeout.Milliseconds())
	}

	ctx := ctxFn()
	resp, err := req.RequestWith(ctx, cl)
	if err != nil {
		if errors.Is(err, errUnknownRequestKey) || errors.Is(err, errBrokerTooOld) {
			cl.cfg.logger.Log(LogLevelInfo, "unable to initialize a producer id because the broker is too old or the client is pinned to an old version, continuing without a producer id")
			return &producerID{-1, -1, nil}, true
		}
		if errors.Is(err, errChosenBrokerDead) {
			select {
			case <-cl.ctx.Done():
				cl.cfg.logger.Log(LogLevelInfo, "producer id initialization failure due to dying client", "err", err)
				return &producerID{lastID, lastEpoch, ErrClientClosed}, true
			default:
			}
		}
		cl.cfg.logger.Log(LogLevelInfo, "producer id initialization failure, discarding initialization attempt", "err", err)
		return &producerID{lastID, lastEpoch, err}, false
	}

	if err = kerr.ErrorForCode(resp.ErrorCode); err != nil {
		// We could receive concurrent transactions; this is ignorable
		// and we just want to re-init.
		if kerr.IsRetriable(err) || errors.Is(err, kerr.ConcurrentTransactions) {
			cl.cfg.logger.Log(LogLevelInfo, "producer id initialization resulted in retryable error, discarding initialization attempt", "err", err)
			return &producerID{lastID, lastEpoch, err}, false
		}
		cl.cfg.logger.Log(LogLevelInfo, "producer id initialization errored", "err", err)
		return &producerID{lastID, lastEpoch, err}, true
	}

	cl.cfg.logger.Log(LogLevelInfo, "producer id initialization success", "id", resp.ProducerID, "epoch", resp.ProducerEpoch)

	// We track if this was v3. We do not need to gate this behind a mutex,
	// because the only other use is EndTransaction's read, which is
	// documented to only be called sequentially after producing.
	if cl.producer.idVersion == -1 {
		cl.producer.idVersion = req.Version
	}

	return &producerID{resp.ProducerID, resp.ProducerEpoch, nil}, true
}

// partitionsForTopicProduce returns the topic partitions for a record.
// If the topic is not loaded yet, this buffers the record and returns
// nil, nil.
func (cl *Client) partitionsForTopicProduce(pr promisedRec) (*topicPartitions, *topicPartitionsData) {
	p := &cl.producer
	topic := pr.Topic

	topics := p.topics.load()
	parts, exists := topics[topic]
	if exists {
		if v := parts.load(); len(v.partitions) > 0 {
			return parts, v
		}
	}

	if !exists { // topic did not exist: check again under mu and potentially create it
		p.topicsMu.Lock()
		defer p.topicsMu.Unlock()

		if parts, exists = p.topics.load()[topic]; !exists { // update parts for below
			// Before we store the new topic, we lock unknown
			// topics to prevent a concurrent metadata update
			// seeing our new topic before we are waiting from the
			// addUnknownTopicRecord fn. Otherwise, we would wait
			// and never be re-notified.
			p.unknownTopicsMu.Lock()
			defer p.unknownTopicsMu.Unlock()

			p.topics.storeTopics([]string{topic})
			cl.addUnknownTopicRecord(pr)
			cl.triggerUpdateMetadataNow("forced load because we are producing to a topic for the first time")
			return nil, nil
		}
	}

	// Here, the topic existed, but maybe has not loaded partitions yet. We
	// have to lock unknown topics first to ensure ordering just in case a
	// load has not happened.
	p.unknownTopicsMu.Lock()
	defer p.unknownTopicsMu.Unlock()

	if v := parts.load(); len(v.partitions) > 0 {
		return parts, v
	}
	cl.addUnknownTopicRecord(pr)
	cl.triggerUpdateMetadata(false, "reload trigger due to produce topic still not known")

	return nil, nil // our record is buffered waiting for metadata update; nothing to return
}

// addUnknownTopicRecord adds a record to a topic whose partitions are
// currently unknown. This is always called with the unknownTopicsMu held.
func (cl *Client) addUnknownTopicRecord(pr promisedRec) {
	unknown := cl.producer.unknownTopics[pr.Topic]
	if unknown == nil {
		unknown = &unknownTopicProduces{
			buffered: make([]promisedRec, 0, 100),
			wait:     make(chan error, 5),
			fatal:    make(chan error, 1),
		}
		cl.producer.unknownTopics[pr.Topic] = unknown
	}
	unknown.buffered = append(unknown.buffered, pr)
	if len(unknown.buffered) == 1 {
		go cl.waitUnknownTopic(pr.ctx, pr.Record.Context, pr.Topic, unknown)
	}
}

// waitUnknownTopic waits for a notification
func (cl *Client) waitUnknownTopic(
	pctx context.Context, // context passed to Produce
	rctx context.Context, // context on the record itself
	topic string,
	unknown *unknownTopicProduces,
) {
	cl.cfg.logger.Log(LogLevelInfo, "producing to a new topic for the first time, fetching metadata to learn its partitions", "topic", topic)

	var (
		tries        int
		unknownTries int64
		err          error
		after        <-chan time.Time
	)

	if timeout := cl.cfg.recordTimeout; timeout > 0 {
		timer := time.NewTimer(cl.cfg.recordTimeout)
		defer timer.Stop()
		after = timer.C
	}

	// Ordering: aborting is set first, then unknown topics are manually
	// canceled in a lock. New unknown topics after that lock will see
	// aborting here and immediately cancel themselves.
	if cl.producer.isAborting() {
		err = ErrAborting
	}

	for err == nil {
		select {
		case <-pctx.Done():
			err = pctx.Err()
		case <-rctx.Done():
			err = rctx.Err()
		case <-cl.ctx.Done():
			err = ErrClientClosed
		case <-after:
			err = ErrRecordTimeout
		case err = <-unknown.fatal:
		case retryableErr, ok := <-unknown.wait:
			if !ok {
				cl.cfg.logger.Log(LogLevelInfo, "done waiting for metadata for new topic", "topic", topic)
				return // metadata was successful!
			}
			cl.cfg.logger.Log(LogLevelInfo, "new topic metadata wait failed, retrying wait", "topic", topic, "err", retryableErr)
			tries++
			if int64(tries) >= cl.cfg.recordRetries {
				err = fmt.Errorf("no partitions available after attempting to refresh metadata %d times, last err: %w", tries, retryableErr)
			}
			if cl.cfg.maxUnknownFailures >= 0 && errors.Is(retryableErr, kerr.UnknownTopicOrPartition) {
				unknownTries++
				if unknownTries > cl.cfg.maxUnknownFailures {
					err = retryableErr
				}
			}
		}
	}

	// If we errored above, we come down here to potentially clear the
	// topic wait and fail all buffered records. However, under some
	// extreme conditions, a quickly following metadata update could delete
	// our unknown topic, and then a produce could recreate a new unknown
	// topic. We only delete and finish promises if the pointer in the
	// unknown topic map is still the same.
	p := &cl.producer

	p.unknownTopicsMu.Lock()
	defer p.unknownTopicsMu.Unlock()

	nowUnknown := p.unknownTopics[topic]
	if nowUnknown != unknown {
		return
	}
	cl.cfg.logger.Log(LogLevelInfo, "new topic metadata wait failed, done retrying, failing all records", "topic", topic, "err", err)

	delete(p.unknownTopics, topic)
	p.promiseBatch(batchPromise{
		recs: unknown.buffered,
		err:  err,
	})
}

func (cl *Client) unlingerDueToMaxRecsBuffered() {
	if cl.cfg.linger <= 0 {
		return
	}
	for _, parts := range cl.producer.topics.load() {
		for _, part := range parts.load().partitions {
			part.records.unlingerAndManuallyDrain()
		}
	}
	cl.cfg.logger.Log(LogLevelDebug, "unlingered all partitions due to hitting max buffered")
}

// Flush hangs waiting for all buffered records to be flushed, stopping all
// lingers if necessary.
//
// If the context finishes (Done), this returns the context's error.
//
// This function is safe to call multiple times concurrently, and safe to call
// concurrent with Flush.
func (cl *Client) Flush(ctx context.Context) error {
	p := &cl.producer

	// Signal to finishRecord that we want to be notified once buffered hits 0.
	// Also forbid any new producing to start a linger.
	p.flushing.Add(1)
	defer p.flushing.Add(-1)

	cl.cfg.logger.Log(LogLevelInfo, "flushing")
	defer cl.cfg.logger.Log(LogLevelDebug, "flushed")

	// At this point, if lingering is configured, nothing will _start_ a
	// linger because the producer's flushing atomic int32 is nonzero. We
	// must wake anything that could be lingering up, after which all sinks
	// will loop draining.
	if cl.cfg.linger > 0 || cl.cfg.manualFlushing {
		for _, parts := range p.topics.load() {
			for _, part := range parts.load().partitions {
				part.records.unlingerAndManuallyDrain()
			}
		}
	}

	quit := false
	done := make(chan struct{})
	go func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		defer close(done)

		for !quit && p.bufferedRecords+int64(p.blocked.Load()) > 0 {
			p.c.Wait()
		}
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		p.mu.Lock()
		quit = true
		p.mu.Unlock()
		p.c.Broadcast()
		return ctx.Err()
	}
}

func (p *producer) pause(ctx context.Context) error {
	p.inflight.Add(1 << 48)

	quit := false
	done := make(chan struct{})
	go func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		defer close(done)
		for !quit && p.inflight.Load()&((1<<48)-1) != 0 {
			p.c.Wait()
		}
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		p.mu.Lock()
		quit = true
		p.mu.Unlock()
		p.c.Broadcast()
		p.resume() // dec our inflight
		return ctx.Err()
	}
}

func (p *producer) resume() {
	if p.inflight.Add(-1<<48) == 0 {
		p.cl.allSinksAndSources(func(sns sinkAndSource) {
			sns.sink.maybeDrain()
		})
	}
}

func (p *producer) maybeAddInflight() bool {
	if p.inflight.Load()>>48 > 0 {
		return false
	}
	if p.inflight.Add(1)>>48 > 0 {
		p.decInflight()
		return false
	}
	return true
}

func (p *producer) decInflight() {
	if p.inflight.Add(-1)>>48 > 0 {
		p.mu.Lock()
		p.mu.Unlock() //nolint:gocritic,staticcheck // We use the lock as a barrier, unlocking immediately is safe.
		p.c.Broadcast()
	}
}

// Bumps the tries for all buffered records in the client.
//
// This is called whenever there is a problematic error that would affect the
// state of all buffered records as a whole:
//
//   - if we cannot init a producer ID due to RequestWith errors, producing is useless
//   - if we cannot add partitions to a txn due to RequestWith errors, producing is useless
//
// Note that these are specifically due to RequestWith errors, not due to
// receiving a response that has a retryable error code. That is, if our
// request keeps dying.
func (cl *Client) bumpRepeatedLoadErr(err error) {
	p := &cl.producer

	for _, partitions := range p.topics.load() {
		for _, partition := range partitions.load().partitions {
			partition.records.bumpRepeatedLoadErr(err)
		}
	}
	p.unknownTopicsMu.Lock()
	defer p.unknownTopicsMu.Unlock()
	for _, unknown := range p.unknownTopics {
		select {
		case unknown.wait <- err:
		default:
		}
	}
}

// Clears all buffered records in the client with the given error.
//
// - closing client
// - aborting transaction
// - fatal AddPartitionsToTxn
//
// Because the error fails everything, we also empty our unknown topics and
// delete any topics that were still unknown from the producer's topics.
func (cl *Client) failBufferedRecords(err error) {
	p := &cl.producer

	for _, partitions := range p.topics.load() {
		for _, partition := range partitions.load().partitions {
			recBuf := partition.records
			recBuf.mu.Lock()
			recBuf.failAllRecords(err)
			recBuf.mu.Unlock()
		}
	}

	p.topicsMu.Lock()
	defer p.topicsMu.Unlock()
	p.unknownTopicsMu.Lock()
	defer p.unknownTopicsMu.Unlock()

	toStore := p.topics.clone()
	defer p.topics.storeData(toStore)

	var toFail [][]promisedRec
	for topic, unknown := range p.unknownTopics {
		delete(toStore, topic)
		delete(p.unknownTopics, topic)
		close(unknown.wait)
		toFail = append(toFail, unknown.buffered)
	}

	for _, fail := range toFail {
		p.promiseBatch(batchPromise{
			recs: fail,
			err:  err,
		})
	}
}
