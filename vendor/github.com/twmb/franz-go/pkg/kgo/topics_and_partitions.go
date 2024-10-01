package kgo

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
)

/////////////
// HELPERS // -- ugly types to eliminate the toil of nil maps and lookups
/////////////

func dupmsi32(m map[string]int32) map[string]int32 {
	d := make(map[string]int32, len(m))
	for t, ps := range m {
		d[t] = ps
	}
	return d
}

// "Atomic map of topic partitions", for lack of a better name at this point.
type amtps struct {
	v atomic.Value
}

func (a *amtps) read() map[string][]int32 {
	v := a.v.Load()
	if v == nil {
		return nil
	}
	return v.(map[string][]int32)
}

func (a *amtps) write(fn func(map[string][]int32)) {
	dup := a.clone()
	fn(dup)
	a.store(dup)
}

func (a *amtps) clone() map[string][]int32 {
	orig := a.read()
	dup := make(map[string][]int32, len(orig))
	for t, ps := range orig {
		dup[t] = append(dup[t], ps...)
	}
	return dup
}

func (a *amtps) store(m map[string][]int32) { a.v.Store(m) }

type mtps map[string][]int32

func (m mtps) String() string {
	var sb strings.Builder
	var topicsWritten int
	ts := make([]string, 0, len(m))
	var ps []int32
	for t := range m {
		ts = append(ts, t)
	}
	sort.Strings(ts)
	for _, t := range ts {
		ps = append(ps[:0], m[t]...)
		sort.Slice(ps, func(i, j int) bool { return ps[i] < ps[j] })
		topicsWritten++
		fmt.Fprintf(&sb, "%s%v", t, ps)
		if topicsWritten < len(m) {
			sb.WriteString(", ")
		}
	}
	return sb.String()
}

type mtmps map[string]map[int32]struct{} // map of topics to map of partitions

func (m *mtmps) add(t string, p int32) {
	if *m == nil {
		*m = make(mtmps)
	}
	mps := (*m)[t]
	if mps == nil {
		mps = make(map[int32]struct{})
		(*m)[t] = mps
	}
	mps[p] = struct{}{}
}

func (m *mtmps) addt(t string) {
	if *m == nil {
		*m = make(mtmps)
	}
	mps := (*m)[t]
	if mps == nil {
		mps = make(map[int32]struct{})
		(*m)[t] = mps
	}
}

func (m mtmps) onlyt(t string) bool {
	if m == nil {
		return false
	}
	ps, exists := m[t]
	return exists && len(ps) == 0
}

func (m mtmps) remove(t string, p int32) {
	if m == nil {
		return
	}
	mps, exists := m[t]
	if !exists {
		return
	}
	delete(mps, p)
	if len(mps) == 0 {
		delete(m, t)
	}
}

////////////
// PAUSED // -- types for pausing topics and partitions
////////////

type pausedTopics map[string]pausedPartitions

type pausedPartitions struct {
	all bool
	m   map[int32]struct{}
}

func (m pausedTopics) t(topic string) (pausedPartitions, bool) {
	if len(m) == 0 { // potentially nil
		return pausedPartitions{}, false
	}
	pps, exists := m[topic]
	return pps, exists
}

func (m pausedTopics) has(topic string, partition int32) (paused bool) {
	if len(m) == 0 {
		return false
	}
	pps, exists := m[topic]
	if !exists {
		return false
	}
	if pps.all {
		return true
	}
	_, exists = pps.m[partition]
	return exists
}

func (m pausedTopics) addTopics(topics ...string) {
	for _, topic := range topics {
		pps, exists := m[topic]
		if !exists {
			pps = pausedPartitions{m: make(map[int32]struct{})}
		}
		pps.all = true
		m[topic] = pps
	}
}

func (m pausedTopics) delTopics(topics ...string) {
	for _, topic := range topics {
		pps, exists := m[topic]
		if !exists {
			continue
		}
		pps.all = false
		if !pps.all && len(pps.m) == 0 {
			delete(m, topic)
		}
	}
}

func (m pausedTopics) addPartitions(topicPartitions map[string][]int32) {
	for topic, partitions := range topicPartitions {
		pps, exists := m[topic]
		if !exists {
			pps = pausedPartitions{m: make(map[int32]struct{})}
		}
		for _, partition := range partitions {
			pps.m[partition] = struct{}{}
		}
		m[topic] = pps
	}
}

func (m pausedTopics) delPartitions(topicPartitions map[string][]int32) {
	for topic, partitions := range topicPartitions {
		pps, exists := m[topic]
		if !exists {
			continue
		}
		for _, partition := range partitions {
			delete(pps.m, partition)
		}
		if !pps.all && len(pps.m) == 0 {
			delete(m, topic)
		}
	}
}

func (m pausedTopics) pausedTopics() []string {
	var r []string
	for topic, pps := range m {
		if pps.all {
			r = append(r, topic)
		}
	}
	return r
}

func (m pausedTopics) pausedPartitions() map[string][]int32 {
	r := make(map[string][]int32)
	for topic, pps := range m {
		ps := make([]int32, 0, len(pps.m))
		for partition := range pps.m {
			ps = append(ps, partition)
		}
		r[topic] = ps
	}
	return r
}

func (m pausedTopics) clone() pausedTopics {
	dup := make(pausedTopics)
	dup.addTopics(m.pausedTopics()...)
	dup.addPartitions(m.pausedPartitions())
	return dup
}

//////////
// GUTS // -- the key types for storing important metadata for topics & partitions
//////////

func newTopicPartitions() *topicPartitions {
	parts := new(topicPartitions)
	parts.v.Store(new(topicPartitionsData))
	return parts
}

// Contains all information about a topic's partitions.
type topicPartitions struct {
	v atomic.Value // *topicPartitionsData

	partsMu     sync.Mutex
	partitioner TopicPartitioner
	lb          *leastBackupInput // for partitioning if the partitioner is a LoadTopicPartitioner
}

func (t *topicPartitions) load() *topicPartitionsData { return t.v.Load().(*topicPartitionsData) }

func newTopicsPartitions() *topicsPartitions {
	var t topicsPartitions
	t.v.Store(make(topicsPartitionsData))
	return &t
}

// A helper type mapping topics to their partitions;
// this is the inner value of topicPartitions.v.
type topicsPartitionsData map[string]*topicPartitions

func (d topicsPartitionsData) hasTopic(t string) bool { _, exists := d[t]; return exists }
func (d topicsPartitionsData) loadTopic(t string) *topicPartitionsData {
	tp, exists := d[t]
	if !exists {
		return nil
	}
	return tp.load()
}

// A helper type mapping topics to their partitions that can be updated
// atomically.
type topicsPartitions struct {
	v atomic.Value // topicsPartitionsData (map[string]*topicPartitions)
}

func (t *topicsPartitions) load() topicsPartitionsData {
	if t == nil {
		return nil
	}
	return t.v.Load().(topicsPartitionsData)
}
func (t *topicsPartitions) storeData(d topicsPartitionsData) { t.v.Store(d) }
func (t *topicsPartitions) storeTopics(topics []string)      { t.v.Store(t.ensureTopics(topics)) }
func (t *topicsPartitions) clone() topicsPartitionsData {
	current := t.load()
	clone := make(map[string]*topicPartitions, len(current))
	for k, v := range current {
		clone[k] = v
	}
	return clone
}

// Ensures that the topics exist in the returned map, but does not store the
// update. This can be used to update the data and store later, rather than
// storing immediately.
func (t *topicsPartitions) ensureTopics(topics []string) topicsPartitionsData {
	var cloned bool
	current := t.load()
	for _, topic := range topics {
		if _, exists := current[topic]; !exists {
			if !cloned {
				current = t.clone()
				cloned = true
			}
			current[topic] = newTopicPartitions()
		}
	}
	return current
}

// Opposite of ensureTopics, this purges the input topics and *does* store.
func (t *topicsPartitions) purgeTopics(topics []string) {
	var cloned bool
	current := t.load()
	for _, topic := range topics {
		if _, exists := current[topic]; exists {
			if !cloned {
				current = t.clone()
				cloned = true
			}
			delete(current, topic)
		}
	}
	if cloned {
		t.storeData(current)
	}
}

// Updates the topic partitions data atomic value.
//
// If this is the first time seeing partitions, we do processing of unknown
// partitions that may be buffered for producing.
func (cl *Client) storePartitionsUpdate(topic string, l *topicPartitions, lv *topicPartitionsData, hadPartitions bool) {
	// If the topic already had partitions, then there would be no
	// unknown topic waiting and we do not need to notify anything.
	if hadPartitions {
		l.v.Store(lv)
		return
	}

	p := &cl.producer

	p.unknownTopicsMu.Lock()
	defer p.unknownTopicsMu.Unlock()

	// If the topic did not have partitions, then we need to store the
	// partition update BEFORE unlocking the mutex to guard against this
	// sequence of events:
	//
	//   - unlock waiters
	//   - delete waiter
	//   - new produce recreates waiter
	//   - we store update
	//   - we never notify the recreated waiter
	//
	// By storing before releasing the locks, we ensure that later
	// partition loads for this topic under the mu will see our update.
	defer l.v.Store(lv)

	// If there are no unknown topics or this topic is not unknown, then we
	// have nothing to do.
	if len(p.unknownTopics) == 0 {
		return
	}
	unknown, exists := p.unknownTopics[topic]
	if !exists {
		return
	}

	// If we loaded no partitions because of a retryable error, we signal
	// the waiting goroutine that a try happened. It is possible the
	// goroutine is quitting and will not be draining unknownWait, so we do
	// not require the send.
	if len(lv.partitions) == 0 && kerr.IsRetriable(lv.loadErr) {
		select {
		case unknown.wait <- lv.loadErr:
		default:
		}
		return
	}

	// Either we have a fatal error or we can successfully partition.
	//
	// Even with a fatal error, if we loaded any partitions, we partition.
	// If we only had a fatal error, we can finish promises in a goroutine.
	// If we are partitioning, we have to do it under the unknownMu to
	// ensure prior buffered records are produced in order before we
	// release the mu.
	delete(p.unknownTopics, topic)
	close(unknown.wait) // allow waiting goroutine to quit

	if len(lv.partitions) == 0 {
		cl.producer.promiseBatch(batchPromise{
			recs: unknown.buffered,
			err:  lv.loadErr,
		})
	} else {
		for _, pr := range unknown.buffered {
			cl.doPartitionRecord(l, lv, pr)
		}
	}
}

// If a metadata request fails after retrying (internally retrying, so only a
// few times), or the metadata request does not return topics that we requested
// (which may also happen additionally consuming via regex), then we need to
// bump errors for topics that were previously loaded, and bump errors for
// topics awaiting load.
//
// This has two modes of operation:
//
//  1. if no topics were missing, then the metadata request failed outright,
//     and we need to bump errors on all stored topics and unknown topics.
//
//  2. if topics were missing, then the metadata request was successful but
//     had missing data, and we need to bump errors on only what was mising.
func (cl *Client) bumpMetadataFailForTopics(requested map[string]*topicPartitions, err error, missingTopics ...string) {
	p := &cl.producer

	// mode 1
	if len(missingTopics) == 0 {
		for _, topic := range requested {
			for _, topicPartition := range topic.load().partitions {
				topicPartition.records.bumpRepeatedLoadErr(err)
			}
		}
	}

	// mode 2
	var missing map[string]bool
	for _, failTopic := range missingTopics {
		if missing == nil {
			missing = make(map[string]bool, len(missingTopics))
		}
		missing[failTopic] = true

		if topic, exists := requested[failTopic]; exists {
			for _, topicPartition := range topic.load().partitions {
				topicPartition.records.bumpRepeatedLoadErr(err)
			}
		}
	}

	p.unknownTopicsMu.Lock()
	defer p.unknownTopicsMu.Unlock()

	for topic, unknown := range p.unknownTopics {
		// if nil, mode 1 (req err), else mode 2 (missing resp)
		if missing != nil && !missing[topic] {
			continue
		}

		select {
		case unknown.wait <- err:
		default:
		}
	}
}

// topicPartitionsData is the data behind a topicPartitions' v.
//
// We keep this in an atomic because it is expected to be extremely read heavy,
// and if it were behind a lock, the lock would need to be held for a while.
type topicPartitionsData struct {
	// NOTE if adding anything to this struct, be sure to fix meta merge.
	loadErr            error // could be auth, unknown, leader not avail, or creation err
	isInternal         bool
	partitions         []*topicPartition // partition num => partition
	writablePartitions []*topicPartition // subset of above
	topic              string
	when               int64
}

// topicPartition contains all information from Kafka for a topic's partition,
// as well as what a client is producing to it or info about consuming from it.
type topicPartition struct {
	// If we have a load error (leader/listener/replica not available), we
	// keep the old topicPartition data and the new error.
	loadErr error

	// If, on metadata refresh, the leader epoch for this partition goes
	// backwards, we ignore the metadata refresh and signal the metadata
	// should be reloaded: the broker we requested is stale. However, the
	// broker could get into a bad state through some weird cluster failure
	// scenarios. If we see the epoch rewind repeatedly, we eventually keep
	// the metadata refresh. This is not detrimental and at worst will lead
	// to the broker telling us to update our metadata.
	epochRewinds uint8

	// If we do not have a load error, we determine if the new
	// topicPartition is the same or different from the old based on
	// whether the data changed (leader or leader epoch, etc.).
	topicPartitionData

	// If we do not have a load error, we copy the records and cursor
	// pointers from the old after updating any necessary fields in them
	// (see migrate functions below).
	//
	// Only one of records or cursor is non-nil.
	records *recBuf
	cursor  *cursor
}

func (tp *topicPartition) partition() int32 {
	if tp.records != nil {
		return tp.records.partition
	}
	return tp.cursor.partition
}

// Contains stuff that changes on metadata update that we copy into a cursor or
// recBuf.
type topicPartitionData struct {
	// Our leader; if metadata sees this change, the metadata update
	// migrates the cursor to a different source with the session stopped,
	// and the recBuf to a different sink under a tight mutex.
	leader int32

	// What we believe to be the epoch of the leader for this partition.
	//
	// For cursors, for KIP-320, if a broker receives a fetch request where
	// the current leader epoch does not match the brokers, either the
	// broker is behind and returns UnknownLeaderEpoch, or we are behind
	// and the broker returns FencedLeaderEpoch. For the former, we back
	// off and retry. For the latter, we update our metadata.
	leaderEpoch int32
}

// migrateProductionTo is called on metadata update if a topic partition's sink
// has changed. This moves record production from one sink to the other; this
// must be done such that records produced during migration follow those
// already buffered.
func (old *topicPartition) migrateProductionTo(new *topicPartition) { //nolint:revive // old/new naming makes this clearer
	// First, remove our record buffer from the old sink.
	old.records.sink.removeRecBuf(old.records)

	// Before this next lock, record producing will buffer to the
	// in-migration-progress records and may trigger draining to
	// the old sink. That is fine, the old sink no longer consumes
	// from these records. We just have wasted drain triggers.

	old.records.mu.Lock() // guard setting sink and topic partition data
	old.records.sink = new.records.sink
	old.records.topicPartitionData = new.topicPartitionData
	old.records.mu.Unlock()

	// After the unlock above, record buffering can trigger drains
	// on the new sink, which is not yet consuming from these
	// records. Again, just more wasted drain triggers.

	old.records.sink.addRecBuf(old.records) // add our record source to the new sink

	// At this point, the new sink will be draining our records. We lastly
	// need to copy the records pointer to our new topicPartition.
	new.records = old.records
}

// migrateCursorTo is called on metadata update if a topic partition's leader
// or leader epoch has changed.
//
// This is a little bit different from above, in that we do this logic only
// after stopping a consumer session. With the consumer session stopped, we
// have fewer concurrency issues to worry about.
func (old *topicPartition) migrateCursorTo( //nolint:revive // old/new naming makes this clearer
	new *topicPartition,
	css *consumerSessionStopper,
) {
	css.stop()

	old.cursor.source.removeCursor(old.cursor)

	// With the session stopped, we can update fields on the old cursor
	// with no concurrency issue.
	old.cursor.source = new.cursor.source

	// KIP-320: if we had consumed some messages, we need to validate the
	// leader epoch on the new broker to see if we experienced data loss
	// before we can use this cursor.
	//
	// Metadata ensures that leaderEpoch is non-negative only if the broker
	// supports KIP-320.
	if new.leaderEpoch != -1 && old.cursor.lastConsumedEpoch >= 0 {
		// Since the cursor consumed messages, it is definitely usable.
		// We use it so that the epoch load can finish using it
		// properly.
		old.cursor.use()
		css.reloadOffsets.addLoad(old.cursor.topic, old.cursor.partition, loadTypeEpoch, offsetLoad{
			replica: -1,
			Offset: Offset{
				at:    old.cursor.offset,
				epoch: old.cursor.lastConsumedEpoch,
			},
		})
	}

	old.cursor.topicPartitionData = new.topicPartitionData

	old.cursor.source.addCursor(old.cursor)
	new.cursor = old.cursor
}

type kip951move struct {
	recBufs map[*recBuf]topicPartitionData
	cursors map[*cursor]topicPartitionData
	brokers []BrokerMetadata
}

func (k *kip951move) empty() bool {
	return len(k.brokers) == 0
}

func (k *kip951move) hasRecBuf(rb *recBuf) bool {
	if k == nil || k.recBufs == nil {
		return false
	}
	_, ok := k.recBufs[rb]
	return ok
}

func (k *kip951move) maybeAddProducePartition(resp *kmsg.ProduceResponse, p *kmsg.ProduceResponseTopicPartition, rb *recBuf) bool {
	if resp.GetVersion() < 10 ||
		p.ErrorCode != kerr.NotLeaderForPartition.Code ||
		len(resp.Brokers) == 0 ||
		p.CurrentLeader.LeaderID < 0 ||
		p.CurrentLeader.LeaderEpoch < 0 {
		return false
	}
	if len(k.brokers) == 0 {
		for _, rb := range resp.Brokers {
			b := BrokerMetadata{
				NodeID: rb.NodeID,
				Host:   rb.Host,
				Port:   rb.Port,
				Rack:   rb.Rack,
			}
			k.brokers = append(k.brokers, b)
		}
	}
	if k.recBufs == nil {
		k.recBufs = make(map[*recBuf]topicPartitionData)
	}
	k.recBufs[rb] = topicPartitionData{
		leader:      p.CurrentLeader.LeaderID,
		leaderEpoch: p.CurrentLeader.LeaderEpoch,
	}
	return true
}

func (k *kip951move) maybeAddFetchPartition(resp *kmsg.FetchResponse, p *kmsg.FetchResponseTopicPartition, c *cursor) bool {
	if resp.GetVersion() < 16 ||
		p.ErrorCode != kerr.NotLeaderForPartition.Code ||
		len(resp.Brokers) == 0 ||
		p.CurrentLeader.LeaderID < 0 ||
		p.CurrentLeader.LeaderEpoch < 0 {
		return false
	}

	if len(k.brokers) == 0 {
		for _, rb := range resp.Brokers {
			b := BrokerMetadata{
				NodeID: rb.NodeID,
				Host:   rb.Host,
				Port:   rb.Port,
				Rack:   rb.Rack,
			}
			k.brokers = append(k.brokers, b)
		}
	}
	if k.cursors == nil {
		k.cursors = make(map[*cursor]topicPartitionData)
	}
	k.cursors[c] = topicPartitionData{
		leader:      p.CurrentLeader.LeaderID,
		leaderEpoch: p.CurrentLeader.LeaderEpoch,
	}
	return true
}

func (k *kip951move) ensureSinksAndSources(cl *Client) {
	cl.sinksAndSourcesMu.Lock()
	defer cl.sinksAndSourcesMu.Unlock()

	ensure := func(leader int32) {
		if _, exists := cl.sinksAndSources[leader]; exists {
			return
		}
		cl.sinksAndSources[leader] = sinkAndSource{
			sink:   cl.newSink(leader),
			source: cl.newSource(leader),
		}
	}

	for _, td := range k.recBufs {
		ensure(td.leader)
	}
	for _, td := range k.cursors {
		ensure(td.leader)
	}
}

func (k *kip951move) ensureBrokers(cl *Client) {
	if len(k.brokers) == 0 {
		return
	}

	kbs := make([]kmsg.MetadataResponseBroker, 0, len(k.brokers))
	for _, b := range k.brokers {
		kbs = append(kbs, kmsg.MetadataResponseBroker{
			NodeID: b.NodeID,
			Host:   b.Host,
			Port:   b.Port,
			Rack:   b.Rack,
		})
	}
	cl.updateBrokers(kbs)
}

func (k *kip951move) maybeBeginMove(cl *Client) {
	if k.empty() {
		return
	}
	// We want to do the move independent of whatever is calling us, BUT we
	// want to ensure it is not concurrent with a metadata request.
	go cl.blockingMetadataFn(func() {
		k.ensureBrokers(cl)
		k.ensureSinksAndSources(cl)
		k.doMove(cl)
	})
}

func (k *kip951move) doMove(cl *Client) {
	// Moving partitions is theoretically simple, but the client is written
	// in a confusing way around concurrency.
	//
	// The problem is that topicPartitionsData is read-only after
	// initialization. Updates are done via atomic stores of the containing
	// topicPartitionsData struct. Moving a single partition requires some
	// deep copying.

	// oldNew pairs what NEEDS to be atomically updated (old; left value)
	// with the value that WILL be stored (new; right value).
	type oldNew struct {
		l *topicPartitions
		r *topicPartitionsData
	}
	topics := make(map[string]oldNew)

	// getT returns the oldNew for the topic, performing a shallow clone of
	// the old whole-topic struct.
	getT := func(m topicsPartitionsData, topic string) (oldNew, bool) {
		lr, ok := topics[topic]
		if !ok {
			l := m[topic]
			if l == nil {
				return oldNew{}, false
			}
			dup := *l.load()
			r := &dup
			r.writablePartitions = append([]*topicPartition{}, r.writablePartitions...)
			r.partitions = append([]*topicPartition{}, r.partitions...)
			lr = oldNew{l, r}
			topics[topic] = lr
		}
		return lr, true
	}

	// modifyP returns the old topicPartition and a new one that will be
	// used in migrate<Fn>To. The new topicPartition only contains the sink
	// and topicPartitionData that will be copied into old under old's
	// mutex. The actual migration is done in the migrate function (see
	// below).
	//
	// A migration is not needed if the old value has a higher leader
	// epoch.  If the leader epoch is equal, we check if the leader is the
	// same (this allows easier injection of failures in local testing).  A
	// higher epoch can come from a concurrent metadata update that
	// actually performed the move first.
	modifyP := func(d *topicPartitionsData, partition int32, td topicPartitionData) (old, new *topicPartition, modified bool) {
		old = d.partitions[partition]
		if old.leaderEpoch > td.leaderEpoch {
			return nil, nil, false
		}
		if old.leaderEpoch == td.leaderEpoch && old.leader == td.leader {
			return nil, nil, false
		}

		cl.sinksAndSourcesMu.Lock()
		sns := cl.sinksAndSources[td.leader]
		cl.sinksAndSourcesMu.Unlock()

		dup := *old
		new = &dup
		new.topicPartitionData = topicPartitionData{
			leader:      td.leader,
			leaderEpoch: td.leaderEpoch,
		}
		if new.records != nil {
			new.records = &recBuf{
				sink:               sns.sink,
				topicPartitionData: new.topicPartitionData,
			}
		} else {
			new.cursor = &cursor{
				source:             sns.source,
				topicPartitionData: new.topicPartitionData,
			}
		}

		// We now have to mirror the new partition back to the topic
		// slice that will be atomically stored.
		d.partitions[partition] = new
		idxWritable := sort.Search(len(d.writablePartitions), func(i int) bool { return d.writablePartitions[i].partition() >= partition })
		if idxWritable < len(d.writablePartitions) && d.writablePartitions[idxWritable].partition() == partition {
			if d.writablePartitions[idxWritable] != old {
				panic("invalid invariant -- partition in writablePartitions != partition at expected index in partitions")
			}
			d.writablePartitions[idxWritable] = new
		}

		return old, new, true
	}

	if k.recBufs != nil {
		tpsProducer := cl.producer.topics.load() // must be non-nil, since we have recBufs to move
		for recBuf, td := range k.recBufs {
			lr, ok := getT(tpsProducer, recBuf.topic)
			if !ok {
				continue // perhaps concurrently purged
			}
			old, new, modified := modifyP(lr.r, recBuf.partition, td)
			if modified {
				cl.cfg.logger.Log(LogLevelInfo, "moving producing partition due to kip-951 not_leader_for_partition",
					"topic", recBuf.topic,
					"partition", recBuf.partition,
					"new_leader", new.leader,
					"new_leader_epoch", new.leaderEpoch,
					"old_leader", old.leader,
					"old_leader_epoch", old.leaderEpoch,
				)
				old.migrateProductionTo(new)
			} else {
				recBuf.clearFailing()
			}
		}
	} else {
		var tpsConsumer topicsPartitionsData
		c := &cl.consumer
		switch {
		case c.g != nil:
			tpsConsumer = c.g.tps.load()
		case c.d != nil:
			tpsConsumer = c.d.tps.load()
		}
		css := &consumerSessionStopper{cl: cl}
		defer css.maybeRestart()
		for cursor, td := range k.cursors {
			lr, ok := getT(tpsConsumer, cursor.topic)
			if !ok {
				continue // perhaps concurrently purged
			}
			old, new, modified := modifyP(lr.r, cursor.partition, td)
			if modified {
				cl.cfg.logger.Log(LogLevelInfo, "moving consuming partition due to kip-951 not_leader_for_partition",
					"topic", cursor.topic,
					"partition", cursor.partition,
					"new_leader", new.leader,
					"new_leader_epoch", new.leaderEpoch,
					"old_leader", old.leader,
					"old_leader_epoch", old.leaderEpoch,
				)
				old.migrateCursorTo(new, css)
			}
		}
	}

	// We can always do a simple store. For producing, we *must* have
	// had partitions, so this is not updating an unknown topic.
	for _, lr := range topics {
		lr.l.v.Store(lr.r)
	}
}

// Migrating a cursor requires stopping any consumer session. If we
// stop a session, we need to eventually re-start any offset listing or
// epoch loading that was stopped. Thus, we simply merge what we
// stopped into what we will reload.
type consumerSessionStopper struct {
	cl            *Client
	stopped       bool
	reloadOffsets listOrEpochLoads
	tpsPrior      *topicsPartitions
}

func (css *consumerSessionStopper) stop() {
	if css.stopped {
		return
	}
	css.stopped = true
	loads, tps := css.cl.consumer.stopSession()
	css.reloadOffsets.mergeFrom(loads)
	css.tpsPrior = tps
}

func (css *consumerSessionStopper) maybeRestart() {
	if !css.stopped {
		return
	}
	session := css.cl.consumer.startNewSession(css.tpsPrior)
	defer session.decWorker()
	css.reloadOffsets.loadWithSession(session, "resuming reload offsets after session stopped for cursor migrating in metadata")
}
