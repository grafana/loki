package kgo

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// Offset is a message offset in a partition.
type Offset struct {
	at       int64
	relative int64
	epoch    int32

	currentEpoch int32 // set by us when mapping offsets to brokers

	noReset    bool
	afterMilli bool
}

// Random negative, only significant within this package.
const atCommitted = -999

// MarshalJSON implements json.Marshaler.
func (o Offset) MarshalJSON() ([]byte, error) {
	if o.relative == 0 {
		return []byte(fmt.Sprintf(`{"At":%d,"Epoch":%d,"CurrentEpoch":%d}`, o.at, o.epoch, o.currentEpoch)), nil
	}
	return []byte(fmt.Sprintf(`{"At":%d,"Relative":%d,"Epoch":%d,"CurrentEpoch":%d}`, o.at, o.relative, o.epoch, o.currentEpoch)), nil
}

// String returns the offset as a string; the purpose of this is for logs.
func (o Offset) String() string {
	if o.relative == 0 {
		return fmt.Sprintf("{%d e%d ce%d}", o.at, o.epoch, o.currentEpoch)
	} else if o.relative > 0 {
		return fmt.Sprintf("{%d+%d e%d ce%d}", o.at, o.relative, o.epoch, o.currentEpoch)
	}
	return fmt.Sprintf("{%d-%d e%d ce%d}", o.at, -o.relative, o.epoch, o.currentEpoch)
}

// EpochOffset returns this offset as an EpochOffset, allowing visibility into
// what this offset actually currently is.
func (o Offset) EpochOffset() EpochOffset {
	return EpochOffset{
		Epoch:  o.epoch,
		Offset: o.at,
	}
}

// NewOffset creates and returns an offset to use in [ConsumePartitions] or
// [ConsumeResetOffset].
//
// The default offset begins at the end.
func NewOffset() Offset {
	return Offset{
		at:    -1,
		epoch: -1,
	}
}

// NoResetOffset returns an offset that can be used as a "none" option for the
// [ConsumeResetOffset] option. By default, NoResetOffset starts consuming from
// the beginning of partitions (similar to NewOffset().AtStart()). This can be
// changed with AtEnd, Relative, etc.
//
// Using this offset will make it such that if OffsetOutOfRange is ever
// encountered while consuming, rather than trying to recover, the client will
// return the error to the user and enter a fatal state (for the affected
// partition).
func NoResetOffset() Offset {
	return Offset{
		at:      -1,
		epoch:   -1,
		noReset: true,
	}
}

// AfterMilli returns an offset that consumes from the first offset after a
// given timestamp. This option is *not* compatible with any At options (nor
// Relative nor WithEpoch); using any of those will clear the special
// millisecond state.
//
// This option can be used to consume at the end of existing partitions, but at
// the start of any new partitions that are created later:
//
//	AfterMilli(time.Now().UnixMilli())
//
// By default when using this offset, if consuming encounters an
// OffsetOutOfRange error, consuming will reset to the first offset after this
// timestamp. You can use NoResetOffset().AfterMilli(...) to instead switch the
// client to a fatal state (for the affected partition).
func (o Offset) AfterMilli(millisec int64) Offset {
	o.at = millisec
	o.relative = 0
	o.epoch = -1
	o.afterMilli = true
	return o
}

// AtStart copies 'o' and returns an offset starting at the beginning of a
// partition.
func (o Offset) AtStart() Offset {
	o.afterMilli = false
	o.at = -2
	return o
}

// AtEnd copies 'o' and returns an offset starting at the end of a partition.
// If you want to consume at the end of the topic as it exists right now, but
// at the beginning of new partitions as they are added to the topic later,
// check out AfterMilli.
func (o Offset) AtEnd() Offset {
	o.afterMilli = false
	o.at = -1
	return o
}

// AtCommitted copies 'o' and returns an offset that is used *only if*
// there is an existing commit. This is only useful for group consumers.
// If a partition being consumed does not have a commit, the partition will
// enter a fatal state and return an error from PollFetches.
//
// Using this function automatically opts into [NoResetOffset].
func (o Offset) AtCommitted() Offset {
	o.noReset = true
	o.afterMilli = false
	o.at = atCommitted
	return o
}

// Relative copies 'o' and returns an offset that starts 'n' relative to what
// 'o' currently is. If 'o' is at the end (from [AtEnd]), Relative(-100) will
// begin 100 before the end.
func (o Offset) Relative(n int64) Offset {
	o.afterMilli = false
	o.relative = n
	return o
}

// WithEpoch copies 'o' and returns an offset with the given epoch.  to use the
// given epoch. This epoch is used for truncation detection; the default of -1
// implies no truncation detection.
func (o Offset) WithEpoch(e int32) Offset {
	o.afterMilli = false
	if e < 0 {
		e = -1
	}
	o.epoch = e
	return o
}

// At returns a copy of the calling offset, changing the returned offset to
// begin at exactly the requested offset.
//
// There are two potential special offsets to use: -2 allows for consuming at
// the start, and -1 allows for consuming at the end. These two offsets are
// equivalent to calling AtStart or AtEnd.
//
// If the offset is less than -2, the client bounds it to -2 to consume at the
// start.
func (o Offset) At(at int64) Offset {
	o.afterMilli = false
	if at < -2 {
		at = -2
	}
	o.at = at
	return o
}

type consumer struct {
	bufferedRecords atomicI64
	bufferedBytes   atomicI64

	cl *Client

	pausedMu sync.Mutex   // grabbed when updating paused
	paused   atomic.Value // loaded when issuing fetches

	// mu is grabbed when
	//  - polling fetches, for quickly draining sources / updating group uncommitted
	//  - calling assignPartitions (group / direct updates)
	mu sync.Mutex
	d  *directConsumer // if non-nil, we are consuming partitions directly
	g  *groupConsumer  // if non-nil, we are consuming as a group member

	// On metadata update, if the consumer is set (direct or group), the
	// client begins a goroutine that updates the consumer kind's
	// assignments.
	//
	// This is done in a goroutine to not block the metadata loop, because
	// the update **could** wait on a group consumer leaving if a
	// concurrent LeaveGroup is called, or if restarting a session takes
	// just a little bit of time.
	//
	// The update realistically should be instantaneous, but if it is slow,
	// some metadata updates could pile up. We loop with our atomic work
	// loop, which collapses repeated updates into one extra update, so we
	// loop as little as necessary.
	outstandingMetadataUpdates workLoop

	// sessionChangeMu is grabbed when a session is stopped and held through
	// when a session can be started again. The sole purpose is to block an
	// assignment change running concurrently with a metadata update.
	sessionChangeMu sync.Mutex

	session atomic.Value // *consumerSession
	kill    atomic.Bool

	usingCursors usedCursors

	sourcesReadyMu          sync.Mutex
	sourcesReadyCond        *sync.Cond
	sourcesReadyForDraining []*source
	fakeReadyForDraining    []Fetch

	pollWaitMu    sync.Mutex
	pollWaitC     *sync.Cond
	pollWaitState uint64 // 0 == nothing, low 32 bits: # pollers, high 32: # waiting rebalances
}

func (c *consumer) loadPaused() pausedTopics   { return c.paused.Load().(pausedTopics) }
func (c *consumer) clonePaused() pausedTopics  { return c.paused.Load().(pausedTopics).clone() }
func (c *consumer) storePaused(p pausedTopics) { c.paused.Store(p) }

func (c *consumer) waitAndAddPoller() {
	if !c.cl.cfg.blockRebalanceOnPoll {
		return
	}
	c.pollWaitMu.Lock()
	defer c.pollWaitMu.Unlock()
	for c.pollWaitState>>32 != 0 {
		c.pollWaitC.Wait()
	}
	// Rebalance always takes priority, but if there are no active
	// rebalances, our poll blocks rebalances.
	c.pollWaitState++
}

func (c *consumer) unaddPoller() {
	if !c.cl.cfg.blockRebalanceOnPoll {
		return
	}
	c.pollWaitMu.Lock()
	defer c.pollWaitMu.Unlock()
	c.pollWaitState--
	c.pollWaitC.Broadcast()
}

func (c *consumer) allowRebalance() {
	if !c.cl.cfg.blockRebalanceOnPoll {
		return
	}
	c.pollWaitMu.Lock()
	defer c.pollWaitMu.Unlock()
	// When allowing rebalances, the user is explicitly saying all pollers
	// are done. We mask them out.
	c.pollWaitState &= math.MaxUint32 << 32
	c.pollWaitC.Broadcast()
}

func (c *consumer) waitAndAddRebalance() {
	if !c.cl.cfg.blockRebalanceOnPoll {
		return
	}
	c.pollWaitMu.Lock()
	defer c.pollWaitMu.Unlock()
	c.pollWaitState += 1 << 32
	for c.pollWaitState&math.MaxUint32 != 0 {
		c.pollWaitC.Wait()
	}
}

func (c *consumer) unaddRebalance() {
	if !c.cl.cfg.blockRebalanceOnPoll {
		return
	}
	c.pollWaitMu.Lock()
	defer c.pollWaitMu.Unlock()
	c.pollWaitState -= 1 << 32
	c.pollWaitC.Broadcast()
}

// BufferedFetchRecords returns the number of records currently buffered from
// fetching within the client.
//
// This can be used as a gauge to determine how behind your application is for
// processing records the client has fetched. Note that it is perfectly normal
// to see a spike of buffered records, which would correspond to a fetch
// response being processed just before a call to this function. It is only
// problematic if for you if this function is consistently returning large
// values.
func (cl *Client) BufferedFetchRecords() int64 {
	return cl.consumer.bufferedRecords.Load()
}

// BufferedFetchBytes returns the number of bytes currently buffered from
// fetching within the client. This is the sum of all keys, values, and header
// keys/values. See the related [BufferedFetchRecords] for more information.
func (cl *Client) BufferedFetchBytes() int64 {
	return cl.consumer.bufferedBytes.Load()
}

type usedCursors map[*cursor]struct{}

func (u *usedCursors) use(c *cursor) {
	if *u == nil {
		*u = make(map[*cursor]struct{})
	}
	(*u)[c] = struct{}{}
}

func (c *consumer) init(cl *Client) {
	c.cl = cl
	c.paused.Store(make(pausedTopics))
	c.sourcesReadyCond = sync.NewCond(&c.sourcesReadyMu)
	c.pollWaitC = sync.NewCond(&c.pollWaitMu)

	if len(cl.cfg.topics) > 0 || len(cl.cfg.partitions) > 0 {
		defer cl.triggerUpdateMetadataNow("querying metadata for consumer initialization") // we definitely want to trigger a metadata update
	}

	if len(cl.cfg.group) == 0 {
		c.initDirect()
	} else {
		c.initGroup()
	}
}

func (c *consumer) consuming() bool {
	return c.g != nil || c.d != nil
}

// addSourceReadyForDraining tracks that a source needs its buffered fetch
// consumed.
func (c *consumer) addSourceReadyForDraining(source *source) {
	c.sourcesReadyMu.Lock()
	c.sourcesReadyForDraining = append(c.sourcesReadyForDraining, source)
	c.sourcesReadyMu.Unlock()
	c.sourcesReadyCond.Broadcast()
}

// addFakeReadyForDraining saves a fake fetch that has important partition
// errors--data loss or auth failures.
func (c *consumer) addFakeReadyForDraining(topic string, partition int32, err error, why string) {
	c.cl.cfg.logger.Log(LogLevelInfo, "injecting fake fetch with an error", "err", err, "why", why)
	c.sourcesReadyMu.Lock()
	c.fakeReadyForDraining = append(c.fakeReadyForDraining, Fetch{Topics: []FetchTopic{{
		Topic: topic,
		Partitions: []FetchPartition{{
			Partition: partition,
			Err:       err,
		}},
	}}})
	c.sourcesReadyMu.Unlock()
	c.sourcesReadyCond.Broadcast()
}

// NewErrFetch returns a fake fetch containing a single empty topic with a
// single zero partition with the given error.
func NewErrFetch(err error) Fetches {
	return []Fetch{{
		Topics: []FetchTopic{{
			Topic: "",
			Partitions: []FetchPartition{{
				Partition: -1,
				Err:       err,
			}},
		}},
	}}
}

// PollFetches waits for fetches to be available, returning as soon as any
// broker returns a fetch. If the context is nil, this function will return
// immediately with any currently buffered records.
//
// If the client is closed, a fake fetch will be injected that has no topic, a
// partition of 0, and a partition error of ErrClientClosed. If the context is
// canceled, a fake fetch will be injected with ctx.Err. These injected errors
// can be used to break out of a poll loop.
//
// It is important to check all partition errors in the returned fetches. If
// any partition has a fatal error and actually had no records, fake fetch will
// be injected with the error.
//
// If you are group consuming, a rebalance can happen under the hood while you
// process the returned fetches. This can result in duplicate work, and you may
// accidentally commit to partitions that you no longer own. You can prevent
// this by using BlockRebalanceOnPoll, but this comes with different tradeoffs.
// See the documentation on BlockRebalanceOnPoll for more information.
func (cl *Client) PollFetches(ctx context.Context) Fetches {
	return cl.PollRecords(ctx, 0)
}

// PollRecords waits for records to be available, returning as soon as any
// broker returns records in a fetch. If the context is nil, this function will
// return immediately with any currently buffered records.
//
// If the client is closed, a fake fetch will be injected that has no topic, a
// partition of -1, and a partition error of ErrClientClosed. If the context is
// canceled, a fake fetch will be injected with ctx.Err. These injected errors
// can be used to break out of a poll loop.
//
// This returns a maximum of maxPollRecords total across all fetches, or
// returns all buffered records if maxPollRecords is <= 0.
//
// It is important to check all partition errors in the returned fetches. If
// any partition has a fatal error and actually had no records, fake fetch will
// be injected with the error.
//
// If you are group consuming, a rebalance can happen under the hood while you
// process the returned fetches. This can result in duplicate work, and you may
// accidentally commit to partitions that you no longer own. You can prevent
// this by using BlockRebalanceOnPoll, but this comes with different tradeoffs.
// See the documentation on BlockRebalanceOnPoll for more information.
func (cl *Client) PollRecords(ctx context.Context, maxPollRecords int) Fetches {
	if maxPollRecords == 0 {
		maxPollRecords = -1
	}
	c := &cl.consumer

	c.g.undirtyUncommitted()

	// If the user gave us a canceled context, we bail immediately after
	// un-dirty-ing marked records.
	if ctx != nil {
		select {
		case <-ctx.Done():
			return NewErrFetch(ctx.Err())
		default:
		}
	}

	var fetches Fetches
	fill := func() {
		if c.cl.cfg.blockRebalanceOnPoll {
			c.waitAndAddPoller()
			defer func() {
				if len(fetches) == 0 {
					c.unaddPoller()
				}
			}()
		}

		paused := c.loadPaused()

		// A group can grab the consumer lock then the group mu and
		// assign partitions. The group mu is grabbed to update its
		// uncommitted map. Assigning partitions clears sources ready
		// for draining.
		//
		// We need to grab the consumer mu to ensure proper lock
		// ordering and prevent lock inversion. Polling fetches also
		// updates the group's uncommitted map; if we do not grab the
		// consumer mu at the top, we have a problem: without the lock,
		// we could have grabbed some sources, then a group assigned,
		// and after the assign, we update uncommitted with fetches
		// from the old assignment
		c.mu.Lock()
		defer c.mu.Unlock()

		c.sourcesReadyMu.Lock()
		if maxPollRecords < 0 {
			for _, ready := range c.sourcesReadyForDraining {
				fetches = append(fetches, ready.takeBuffered(paused))
			}
			c.sourcesReadyForDraining = nil
		} else {
			for len(c.sourcesReadyForDraining) > 0 && maxPollRecords > 0 {
				source := c.sourcesReadyForDraining[0]
				fetch, taken, drained := source.takeNBuffered(paused, maxPollRecords)
				if drained {
					c.sourcesReadyForDraining = c.sourcesReadyForDraining[1:]
				}
				maxPollRecords -= taken
				fetches = append(fetches, fetch)
			}
		}

		realFetches := fetches

		fetches = append(fetches, c.fakeReadyForDraining...)
		c.fakeReadyForDraining = nil

		c.sourcesReadyMu.Unlock()

		if len(realFetches) == 0 {
			return
		}

		// Before returning, we want to update our uncommitted. If we
		// updated after, then we could end up with weird interactions
		// with group invalidations where we return a stale fetch after
		// committing in onRevoke.
		//
		// A blocking onRevoke commit, on finish, allows a new group
		// session to start. If we returned stale fetches that did not
		// have their uncommitted offset tracked, then we would allow
		// duplicates.
		if c.g != nil {
			c.g.updateUncommitted(realFetches)
		}
	}

	// We try filling fetches once before waiting. If we have no context,
	// we guarantee that we just drain anything available and return.
	fill()
	if len(fetches) > 0 || ctx == nil {
		return fetches
	}

	done := make(chan struct{})
	quit := false
	go func() {
		c.sourcesReadyMu.Lock()
		defer c.sourcesReadyMu.Unlock()
		defer close(done)

		for !quit && len(c.sourcesReadyForDraining) == 0 && len(c.fakeReadyForDraining) == 0 {
			c.sourcesReadyCond.Wait()
		}
	}()

	exit := func() {
		c.sourcesReadyMu.Lock()
		quit = true
		c.sourcesReadyMu.Unlock()
		c.sourcesReadyCond.Broadcast()
	}

	select {
	case <-cl.ctx.Done():
		exit()
		return NewErrFetch(ErrClientClosed)
	case <-ctx.Done():
		exit()
		return NewErrFetch(ctx.Err())
	case <-done:
	}

	fill()
	return fetches
}

// AllowRebalance allows a consumer group to rebalance if it was blocked by you
// polling records in tandem with the BlockRebalanceOnPoll option.
//
// You can poll many times before calling this function; this function
// internally resets the poll count and allows any blocked rebalances to
// continue. Rebalances take priority: if a rebalance is blocked, and you allow
// rebalances and then immediately poll, your poll will be blocked until the
// rebalance completes. Internally, this function simply waits for lost
// partitions to stop being fetched before allowing you to poll again.
func (cl *Client) AllowRebalance() {
	cl.consumer.allowRebalance()
}

// UpdateFetchMaxBytes updates the max bytes that a fetch request will ask for
// and the max partition bytes that a fetch request will ask for each
// partition.
func (cl *Client) UpdateFetchMaxBytes(maxBytes, maxPartBytes int32) {
	cl.cfg.maxBytes.store(maxBytes)
	cl.cfg.maxPartBytes.store(maxPartBytes)
}

// PauseFetchTopics sets the client to no longer fetch the given topics and
// returns all currently paused topics. Paused topics persist until resumed.
// You can call this function with no topics to simply receive the list of
// currently paused topics.
//
// Pausing topics is independent from pausing individual partitions with the
// PauseFetchPartitions method. If you pause partitions for a topic with
// PauseFetchPartitions, and then pause that same topic with PauseFetchTopics,
// the individually paused partitions will not be unpaused if you only call
// ResumeFetchTopics.
func (cl *Client) PauseFetchTopics(topics ...string) []string {
	c := &cl.consumer
	if len(topics) == 0 {
		return c.loadPaused().pausedTopics()
	}
	c.pausedMu.Lock()
	defer c.pausedMu.Unlock()
	paused := c.clonePaused()
	paused.addTopics(topics...)
	c.storePaused(paused)
	return paused.pausedTopics()
}

// PauseFetchPartitions sets the client to no longer fetch the given partitions
// and returns all currently paused partitions. Paused partitions persist until
// resumed. You can call this function with no partitions to simply receive the
// list of currently paused partitions.
//
// Pausing individual partitions is independent from pausing topics with the
// PauseFetchTopics method. If you pause partitions for a topic with
// PauseFetchPartitions, and then pause that same topic with PauseFetchTopics,
// the individually paused partitions will not be unpaused if you only call
// ResumeFetchTopics.
func (cl *Client) PauseFetchPartitions(topicPartitions map[string][]int32) map[string][]int32 {
	c := &cl.consumer
	if len(topicPartitions) == 0 {
		return c.loadPaused().pausedPartitions()
	}
	c.pausedMu.Lock()
	defer c.pausedMu.Unlock()
	paused := c.clonePaused()
	paused.addPartitions(topicPartitions)
	c.storePaused(paused)
	return paused.pausedPartitions()
}

// ResumeFetchTopics resumes fetching the input topics if they were previously
// paused. Resuming topics that are not currently paused is a per-topic no-op.
// See the documentation on PauseFetchTopics for more details.
func (cl *Client) ResumeFetchTopics(topics ...string) {
	defer cl.allSinksAndSources(func(sns sinkAndSource) {
		sns.source.maybeConsume()
	})

	c := &cl.consumer
	c.pausedMu.Lock()
	defer c.pausedMu.Unlock()

	paused := c.clonePaused()
	paused.delTopics(topics...)
	c.storePaused(paused)
}

// ResumeFetchPartitions resumes fetching the input partitions if they were
// previously paused. Resuming partitions that are not currently paused is a
// per-topic no-op. See the documentation on PauseFetchPartitions for more
// details.
func (cl *Client) ResumeFetchPartitions(topicPartitions map[string][]int32) {
	defer cl.allSinksAndSources(func(sns sinkAndSource) {
		sns.source.maybeConsume()
	})

	c := &cl.consumer
	c.pausedMu.Lock()
	defer c.pausedMu.Unlock()

	paused := c.clonePaused()
	paused.delPartitions(topicPartitions)
	c.storePaused(paused)
}

// SetOffsets sets any matching offsets in setOffsets to the given
// epoch/offset. Partitions that are not specified are not set. It is invalid
// to set topics that were not yet returned from a PollFetches: this function
// sets only partitions that were previously consumed, any extra partitions are
// skipped.
//
// If directly consuming, this function operates as expected given the caveats
// of the prior paragraph.
//
// If using transactions, it is advised to just use a GroupTransactSession and
// avoid this function entirely.
//
// If using group consuming, It is strongly recommended to use this function
// outside of the context of a PollFetches loop and only when you know the
// group is not revoked (i.e., block any concurrent revoke while issuing this
// call) and to not use this concurrent with committing. Any other usage is
// prone to odd interactions.
func (cl *Client) SetOffsets(setOffsets map[string]map[int32]EpochOffset) {
	cl.setOffsets(setOffsets, true)
}

func (cl *Client) setOffsets(setOffsets map[string]map[int32]EpochOffset, log bool) {
	if len(setOffsets) == 0 {
		return
	}

	// We assignPartitions before returning, so we grab the consumer lock
	// first to preserve consumer mu => group mu ordering, or to ensure
	// no concurrent metadata assign for direct consuming.
	c := &cl.consumer
	c.mu.Lock()
	defer c.mu.Unlock()

	var assigns map[string]map[int32]Offset
	var tps *topicsPartitions
	switch {
	case c.d != nil:
		assigns = c.d.getSetAssigns(setOffsets)
		tps = c.d.tps
	case c.g != nil:
		assigns = c.g.getSetAssigns(setOffsets)
		tps = c.g.tps
	}
	if len(assigns) == 0 {
		return
	}
	if log {
		c.assignPartitions(assigns, assignSetMatching, tps, "from manual SetOffsets")
	} else {
		c.assignPartitions(assigns, assignSetMatching, tps, "")
	}
}

// This is guaranteed to be called in a blocking metadata fn, which ensures
// that metadata does not load the tps we are changing. Basically, we ensure
// everything w.r.t. consuming is at a stand still.
func (c *consumer) purgeTopics(topics []string) {
	if c.g == nil && c.d == nil {
		return
	}

	purgeAssignments := make(map[string]map[int32]Offset, len(topics))
	for _, topic := range topics {
		purgeAssignments[topic] = nil
	}

	c.waitAndAddRebalance()
	defer c.unaddRebalance()

	c.mu.Lock()
	defer c.mu.Unlock()

	// The difference for groups is we need to lock the group and there is
	// a slight type difference in g.using vs d.using.
	if c.g != nil {
		c.g.mu.Lock()
		defer c.g.mu.Unlock()
		c.assignPartitions(purgeAssignments, assignPurgeMatching, c.g.tps, fmt.Sprintf("purge of %v requested", topics))
		for _, topic := range topics {
			delete(c.g.using, topic)
			delete(c.g.reSeen, topic)
		}
		c.g.rejoin("rejoin from PurgeFetchTopics")
	} else {
		c.assignPartitions(purgeAssignments, assignPurgeMatching, c.d.tps, fmt.Sprintf("purge of %v requested", topics))
		for _, topic := range topics {
			delete(c.d.using, topic)
			delete(c.d.reSeen, topic)
			delete(c.d.m, topic)
		}
	}
}

// AddConsumeTopics adds new topics to be consumed. This function is a no-op if
// the client is configured to consume via regex.
//
// Note that if you are directly consuming and specified ConsumePartitions,
// this function will not add the rest of the partitions for a topic unless the
// topic has been previously purged. That is, if you directly consumed only one
// of five partitions originally, this will not add the other four until the
// entire topic is purged.
func (cl *Client) AddConsumeTopics(topics ...string) {
	c := &cl.consumer
	if len(topics) == 0 || c.g == nil && c.d == nil || cl.cfg.regex {
		return
	}

	// We can do this outside of the metadata loop because we are strictly
	// adding new topics and forbid regex consuming.
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.g != nil {
		c.g.tps.storeTopics(topics)
	} else {
		c.d.tps.storeTopics(topics)
		for _, topic := range topics {
			c.d.m.addt(topic)
		}
	}
	cl.triggerUpdateMetadataNow("from AddConsumeTopics")
}

// GetConsumeTopics retrives a list of current topics being consumed.
func (cl *Client) GetConsumeTopics() []string {
	c := &cl.consumer
	if c.g == nil && c.d == nil {
		return nil
	}
	var m map[string]*topicPartitions
	var ok bool
	if c.g != nil {
		m, ok = c.g.tps.v.Load().(topicsPartitionsData)
	} else {
		m, ok = c.d.tps.v.Load().(topicsPartitionsData)
	}
	if !ok {
		return nil
	}
	topics := make([]string, 0, len(m))
	for k := range m {
		topics = append(topics, k)
	}
	return topics
}

// AddConsumePartitions adds new partitions to be consumed at the given
// offsets. This function works only for direct, non-regex consumers.
func (cl *Client) AddConsumePartitions(partitions map[string]map[int32]Offset) {
	c := &cl.consumer
	if c.d == nil || cl.cfg.regex {
		return
	}
	var topics []string
	for t, ps := range partitions {
		if len(ps) == 0 {
			delete(partitions, t)
			continue
		}
		topics = append(topics, t)
	}
	if len(partitions) == 0 {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.d.tps.storeTopics(topics)
	for t, ps := range partitions {
		if c.d.ps[t] == nil {
			c.d.ps[t] = make(map[int32]Offset)
		}
		for p, o := range ps {
			c.d.m.add(t, p)
			c.d.ps[t][p] = o
		}
	}
	cl.triggerUpdateMetadataNow("from AddConsumePartitions")
}

// RemoveConsumePartitions removes partitions from being consumed. This
// function works only for direct, non-regex consumers.
//
// This method does not purge the concept of any topics from the client -- if
// you remove all partitions from a topic that was being consumed, metadata
// fetches will still occur for the topic. If you want to remove the topic
// entirely, use PurgeTopicsFromClient.
//
// If you specified ConsumeTopics and this function removes all partitions for
// a topic, the topic will no longer be consumed.
func (cl *Client) RemoveConsumePartitions(partitions map[string][]int32) {
	c := &cl.consumer
	if c.d == nil || cl.cfg.regex {
		return
	}
	for t, ps := range partitions {
		if len(ps) == 0 {
			delete(partitions, t)
			continue
		}
	}
	if len(partitions) == 0 {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	removeOffsets := make(map[string]map[int32]Offset, len(partitions))
	for t, ps := range partitions {
		removePartitionOffsets := make(map[int32]Offset, len(ps))
		for _, p := range ps {
			removePartitionOffsets[p] = Offset{}
		}
		removeOffsets[t] = removePartitionOffsets
	}

	c.assignPartitions(removeOffsets, assignInvalidateMatching, c.d.tps, fmt.Sprintf("remove of %v requested", partitions))
	for t, ps := range partitions {
		for _, p := range ps {
			c.d.using.remove(t, p)
			c.d.m.remove(t, p)
			delete(c.d.ps[t], p)
		}
		if len(c.d.ps[t]) == 0 {
			delete(c.d.ps, t)
		}
	}
}

// assignHow controls how assignPartitions operates.
type assignHow int8

const (
	// This option simply assigns new offsets, doing nothing with existing
	// offsets / active fetches / buffered fetches.
	assignWithoutInvalidating assignHow = iota

	// This option invalidates active fetches so they will not buffer and
	// drops all buffered fetches, and then continues to assign the new
	// assignments.
	assignInvalidateAll

	// This option does not assign, but instead invalidates any active
	// fetches for "assigned" (actually lost) partitions. This additionally
	// drops all buffered fetches, because they could contain partitions we
	// lost. Thus, with this option, the actual offset in the map is
	// meaningless / a dummy offset.
	assignInvalidateMatching

	assignPurgeMatching

	// The counterpart to assignInvalidateMatching, assignSetMatching
	// resets all matching partitions to the specified offset / epoch.
	assignSetMatching
)

func (h assignHow) String() string {
	switch h {
	case assignWithoutInvalidating:
		return "assigning everything new, keeping current assignment"
	case assignInvalidateAll:
		return "unassigning everything"
	case assignInvalidateMatching:
		return "unassigning any currently assigned matching partition that is in the input"
	case assignPurgeMatching:
		return "unassigning and purging any partition matching the input topics"
	case assignSetMatching:
		return "reassigning any currently assigned matching partition to the input"
	}
	return ""
}

type fmtAssignment map[string]map[int32]Offset

func (f fmtAssignment) String() string {
	var sb strings.Builder

	var topicsWritten int
	for topic, partitions := range f {
		topicsWritten++
		sb.WriteString(topic)
		sb.WriteString("[")

		var partitionsWritten int
		for partition, offset := range partitions {
			fmt.Fprintf(&sb, "%d%s", partition, offset)
			partitionsWritten++
			if partitionsWritten < len(partitions) {
				sb.WriteString(" ")
			}
		}

		sb.WriteString("]")
		if topicsWritten < len(f) {
			sb.WriteString(", ")
		}
	}

	return sb.String()
}

// assignPartitions, called under the consumer's mu, is used to set new cursors
// or add to the existing cursors.
//
// We do not need to pass tps when we are bumping the session or when we are
// invalidating all. All other cases, we want the tps -- the logic below does
// not fully differentiate needing to start a new session vs. just reusing the
// old (third if case below)
func (c *consumer) assignPartitions(assignments map[string]map[int32]Offset, how assignHow, tps *topicsPartitions, why string) {
	if c.mu.TryLock() {
		c.mu.Unlock()
		panic("assignPartitions called without holding the consumer lock, this is a bug in franz-go, please open an issue at github.com/twmb/franz-go")
	}

	// The internal code can avoid giving an assign reason in cases where
	// the caller logs itself immediately before assigning. We only log if
	// there is a reason.
	if len(why) > 0 {
		c.cl.cfg.logger.Log(LogLevelInfo, "assigning partitions",
			"why", why,
			"how", how,
			"input", fmtAssignment(assignments),
		)
	}
	var session *consumerSession
	var loadOffsets listOrEpochLoads

	defer func() {
		if session == nil { // if nil, we stopped the session
			session = c.startNewSession(tps)
		} else { // else we guarded it
			c.unguardSessionChange(session)
		}
		loadOffsets.loadWithSession(session, "loading offsets in new session from assign") // odds are this assign came from a metadata update, so no reason to force a refresh with loadWithSessionNow

		// If we started a new session or if we unguarded, we have one
		// worker. This one worker allowed us to safely add our load
		// offsets before the session could be concurrently stopped
		// again. Now that we have added the load offsets, we allow the
		// session to be stopped.
		session.decWorker()
	}()

	if how == assignWithoutInvalidating {
		// Guarding a session change can actually create a new session
		// if we had no session before, which is why we need to pass in
		// our topicPartitions.
		session = c.guardSessionChange(tps)
	} else {
		loadOffsets, _ = c.stopSession()

		// First, over all cursors currently in use, we unset them or set them
		// directly as appropriate. Anything we do not unset, we keep.

		var keep usedCursors
		for usedCursor := range c.usingCursors {
			shouldKeep := true
			if how == assignInvalidateAll {
				usedCursor.unset()
				shouldKeep = false
			} else { // invalidateMatching or setMatching
				if assignTopic, ok := assignments[usedCursor.topic]; ok {
					if how == assignPurgeMatching { // topic level
						usedCursor.source.removeCursor(usedCursor)
						shouldKeep = false
					} else if assignPart, ok := assignTopic[usedCursor.partition]; ok {
						if how == assignInvalidateMatching {
							usedCursor.unset()
							shouldKeep = false
						} else { // how == assignSetMatching
							usedCursor.setOffset(cursorOffset{
								offset:            assignPart.at,
								lastConsumedEpoch: assignPart.epoch,
							})
						}
					}
				}
			}
			if shouldKeep {
				keep.use(usedCursor)
			}
		}
		c.usingCursors = keep

		// For any partition that was listing offsets or loading
		// epochs, we want to ensure that if we are keeping those
		// partitions, we re-start the list/load.
		//
		// Note that we do not need to unset cursors here; anything
		// that actually resulted in a cursor is forever tracked in
		// usedCursors. We only do not have used cursors if an
		// assignment went straight to listing / epoch loading, and
		// that list/epoch never finished.
		switch how {
		case assignWithoutInvalidating:
			// Nothing to do -- this is handled above.
		case assignInvalidateAll:
			loadOffsets = listOrEpochLoads{}
		case assignSetMatching:
			// We had not yet loaded this partition, so there is
			// nothing to set, and we keep everything.
		case assignInvalidateMatching:
			loadOffsets.keepFilter(func(t string, p int32) bool {
				if assignTopic, ok := assignments[t]; ok {
					if _, ok := assignTopic[p]; ok {
						return false
					}
				}
				return true
			})
		case assignPurgeMatching:
			// This is slightly different than invalidate in that
			// we invalidate whole topics.
			loadOffsets.keepFilter(func(t string, _ int32) bool {
				_, ok := assignments[t]
				return !ok // assignments are topics to purge -- do NOT keep the topic if it is being purged
			})
			// We have to purge from tps _after_ the session is
			// stopped. If we purge early while the session is
			// ongoing, then another goroutine could be loading and
			// using tps and expecting topics not yet removed from
			// assignPartitions to still be there. Specifically,
			// mapLoadsToBrokers could be expecting topic foo to be
			// there (from the session!), so if we purge foo before
			// stopping the session, we will panic.
			topics := make([]string, 0, len(assignments))
			for t := range assignments {
				topics = append(topics, t)
			}
			tps.purgeTopics(topics)
		}
	}

	// This assignment could contain nothing (for the purposes of
	// invalidating active fetches), so we only do this if needed.
	if len(assignments) == 0 || how != assignWithoutInvalidating {
		return
	}

	c.cl.cfg.logger.Log(LogLevelDebug, "assign requires loading offsets")

	topics := tps.load()
	for topic, partitions := range assignments {
		topicPartitions := topics.loadTopic(topic) // should be non-nil
		if topicPartitions == nil {
			c.cl.cfg.logger.Log(LogLevelError, "BUG! consumer was assigned topic that we did not ask for in ConsumeTopics nor ConsumePartitions, skipping!", "topic", topic)
			continue
		}

		for partition, offset := range partitions {
			// If we are loading the first record after a millisec,
			// we go directly to listing offsets. Epoch validation
			// does not ever set afterMilli.
			if offset.afterMilli {
				loadOffsets.addLoad(topic, partition, loadTypeList, offsetLoad{
					replica: -1,
					Offset:  offset,
				})
				continue
			}

			// First, if the request is exact, get rid of the relative
			// portion. We are modifying a copy of the offset, i.e. we
			// are appropriately not modfying 'assignments' itself.
			if offset.at >= 0 {
				offset.at += offset.relative
				if offset.at < 0 {
					offset.at = 0
				}
				offset.relative = 0
			}

			// If we are requesting an exact offset with an epoch,
			// we do truncation detection and then use the offset.
			//
			// Otherwise, an epoch is specified without an exact
			// request which is useless for us, or a request is
			// specified without a known epoch.
			//
			// The client ensures the epoch is non-negative from
			// fetch offsets only if the broker supports KIP-320,
			// but we do not override the user manually specifying
			// an epoch.
			if offset.at >= 0 && offset.epoch >= 0 {
				loadOffsets.addLoad(topic, partition, loadTypeEpoch, offsetLoad{
					replica: -1,
					Offset:  offset,
				})
				continue
			}

			// If an exact offset is specified and we have loaded
			// the partition, we use it. We have to use epoch -1
			// rather than the latest loaded epoch on the partition
			// because the offset being requested to use could be
			// from an epoch after OUR loaded epoch. Otherwise, we
			// could update the metadata, see the later epoch,
			// request the end offset for our prior epoch, and then
			// think data loss occurred.
			//
			// If an offset is unspecified or we have not loaded
			// the partition, we list offsets to find out what to
			// use.
			if offset.at >= 0 && partition >= 0 && partition < int32(len(topicPartitions.partitions)) {
				part := topicPartitions.partitions[partition]
				cursor := part.cursor
				cursor.setOffset(cursorOffset{
					offset:            offset.at,
					lastConsumedEpoch: -1,
				})
				cursor.allowUsable()
				c.usingCursors.use(cursor)
				continue
			}

			// If the offset is atCommitted, then no offset was
			// loaded from FetchOffsets. We inject an error and
			// avoid using this partition.
			if offset.at == atCommitted {
				c.addFakeReadyForDraining(topic, partition, errNoCommittedOffset, "notification of uncommitted partition")
				continue
			}

			loadOffsets.addLoad(topic, partition, loadTypeList, offsetLoad{
				replica: -1,
				Offset:  offset,
			})
		}
	}
}

// filterMetadataAllTopics, called BEFORE doOnMetadataUpdate, evaluates
// all topics received against the user provided regex.
func (c *consumer) filterMetadataAllTopics(topics []string) []string {
	c.mu.Lock()
	defer c.mu.Unlock()

	var rns reNews
	defer rns.log(&c.cl.cfg)

	var reSeen map[string]bool
	if c.d != nil {
		reSeen = c.d.reSeen
	} else {
		reSeen = c.g.reSeen
	}

	keep := topics[:0]
	for _, topic := range topics {
		want, seen := reSeen[topic]
		if !seen {
			for rawRe, re := range c.cl.cfg.topics {
				if want = re.MatchString(topic); want {
					rns.add(rawRe, topic)
					break
				}
			}
			if !want {
				rns.skip(topic)
			}
			reSeen[topic] = want
		}
		if want {
			keep = append(keep, topic)
		}
	}
	return keep
}

func (c *consumer) doOnMetadataUpdate() {
	if !c.consuming() {
		return
	}

	// See the comment on the outstandingMetadataUpdates field for why this
	// block below.
	if c.outstandingMetadataUpdates.maybeBegin() {
		doUpdate := func() {
			// We forbid reassignments while we do a quick check for
			// new assignments--for the direct consumer particularly,
			// this prevents TOCTOU, and guards against a concurrent
			// assignment from SetOffsets.
			c.mu.Lock()
			defer c.mu.Unlock()

			switch {
			case c.d != nil:
				if new := c.d.findNewAssignments(); len(new) > 0 {
					c.assignPartitions(new, assignWithoutInvalidating, c.d.tps, "new assignments from direct consumer")
				}
			case c.g != nil:
				c.g.findNewAssignments()
			}

			go c.loadSession().doOnMetadataUpdate()
		}

		go func() {
			again := true
			for again {
				doUpdate()
				again = c.outstandingMetadataUpdates.maybeFinish(false)
			}
		}()
	}
}

func (s *consumerSession) doOnMetadataUpdate() {
	if s == nil || s == noConsumerSession { // no session started yet
		return
	}

	s.listOrEpochMu.Lock()
	defer s.listOrEpochMu.Unlock()

	if s.listOrEpochMetaCh == nil {
		return // nothing waiting to load epochs / offsets
	}
	select {
	case s.listOrEpochMetaCh <- struct{}{}:
	default:
	}
}

type offsetLoadMap map[string]map[int32]offsetLoad

// offsetLoad is effectively an Offset, but also includes a potential replica
// to directly use if a cursor had a preferred replica.
type offsetLoad struct {
	replica int32 // -1 means leader
	Offset
}

func (o offsetLoad) MarshalJSON() ([]byte, error) {
	if o.replica == -1 {
		return o.Offset.MarshalJSON()
	}
	if o.relative == 0 {
		return []byte(fmt.Sprintf(`{"Replica":%d,"At":%d,"Epoch":%d,"CurrentEpoch":%d}`, o.replica, o.at, o.epoch, o.currentEpoch)), nil
	}
	return []byte(fmt.Sprintf(`{"Replica":%d,"At":%d,"Relative":%d,"Epoch":%d,"CurrentEpoch":%d}`, o.replica, o.at, o.relative, o.epoch, o.currentEpoch)), nil
}

func (o offsetLoadMap) errToLoaded(err error) []loadedOffset {
	var loaded []loadedOffset
	for t, ps := range o {
		for p, o := range ps {
			loaded = append(loaded, loadedOffset{
				topic:     t,
				partition: p,
				err:       err,
				request:   o,
			})
		}
	}
	return loaded
}

// Combines list and epoch loads into one type for simplicity.
type listOrEpochLoads struct {
	// List and Epoch are public so that anything marshaling through
	// reflect (i.e. json) can see the fields.
	List  offsetLoadMap
	Epoch offsetLoadMap
}

type listOrEpochLoadType uint8

const (
	loadTypeList listOrEpochLoadType = iota
	loadTypeEpoch
)

func (l listOrEpochLoadType) String() string {
	switch l {
	case loadTypeList:
		return "list"
	default:
		return "epoch"
	}
}

// adds an offset to be loaded, ensuring it exists only in the final loadType.
func (l *listOrEpochLoads) addLoad(t string, p int32, loadType listOrEpochLoadType, load offsetLoad) {
	l.removeLoad(t, p)
	dst := &l.List
	if loadType == loadTypeEpoch {
		dst = &l.Epoch
	}

	if *dst == nil {
		*dst = make(offsetLoadMap)
	}
	ps := (*dst)[t]
	if ps == nil {
		ps = make(map[int32]offsetLoad)
		(*dst)[t] = ps
	}
	ps[p] = load
}

func (l *listOrEpochLoads) removeLoad(t string, p int32) {
	for _, m := range []offsetLoadMap{
		l.List,
		l.Epoch,
	} {
		if m == nil {
			continue
		}
		ps := m[t]
		if ps == nil {
			continue
		}
		delete(ps, p)
		if len(ps) == 0 {
			delete(m, t)
		}
	}
}

func (l listOrEpochLoads) each(fn func(string, int32)) {
	for _, m := range []offsetLoadMap{
		l.List,
		l.Epoch,
	} {
		for topic, partitions := range m {
			for partition := range partitions {
				fn(topic, partition)
			}
		}
	}
}

func (l *listOrEpochLoads) keepFilter(keep func(string, int32) bool) {
	for _, m := range []offsetLoadMap{
		l.List,
		l.Epoch,
	} {
		for t, ps := range m {
			for p := range ps {
				if !keep(t, p) {
					delete(ps, p)
					if len(ps) == 0 {
						delete(m, t)
					}
				}
			}
		}
	}
}

// Merges loads into the caller; used to coalesce loads while a metadata update
// is happening (see the only use below).
func (l *listOrEpochLoads) mergeFrom(src listOrEpochLoads) {
	for _, srcs := range []struct {
		m        offsetLoadMap
		loadType listOrEpochLoadType
	}{
		{src.List, loadTypeList},
		{src.Epoch, loadTypeEpoch},
	} {
		for t, ps := range srcs.m {
			for p, load := range ps {
				l.addLoad(t, p, srcs.loadType, load)
			}
		}
	}
}

func (l listOrEpochLoads) isEmpty() bool { return len(l.List) == 0 && len(l.Epoch) == 0 }

func (l listOrEpochLoads) loadWithSession(s *consumerSession, why string) {
	if !l.isEmpty() {
		s.incWorker()
		go s.listOrEpoch(l, false, why)
	}
}

func (l listOrEpochLoads) loadWithSessionNow(s *consumerSession, why string) bool {
	if !l.isEmpty() {
		s.incWorker()
		go s.listOrEpoch(l, true, why)
		return true
	}
	return false
}

// A consumer session is responsible for an era of fetching records for a set
// of cursors. The set can be added to without killing an active session, but
// it cannot be removed from. Removing any cursor from being consumed kills the
// current consumer session and begins a new one.
type consumerSession struct {
	c *consumer

	ctx    context.Context
	cancel func()

	// tps tracks the topics that were assigned in this session. We use
	// this field to build and handle list offset / load epoch requests.
	tps *topicsPartitions

	// desireFetchCh is sized to the number of concurrent fetches we are
	// configured to be able to send.
	//
	// We receive desires from sources, we reply when they can fetch, and
	// they send back when they are done. Thus, three level chan.
	desireFetchCh       chan chan chan struct{}
	cancelFetchCh       chan chan chan struct{}
	allowedFetches      int
	fetchManagerStarted atomicBool // atomic, once true, we start the fetch manager

	// Workers signify the number of fetch and list / epoch goroutines that
	// are currently running within the context of this consumer session.
	// Stopping a session only returns once workers hits zero.
	workersMu   sync.Mutex
	workersCond *sync.Cond
	workers     int

	listOrEpochMu           sync.Mutex
	listOrEpochLoadsWaiting listOrEpochLoads
	listOrEpochMetaCh       chan struct{} // non-nil if Loads is non-nil, signalled on meta update
	listOrEpochLoadsLoading listOrEpochLoads
}

func (c *consumer) newConsumerSession(tps *topicsPartitions) *consumerSession {
	if tps == nil || len(tps.load()) == 0 {
		return noConsumerSession
	}
	ctx, cancel := context.WithCancel(c.cl.ctx)
	session := &consumerSession{
		c: c,

		ctx:    ctx,
		cancel: cancel,

		tps: tps,

		// NOTE: This channel must be unbuffered. If it is buffered,
		// then we can exit manageFetchConcurrency when we should not
		// and have a deadlock:
		//
		// * source sends to desireFetchCh, is buffered
		// * source seeds context canceled, tries sending to cancelFetchCh
		// * session concurrently sees context canceled
		// * session has not drained desireFetchCh, sees activeFetches is 0
		// * session exits
		// * source permanently hangs sending to desireFetchCh
		//
		// By having desireFetchCh unbuffered, we *ensure* that if the
		// source indicates it wants a fetch, the session knows it and
		// tracks it in wantFetch.
		//
		// See #198.
		desireFetchCh: make(chan chan chan struct{}),

		cancelFetchCh:  make(chan chan chan struct{}, 4),
		allowedFetches: c.cl.cfg.maxConcurrentFetches,
	}
	session.workersCond = sync.NewCond(&session.workersMu)
	return session
}

func (s *consumerSession) desireFetch() chan chan chan struct{} {
	if !s.fetchManagerStarted.Swap(true) {
		go s.manageFetchConcurrency()
	}
	return s.desireFetchCh
}

func (s *consumerSession) manageFetchConcurrency() {
	var (
		activeFetches int
		doneFetch     = make(chan struct{}, 20)
		wantFetch     []chan chan struct{}

		ctxCh    = s.ctx.Done()
		wantQuit bool
	)
	for {
		select {
		case register := <-s.desireFetchCh:
			wantFetch = append(wantFetch, register)
		case cancel := <-s.cancelFetchCh:
			var found bool
			for i, want := range wantFetch {
				if want == cancel {
					_ = append(wantFetch[i:], wantFetch[i+1:]...)
					wantFetch = wantFetch[:len(wantFetch)-1]
					found = true
				}
			}
			// If we did not find the channel, then we have already
			// sent to it, removed it from our wantFetch list, and
			// bumped activeFetches.
			if !found {
				activeFetches--
			}

		case <-doneFetch:
			activeFetches--
		case <-ctxCh:
			wantQuit = true
			ctxCh = nil
		}

		if len(wantFetch) > 0 && (activeFetches < s.allowedFetches || s.allowedFetches == 0) { // 0 means unbounded
			wantFetch[0] <- doneFetch
			wantFetch = wantFetch[1:]
			activeFetches++
			continue
		}

		if wantQuit && activeFetches == 0 {
			return
		}
	}
}

func (s *consumerSession) incWorker() {
	if s == noConsumerSession { // from startNewSession
		return
	}
	s.workersMu.Lock()
	defer s.workersMu.Unlock()
	s.workers++
}

func (s *consumerSession) decWorker() {
	if s == noConsumerSession { // from followup to startNewSession
		return
	}
	s.workersMu.Lock()
	defer s.workersMu.Unlock()
	s.workers--
	if s.workers == 0 {
		s.workersCond.Broadcast()
	}
}

// noConsumerSession exists because we cannot store nil into an atomic.Value.
var noConsumerSession = new(consumerSession)

func (c *consumer) loadSession() *consumerSession {
	if session := c.session.Load(); session != nil {
		return session.(*consumerSession)
	}
	return noConsumerSession
}

// Guards against a session being stopped, and must be paired with an unguard.
// This returns a new session if there was no session.
//
// The purpose of this function is when performing additive-only changes to an
// existing session, because additive-only changes can avoid killing a running
// session.
func (c *consumer) guardSessionChange(tps *topicsPartitions) *consumerSession {
	c.sessionChangeMu.Lock()

	session := c.loadSession()
	if session == noConsumerSession {
		// If there is no session, we simply store one. This is fine;
		// sources will be able to begin a fetch loop, but they will
		// have no cursors to consume yet.
		session = c.newConsumerSession(tps)
		c.session.Store(session)
	}

	return session
}

// For the same reason below as in startNewSession, we inc a worker before
// unguarding. This allows the unguarding to execute a bit of logic if
// necessary before the session can be stopped.
func (c *consumer) unguardSessionChange(session *consumerSession) {
	session.incWorker()
	c.sessionChangeMu.Unlock()
}

// Stops an active consumer session if there is one, and does not return until
// all fetching, listing, offset for leader epoching is complete. This
// invalidates any buffered fetches for the previous session and returns any
// partitions that were listing offsets or loading epochs.
func (c *consumer) stopSession() (listOrEpochLoads, *topicsPartitions) {
	c.sessionChangeMu.Lock()

	session := c.loadSession()

	if session == noConsumerSession {
		return listOrEpochLoads{}, nil // we had no session
	}

	// Before storing noConsumerSession, cancel our old. This pairs
	// with the reverse ordering in source, which checks noConsumerSession
	// then checks the session context.
	session.cancel()

	// At this point, any in progress fetches, offset lists, or epoch loads
	// will quickly die.

	c.session.Store(noConsumerSession)

	// At this point, no source can be started, because the session is
	// noConsumerSession.

	session.workersMu.Lock()
	for session.workers > 0 {
		session.workersCond.Wait()
	}
	session.workersMu.Unlock()

	// At this point, all fetches, lists, and loads are dead. We can close
	// our num-fetches manager without worrying about a source trying to
	// register itself.

	c.cl.allSinksAndSources(func(sns sinkAndSource) {
		sns.source.session.reset()
	})

	// At this point, if we begin fetching anew, then the sources will not
	// be using stale fetch sessions.

	c.sourcesReadyMu.Lock()
	defer c.sourcesReadyMu.Unlock()
	for _, ready := range c.sourcesReadyForDraining {
		ready.discardBuffered()
	}
	c.sourcesReadyForDraining = nil

	// At this point, we have invalidated any buffered data from the prior
	// session. We leave any fake things that were ready so that the user
	// can act on errors. The session is dead.

	session.listOrEpochLoadsWaiting.mergeFrom(session.listOrEpochLoadsLoading)
	return session.listOrEpochLoadsWaiting, session.tps
}

// Starts a new consumer session, allowing fetches to happen.
//
// If there are no topic partitions to start with, this returns noConsumerSession.
//
// This is returned with 1 worker; decWorker must be called after return. The
// 1 worker allows for initialization work to prevent the session from being
// immediately stopped.
func (c *consumer) startNewSession(tps *topicsPartitions) *consumerSession {
	if c.kill.Load() {
		tps = nil
	}
	session := c.newConsumerSession(tps)
	c.session.Store(session)

	// Ensure that this session is usable before being stopped immediately.
	// The caller must dec workers.
	session.incWorker()

	// At this point, sources can start consuming.

	c.sessionChangeMu.Unlock()

	c.cl.allSinksAndSources(func(sns sinkAndSource) {
		sns.source.maybeConsume()
	})

	// At this point, any source that was not consuming becauase it saw the
	// session was stopped has been notified to potentially start consuming
	// again. The session is alive.

	return session
}

// This function is responsible for issuing ListOffsets or
// OffsetForLeaderEpoch. These requests's responses  are only handled within
// the context of a consumer session.
func (s *consumerSession) listOrEpoch(waiting listOrEpochLoads, immediate bool, why string) {
	defer s.decWorker()

	// It is possible for a metadata update to try to migrate partition
	// loads if the update moves partitions between brokers. If we are
	// closing the client, the consumer session could already be stopped,
	// but this stops before the metadata goroutine is killed. So, if we
	// are in this function but actually have no session, we return.
	if s == noConsumerSession {
		return
	}

	wait := true
	if immediate {
		s.c.cl.triggerUpdateMetadataNow(why)
	} else {
		wait = s.c.cl.triggerUpdateMetadata(false, why) // avoid trigger if within refresh interval
	}

	s.listOrEpochMu.Lock() // collapse any listOrEpochs that occur during meta update into one
	if !s.listOrEpochLoadsWaiting.isEmpty() {
		s.listOrEpochLoadsWaiting.mergeFrom(waiting)
		s.listOrEpochMu.Unlock()
		return
	}
	s.listOrEpochLoadsWaiting = waiting
	s.listOrEpochMetaCh = make(chan struct{}, 1)
	s.listOrEpochMu.Unlock()

	if wait {
		select {
		case <-s.ctx.Done():
			return
		case <-s.listOrEpochMetaCh:
		}
	}

	s.listOrEpochMu.Lock()
	loading := s.listOrEpochLoadsWaiting
	s.listOrEpochLoadsLoading.mergeFrom(loading)
	s.listOrEpochLoadsWaiting = listOrEpochLoads{}
	s.listOrEpochMetaCh = nil
	s.listOrEpochMu.Unlock()

	brokerLoads := s.mapLoadsToBrokers(loading)

	results := make(chan loadedOffsets, 2*len(brokerLoads)) // each broker can receive up to two requests

	var issued, received int
	for broker, brokerLoad := range brokerLoads {
		s.c.cl.cfg.logger.Log(LogLevelDebug, "offsets to load broker", "broker", broker.meta.NodeID, "load", brokerLoad)
		if len(brokerLoad.List) > 0 {
			issued++
			go s.c.cl.listOffsetsForBrokerLoad(s.ctx, broker, brokerLoad.List, s.tps, results)
		}
		if len(brokerLoad.Epoch) > 0 {
			issued++
			go s.c.cl.loadEpochsForBrokerLoad(s.ctx, broker, brokerLoad.Epoch, s.tps, results)
		}
	}

	var reloads listOrEpochLoads
	defer func() {
		if !reloads.isEmpty() {
			s.incWorker()
			go func() {
				// Before we dec our worker, we must add the
				// reloads back into the session's waiting loads.
				// Doing so allows a concurrent stopSession to
				// track the waiting loads, whereas if we did not
				// add things back to the session, we could abandon
				// loading these offsets and have a stuck cursor.
				defer s.decWorker()
				defer reloads.loadWithSession(s, "reload offsets from load failure")
				after := time.NewTimer(time.Second)
				defer after.Stop()
				select {
				case <-after.C:
				case <-s.ctx.Done():
					return
				}
			}()
		}
	}()

	for received != issued {
		select {
		case <-s.ctx.Done():
			// If we return early, our session was canceled. We do
			// not move loading list or epoch loads back to
			// waiting; the session stopping manages that.
			return
		case loaded := <-results:
			received++
			reloads.mergeFrom(s.handleListOrEpochResults(loaded))
		}
	}
}

// Called within a consumer session, this function handles results from list
// offsets or epoch loads and returns any loads that should be retried.
//
// To us, all errors are reloadable. We either have request level retryable
// errors (unknown partition, etc) or non-retryable errors (auth), or we have
// request issuing errors (no dial, connection cut repeatedly).
//
// For retryable request errors, we may as well back off a little bit to allow
// Kafka to harmonize if the topic exists / etc.
//
// For non-retryable request errors, we may as well retry to both (a) allow the
// user more signals about a problem that they can maybe fix within Kafka (i.e.
// the auth), and (b) force the user to notice errors.
//
// For request issuing errors, we may as well continue to retry because there
// is not much else we can do. RequestWith already retries, but returns when
// the retry limit is hit. We will backoff 1s and then allow RequestWith to
// continue requesting and backing off.
func (s *consumerSession) handleListOrEpochResults(loaded loadedOffsets) (reloads listOrEpochLoads) {
	// This function can be running twice concurrently, so we need to guard
	// listOrEpochLoadsLoading and usingCursors. For simplicity, we just
	// guard this entire function.

	debug := s.c.cl.cfg.logger.Level() >= LogLevelDebug

	var using map[string]map[int32]EpochOffset
	type epochOffsetWhy struct {
		EpochOffset
		error
	}
	var reloading map[string]map[int32]epochOffsetWhy
	if debug {
		using = make(map[string]map[int32]EpochOffset)
		reloading = make(map[string]map[int32]epochOffsetWhy)
		defer func() {
			t := "list"
			if loaded.loadType == loadTypeEpoch {
				t = "epoch"
			}
			s.c.cl.cfg.logger.Log(LogLevelDebug, fmt.Sprintf("handled %s results", t), "broker", logID(loaded.broker), "using", using, "reloading", reloading)
		}()
	}

	s.listOrEpochMu.Lock()
	defer s.listOrEpochMu.Unlock()

	for _, load := range loaded.loaded {
		s.listOrEpochLoadsLoading.removeLoad(load.topic, load.partition) // remove the tracking of this load from our session

		use := func() {
			if debug {
				tusing := using[load.topic]
				if tusing == nil {
					tusing = make(map[int32]EpochOffset)
					using[load.topic] = tusing
				}
				tusing[load.partition] = EpochOffset{load.leaderEpoch, load.offset}
			}

			load.cursor.setOffset(cursorOffset{
				offset:            load.offset,
				lastConsumedEpoch: load.leaderEpoch,
			})
			load.cursor.allowUsable()
			s.c.usingCursors.use(load.cursor)
		}

		var edl *ErrDataLoss
		switch {
		case errors.As(load.err, &edl):
			s.c.addFakeReadyForDraining(load.topic, load.partition, load.err, "notification of data loss") // signal we lost data, but set the cursor to what we can
			use()

		case load.err == nil:
			use()

		default: // from ErrorCode in a response, or broker request err, or request is canceled as our session is ending
			reloads.addLoad(load.topic, load.partition, loaded.loadType, load.request)
			if !kerr.IsRetriable(load.err) && !isRetryableBrokerErr(load.err) && !isDialNonTimeoutErr(load.err) && !isContextErr(load.err) { // non-retryable response error; signal such in a response
				s.c.addFakeReadyForDraining(load.topic, load.partition, load.err, fmt.Sprintf("notification of non-retryable error from %s request", loaded.loadType))
			}

			if debug {
				treloading := reloading[load.topic]
				if treloading == nil {
					treloading = make(map[int32]epochOffsetWhy)
					reloading[load.topic] = treloading
				}
				treloading[load.partition] = epochOffsetWhy{EpochOffset{load.leaderEpoch, load.offset}, load.err}
			}
		}
	}

	return reloads
}

// Splits the loads into per-broker loads, mapping each partition to the broker
// that leads that partition.
func (s *consumerSession) mapLoadsToBrokers(loads listOrEpochLoads) map[*broker]listOrEpochLoads {
	brokerLoads := make(map[*broker]listOrEpochLoads)

	s.c.cl.brokersMu.RLock() // hold mu so we can check if partition leaders exist
	defer s.c.cl.brokersMu.RUnlock()

	brokers := s.c.cl.brokers
	seed := s.c.cl.loadSeeds()[0]

	topics := s.tps.load()
	for _, loads := range []struct {
		m        offsetLoadMap
		loadType listOrEpochLoadType
	}{
		{loads.List, loadTypeList},
		{loads.Epoch, loadTypeEpoch},
	} {
		for topic, partitions := range loads.m {
			topicPartitions := topics.loadTopic(topic) // this must exist, it not existing would be a bug
			for partition, offset := range partitions {
				// We default to the first seed broker if we have no loaded
				// the broker leader for this partition (we should have).
				// Worst case, we get an error for the partition and retry.
				broker := seed
				if partition >= 0 && partition < int32(len(topicPartitions.partitions)) {
					topicPartition := topicPartitions.partitions[partition]
					brokerID := topicPartition.leader
					if offset.replica != -1 {
						// If we are fetching from a follower, we can list
						// offsets against the follower itself. The replica
						// being non-negative signals that.
						brokerID = offset.replica
					}
					if tryBroker := findBroker(brokers, brokerID); tryBroker != nil {
						broker = tryBroker
					}
					offset.currentEpoch = topicPartition.leaderEpoch // ensure we set our latest epoch for the partition
				}

				brokerLoad := brokerLoads[broker]
				brokerLoad.addLoad(topic, partition, loads.loadType, offset)
				brokerLoads[broker] = brokerLoad
			}
		}
	}

	return brokerLoads
}

// The result of ListOffsets or OffsetForLeaderEpoch for an individual
// partition.
type loadedOffset struct {
	topic     string
	partition int32

	// The following three are potentially unset if the error is non-nil
	// and not ErrDataLoss; these are what we loaded.
	cursor      *cursor
	offset      int64
	leaderEpoch int32

	// Any error encountered for loading this partition, or for epoch
	// loading, potentially ErrDataLoss. If this error is not retryable, we
	// avoid reloading the offset and instead inject a fake partition for
	// PollFetches containing this error.
	err error

	// The original request.
	request offsetLoad
}

// The results of ListOffsets or OffsetForLeaderEpoch for an individual broker.
type loadedOffsets struct {
	broker   int32
	loaded   []loadedOffset
	loadType listOrEpochLoadType
}

func (l *loadedOffsets) add(a loadedOffset) { l.loaded = append(l.loaded, a) }
func (l *loadedOffsets) addAll(as []loadedOffset) loadedOffsets {
	l.loaded = append(l.loaded, as...)
	return *l
}

func (cl *Client) listOffsetsForBrokerLoad(ctx context.Context, broker *broker, load offsetLoadMap, tps *topicsPartitions, results chan<- loadedOffsets) {
	loaded := loadedOffsets{broker: broker.meta.NodeID, loadType: loadTypeList}

	req1, req2 := load.buildListReq(cl.cfg.isolationLevel)
	var (
		wg     sync.WaitGroup
		kresp2 kmsg.Response
		err2   error
	)
	if req2 != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			kresp2, err2 = broker.waitResp(ctx, req2)
		}()
	}
	kresp, err := broker.waitResp(ctx, req1)
	wg.Wait()
	if err != nil || err2 != nil {
		results <- loaded.addAll(load.errToLoaded(err))
		return
	}

	topics := tps.load()
	resp := kresp.(*kmsg.ListOffsetsResponse)

	// If we issued a second req to check that an exact offset is in
	// bounds, then regrettably for safety, we have to ensure that the
	// shapes of both responses match, and the topic & partition at each
	// index matches. Anything that does not match is skipped (and would be
	// a bug from Kafka), and we at the end return UnknownTopicOrPartition.
	var resp2 *kmsg.ListOffsetsResponse
	if req2 != nil {
		resp2 = kresp2.(*kmsg.ListOffsetsResponse)
		for _, r := range []*kmsg.ListOffsetsResponse{
			resp,
			resp2,
		} {
			ts := r.Topics
			sort.Slice(ts, func(i, j int) bool {
				return ts[i].Topic < ts[j].Topic
			})
			for i := range ts {
				ps := ts[i].Partitions
				sort.Slice(ps, func(i, j int) bool {
					return ps[i].Partition < ps[j].Partition
				})
			}
		}

		lt := resp.Topics
		rt := resp2.Topics
		lkeept := lt[:0]
		rkeept := rt[:0]
		// Over each response, we only keep the topic if the topics match.
		for len(lt) > 0 && len(rt) > 0 {
			if lt[0].Topic < rt[0].Topic {
				lt = lt[1:]
				continue
			}
			if rt[0].Topic < lt[0].Topic {
				rt = rt[1:]
				continue
			}
			// As well, for topics that match, we only keep
			// partitions that match. In this case, we also want
			// both partitions to be error free, otherwise we keep
			// an error on both. If one has old style offsets,
			// both must.
			lp := lt[0].Partitions
			rp := rt[0].Partitions
			lkeepp := lp[:0]
			rkeepp := rp[:0]
			for len(lp) > 0 && len(rp) > 0 {
				if lp[0].Partition < rp[0].Partition {
					lp = lp[1:]
					continue
				}
				if rp[0].Partition < lp[0].Partition {
					rp = rp[1:]
					continue
				}
				if len(lp[0].OldStyleOffsets) > 0 && len(rp[0].OldStyleOffsets) == 0 ||
					len(lp[0].OldStyleOffsets) == 0 && len(rp[0].OldStyleOffsets) > 0 {
					lp = lp[1:]
					rp = rp[1:]
					continue
				}
				if lp[0].ErrorCode != 0 {
					rp[0].ErrorCode = lp[0].ErrorCode
				} else if rp[0].ErrorCode != 0 {
					lp[0].ErrorCode = rp[0].ErrorCode
				}
				lkeepp = append(lkeepp, lp[0])
				rkeepp = append(rkeepp, rp[0])
				lp = lp[1:]
				rp = rp[1:]
			}
			// Now we update the partitions in the topic we are
			// keeping, and keep our topic.
			lt[0].Partitions = lkeepp
			rt[0].Partitions = rkeepp
			lkeept = append(lkeept, lt[0])
			rkeept = append(rkeept, rt[0])
			lt = lt[1:]
			rt = rt[1:]
		}
		// Finally, update each response with the topics we kept. The
		// shapes and indices are the same.
		resp.Topics = lkeept
		resp2.Topics = rkeept
	}

	poffset := func(p *kmsg.ListOffsetsResponseTopicPartition) int64 {
		offset := p.Offset
		if len(p.OldStyleOffsets) > 0 {
			offset = p.OldStyleOffsets[0] // list offsets v0
		}
		return offset
	}

	for i, rTopic := range resp.Topics {
		topic := rTopic.Topic
		loadParts, ok := load[topic]
		if !ok {
			continue // should not happen: kafka replied with something we did not ask for
		}

		topicPartitions := topics.loadTopic(topic) // must be non-nil at this point
		for j, rPartition := range rTopic.Partitions {
			partition := rPartition.Partition
			loadPart, ok := loadParts[partition]
			if !ok {
				continue // should not happen: kafka replied with something we did not ask for
			}

			if err := kerr.ErrorForCode(rPartition.ErrorCode); err != nil {
				loaded.add(loadedOffset{
					topic:     topic,
					partition: partition,
					err:       err,
					request:   loadPart,
				})
				continue // partition err: handled in results
			}

			if partition < 0 || partition >= int32(len(topicPartitions.partitions)) {
				continue // should not happen: we have not seen this partition from a metadata response
			}
			topicPartition := topicPartitions.partitions[partition]

			delete(loadParts, partition)
			if len(loadParts) == 0 {
				delete(load, topic)
			}

			offset := poffset(&rPartition)
			end := func() int64 { return poffset(&resp2.Topics[i].Partitions[j]) }

			// We ensured the resp2 shape is as we want and has no
			// error, so resp2 lookups are safe.
			if loadPart.afterMilli {
				// If after a milli, if the milli is after the
				// end of a partition, the offset is -1. We use
				// our end offset request: anything after the
				// end offset *now* is after our milli.
				if offset == -1 {
					offset = end()
				}
			} else if loadPart.at >= 0 {
				// If an exact offset, we listed start and end.
				// We validate the offset is within bounds.
				end := end()
				want := loadPart.at + loadPart.relative
				if want >= offset {
					offset = want
				}
				if want >= end {
					offset = end
				}
			} else if loadPart.at == -2 && loadPart.relative > 0 {
				// Relative to the start: both start & end were
				// issued, and we bound to the end.
				offset += loadPart.relative
				if end := end(); offset >= end {
					offset = end
				}
			} else if loadPart.at == -1 && loadPart.relative < 0 {
				// Relative to the end: both start & end were
				// issued, offset is currently the start, so we
				// set to the end and then bound to the start.
				start := offset
				offset = end()
				offset += loadPart.relative
				if offset <= start {
					offset = start
				}
			}
			if offset < 0 {
				offset = 0 // sanity
			}

			loaded.add(loadedOffset{
				topic:       topic,
				partition:   partition,
				cursor:      topicPartition.cursor,
				offset:      offset,
				leaderEpoch: rPartition.LeaderEpoch,
				request:     loadPart,
			})
		}
	}

	results <- loaded.addAll(load.errToLoaded(kerr.UnknownTopicOrPartition))
}

func (*Client) loadEpochsForBrokerLoad(ctx context.Context, broker *broker, load offsetLoadMap, tps *topicsPartitions, results chan<- loadedOffsets) {
	loaded := loadedOffsets{broker: broker.meta.NodeID, loadType: loadTypeEpoch}

	kresp, err := broker.waitResp(ctx, load.buildEpochReq())
	if err != nil {
		results <- loaded.addAll(load.errToLoaded(err))
		return
	}

	// If the version is < 2, we are speaking to an old broker. We should
	// not have an old version, but we could have spoken to a new broker
	// first then an old broker in the middle of a broker roll. For now, we
	// will just loop retrying until the broker is upgraded.

	topics := tps.load()
	resp := kresp.(*kmsg.OffsetForLeaderEpochResponse)
	for _, rTopic := range resp.Topics {
		topic := rTopic.Topic
		loadParts, ok := load[topic]
		if !ok {
			continue // should not happen: kafka replied with something we did not ask for
		}

		topicPartitions := topics.loadTopic(topic) // must be non-nil at this point
		for _, rPartition := range rTopic.Partitions {
			partition := rPartition.Partition
			loadPart, ok := loadParts[partition]
			if !ok {
				continue // should not happen: kafka replied with something we did not ask for
			}

			if err := kerr.ErrorForCode(rPartition.ErrorCode); err != nil {
				loaded.add(loadedOffset{
					topic:     topic,
					partition: partition,
					err:       err,
					request:   loadPart,
				})
				continue // partition err: handled in results
			}

			if partition < 0 || partition >= int32(len(topicPartitions.partitions)) {
				continue // should not happen: we have not seen this partition from a metadata response
			}
			topicPartition := topicPartitions.partitions[partition]

			delete(loadParts, partition)
			if len(loadParts) == 0 {
				delete(load, topic)
			}

			// Epoch loading never uses noReset nor afterMilli;
			// this at is the offset we wanted to consume and are
			// validating.
			offset := loadPart.at
			var err error
			if rPartition.EndOffset < offset {
				err = &ErrDataLoss{topic, partition, offset, rPartition.EndOffset}
				offset = rPartition.EndOffset
			}

			loaded.add(loadedOffset{
				topic:       topic,
				partition:   partition,
				cursor:      topicPartition.cursor,
				offset:      offset,
				leaderEpoch: rPartition.LeaderEpoch,
				err:         err,
				request:     loadPart,
			})
		}
	}

	results <- loaded.addAll(load.errToLoaded(kerr.UnknownTopicOrPartition))
}

// In general this returns one request, but if the user is using exact offsets
// rather than start/end, then we issue both the start and end requests to
// ensure the user's requested offset is within bounds.
func (o offsetLoadMap) buildListReq(isolationLevel int8) (r1, r2 *kmsg.ListOffsetsRequest) {
	r1 = kmsg.NewPtrListOffsetsRequest()
	r1.ReplicaID = -1
	r1.IsolationLevel = isolationLevel
	r1.Topics = make([]kmsg.ListOffsetsRequestTopic, 0, len(o))
	var createEnd bool
	for topic, partitions := range o {
		parts := make([]kmsg.ListOffsetsRequestTopicPartition, 0, len(partitions))
		for partition, offset := range partitions {
			// If this is a milli request, we issue two lists: if
			// our milli is after the end of a partition, we get no
			// offset back and we want to know the start offset
			// (since it will be after our milli).
			//
			// If we are using an exact offset request, we issue
			// the start and end so that we can bound the exact
			// offset to being within that range.
			//
			// If we are using a relative offset, we potentially
			// issue the end request because relative may shift us
			// too far in the other direction.
			timestamp := offset.at
			if offset.afterMilli {
				createEnd = true
			} else if timestamp >= 0 || timestamp == -2 && offset.relative > 0 || timestamp == -1 && offset.relative < 0 {
				timestamp = -2
				createEnd = true
			}
			p := kmsg.NewListOffsetsRequestTopicPartition()
			p.Partition = partition
			p.CurrentLeaderEpoch = offset.currentEpoch // KIP-320
			p.Timestamp = timestamp
			p.MaxNumOffsets = 1

			parts = append(parts, p)
		}
		t := kmsg.NewListOffsetsRequestTopic()
		t.Topic = topic
		t.Partitions = parts
		r1.Topics = append(r1.Topics, t)
	}

	if createEnd {
		r2 = kmsg.NewPtrListOffsetsRequest()
		*r2 = *r1
		r2.Topics = append([]kmsg.ListOffsetsRequestTopic(nil), r1.Topics...)
		for i := range r1.Topics {
			l := &r2.Topics[i]
			r := &r1.Topics[i]
			*l = *r
			l.Partitions = append([]kmsg.ListOffsetsRequestTopicPartition(nil), r.Partitions...)
			for i := range l.Partitions {
				l.Partitions[i].Timestamp = -1
			}
		}
	}

	return r1, r2
}

func (o offsetLoadMap) buildEpochReq() *kmsg.OffsetForLeaderEpochRequest {
	req := kmsg.NewPtrOffsetForLeaderEpochRequest()
	req.ReplicaID = -1
	req.Topics = make([]kmsg.OffsetForLeaderEpochRequestTopic, 0, len(o))
	for topic, partitions := range o {
		parts := make([]kmsg.OffsetForLeaderEpochRequestTopicPartition, 0, len(partitions))
		for partition, offset := range partitions {
			p := kmsg.NewOffsetForLeaderEpochRequestTopicPartition()
			p.Partition = partition
			p.CurrentLeaderEpoch = offset.currentEpoch
			p.LeaderEpoch = offset.epoch
			parts = append(parts, p)
		}
		t := kmsg.NewOffsetForLeaderEpochRequestTopic()
		t.Topic = topic
		t.Partitions = parts
		req.Topics = append(req.Topics, t)
	}
	return req
}
