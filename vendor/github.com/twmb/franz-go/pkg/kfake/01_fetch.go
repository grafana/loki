package kfake

import (
	"slices"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// Fetch: v4-18
//
// Behavior:
// * If topic does not exist, we hang
// * Topic created while waiting is not returned in final response
// * If any partition is on a different broker, we return immediately
// * Out of range fetch causes early return
// * Raw bytes of batch counts against wait bytes
//
// For read_committed (IsolationLevel=1):
// * We only count stable bytes (inTx=false) toward MinBytes - this includes
//   committed txn batches, aborted txn batches, and non-txn batches
// * Uncommitted txn batches (inTx=true) are not counted and not returned
// * MinBytes is evaluated across all partitions, so stable data from
//   other partitions can satisfy MinBytes even if one partition is all in-txn
// * When a transaction commits/aborts, waiting read_committed fetches are woken
//
// Fetch sessions (KIP-227):
// * Sessions allow incremental fetches where clients only send changed partitions
// * We track session state per broker and merge with request to get full partition list
// * Incremental responses omit unchanged partitions (no records, same HWM/logStartOffset, no error)
//
// Version notes:
// * v4: RecordBatch format, IsolationLevel, LastStableOffset, AbortedTransactions
// * v5: LogStartOffset (KIP-107)
// * v7: Fetch sessions (KIP-227)
// * v9: CurrentLeaderEpoch for epoch fencing (KIP-320)
// * v11: Rack in request (KIP-392) - ignored
// * v12: LastFetchedEpoch for divergence detection - not implemented
// * v13: TopicID (KIP-516)
// * v15: ReplicaState (KIP-903) - broker-only, ignored
// * v18: HighWatermark tag (KIP-1166) - broker-only, ignored

func init() { regKey(1, 4, 18) }

func (c *Cluster) handleFetch(creq *clientReq, w *watchFetch) (kmsg.Response, error) {
	var (
		req  = creq.kreq.(*kmsg.FetchRequest)
		resp = req.ResponseKind().(*kmsg.FetchResponse)
	)

	if err := c.checkReqVersion(req.Key(), req.Version); err != nil {
		return nil, err
	}

	// Handle fetch sessions (v7+)
	var session *fetchSession
	var newSession bool
	if req.Version >= 7 {
		var errCode int16
		session, newSession, errCode = c.fetchSessions.getOrCreate(creq.cc.b.node, req.SessionID, req.SessionEpoch, int(c.fetchSessionCacheSlots()))
		if errCode != 0 {
			resp.ErrorCode = errCode
			return resp, nil
		}

		// Handle ForgottenTopics - remove partitions from session
		for _, ft := range req.ForgottenTopics {
			topic := ft.Topic
			if req.Version >= 13 {
				topic = c.data.id2t[ft.TopicID]
			}
			for _, p := range ft.Partitions {
				if req.Version >= 13 {
					session.forgetUnknownID(ft.TopicID, p)
				}
				session.forgetPartition(topic, p)
			}
		}

		// Set session ID in response
		if session != nil {
			resp.SessionID = session.id
		}
	}

	// Build the list of partitions to fetch. With sessions, we need to merge:
	// 1. Partitions from the request (may be updates or new partitions)
	// 2. Partitions already in the session (for incremental fetches)
	type fetchPartition struct {
		topic        string
		topicID      uuid
		partition    int32
		fetchOffset  int64
		maxBytes     int32
		currentEpoch int32
		// staleID marks a session entry whose topic ID no longer
		// resolves while its cached name still does: the topic was
		// recreated under that name. Kafka resolves a session entry's
		// name once, at insertion, so the entry reaches the log by
		// name and fails the ID check there.
		staleID bool
	}
	var toFetch []fetchPartition

	// First, add partitions from the request and update session
	for i, rt := range req.Topics {
		if req.Version >= 13 {
			rt.Topic = c.data.id2t[rt.TopicID]
			req.Topics[i].Topic = rt.Topic
		}
		for _, rp := range rt.Partitions {
			fp := fetchPartition{
				topic:        rt.Topic,
				topicID:      rt.TopicID,
				partition:    rp.Partition,
				fetchOffset:  rp.FetchOffset,
				maxBytes:     rp.PartitionMaxBytes,
				currentEpoch: rp.CurrentLeaderEpoch,
			}
			// Update session with this partition's state. Kafka
			// matches a v13 request partition to a session entry
			// by topic ID and keeps the name the entry was added
			// under, so an ID that stopped resolving still reaches
			// the log by that name. An unresolvable ID with no
			// entry is tracked by ID: Kafka caches it with no name
			// and answers UNKNOWN_TOPIC_ID for it on every fetch of
			// the session.
			if fp.topic == "" && req.Version >= 13 {
				fp.topic = session.nameOfID(rt.TopicID, rp.Partition)
				fp.staleID = fp.topic != ""
			}
			if fp.topic != "" || req.Version < 13 {
				session.updatePartition(fp.topic, rt.TopicID, rp.Partition, rp.FetchOffset, rp.PartitionMaxBytes, rp.CurrentLeaderEpoch)
			} else {
				session.updateUnknownID(rt.TopicID, rp.Partition, rp.FetchOffset, rp.PartitionMaxBytes, rp.CurrentLeaderEpoch)
			}
			toFetch = append(toFetch, fp)
		}
	}

	// For incremental fetches (session exists and not new), add session partitions
	// that weren't in the request
	if session != nil && !newSession {
		inRequest := make(map[tp]bool)
		inRequestUnknown := make(map[idp]bool)
		for _, fp := range toFetch {
			if fp.topic == "" {
				inRequestUnknown[idp{fp.topicID, fp.partition}] = true
				continue
			}
			inRequest[tp{fp.topic, fp.partition}] = true
		}
		for key, sp := range session.unknownIDs {
			if inRequestUnknown[key] {
				continue
			}
			// An ID that resolves now was created after the entry was
			// added; it becomes a normal entry under its name, which
			// the loop below picks up.
			if topic := c.data.id2t[key.id]; topic != "" {
				delete(session.unknownIDs, key)
				session.partitions[tp{topic, key.p}] = sp
				continue
			}
			toFetch = append(toFetch, fetchPartition{
				topicID:      key.id,
				partition:    key.p,
				fetchOffset:  sp.fetchOffset,
				maxBytes:     sp.maxBytes,
				currentEpoch: sp.currentEpoch,
			})
		}
		for key, sp := range session.partitions {
			if !inRequest[key] {
				topic := key.t
				var staleID bool
				if sp.topicID != (uuid{}) {
					// v13+ entries are addressed by the ID they
					// were added with. If it no longer resolves,
					// the entry keeps the name it was inserted
					// under, like a real broker: a deleted topic
					// answers UNKNOWN_TOPIC_ID, a recreated one
					// INCONSISTENT_TOPIC_ID from a broker hosting
					// the new incarnation.
					if _, ok := c.data.id2t[sp.topicID]; !ok {
						staleID = true
					}
				}
				toFetch = append(toFetch, fetchPartition{
					topic:        topic,
					topicID:      sp.topicID,
					partition:    key.p,
					fetchOffset:  sp.fetchOffset,
					maxBytes:     sp.maxBytes,
					currentEpoch: sp.currentEpoch,
					staleID:      staleID,
				})
			}
		}
	}

	var (
		readCommitted = req.IsolationLevel == 1
		nbytes        int
		returnEarly   bool
		needp         tps[int]
		fc            = creq.faults
		syn           = c.cfg.synthetic
	)
	if syn != nil {
		// A synthetic cluster serves its canned batch from any offset,
		// so there is always data: we answer now rather than waiting.
		returnEarly = true
	} else if w == nil {
		// Any partition that errors completes the fetch at once, as a
		// real broker's fetch purgatory does; only partitions waiting
		// on data hold the request for MaxWait.
	out:
		for _, fp := range toFetch {
			if e := fc.check(faultKey{topic: fp.topic, topicID: fp.topicID}.part(fp.partition)); e != nil {
				returnEarly = true // the fault's error
				break out
			}
			if fp.staleID {
				returnEarly = true // InconsistentTopicID or UnknownTopicID
				break out
			}
			t, ok := c.data.tps.gett(fp.topic)
			if !ok {
				returnEarly = true // UnknownTopicID or UnknownTopicOrPartition
				break out
			}
			pd, ok := t[fp.partition]
			if !ok {
				returnEarly = true // UnknownTopicID or UnknownTopicOrPartition
				break out
			}
			if pd.leader != creq.cc.b && !slices.Contains(pd.followers, creq.cc.b.node) {
				returnEarly = true // NotLeaderForPartition
				break out
			}
			if le := fp.currentEpoch; le != -1 && le != pd.epoch {
				returnEarly = true // FencedLeaderEpoch or UnknownLeaderEpoch
				break out
			}
			segIdx, metaIdx, ok, atEnd := pd.searchOffset(fp.fetchOffset)
			if atEnd {
				continue
			}
			if !ok {
				returnEarly = true // OffsetOutOfRange
				break out
			}
			pbytes := 0
			// Walk batches starting from the found position (segIdx, metaIdx).
			// The first segment starts at metaIdx; subsequent segments start at 0.
			for si := segIdx; si < len(pd.segments); si++ {
				seg := &pd.segments[si]
				startIdx := 0
				if si == segIdx {
					startIdx = metaIdx
				}
				for batchIdx := startIdx; batchIdx < len(seg.index); batchIdx++ {
					m := &seg.index[batchIdx]
					if readCommitted && m.firstOffset >= pd.lastStableOffset {
						break
					}
					nbytes += int(m.nbytes)
					pbytes += int(m.nbytes)
					if pbytes >= int(fp.maxBytes) {
						returnEarly = true
						break out
					}
				}
			}
			needp.set(fp.topic, fp.partition, int(fp.maxBytes)-pbytes)
		}
	}

	wait := time.Duration(req.MaxWaitMillis) * time.Millisecond
	deadline := creq.at.Add(wait)
	if w == nil && !returnEarly && nbytes < int(req.MinBytes) && time.Now().Before(deadline) {
		w := &watchFetch{
			need:          int(req.MinBytes) - nbytes,
			needp:         needp,
			readCommitted: readCommitted,
			creq:          creq,
		}
		w.cb = func() {
			select {
			case c.watchFetchCh <- w:
			case <-c.die:
			}
		}
		for _, fp := range toFetch {
			t, ok := c.data.tps.gett(fp.topic)
			if !ok {
				continue
			}
			pd, ok := t[fp.partition]
			if !ok {
				continue
			}
			pd.watch[w] = struct{}{}
			w.in = append(w.in, pd)
		}
		w.t = time.AfterFunc(wait, w.cb)
		return nil, nil
	}

	id2t := make(map[uuid]string)
	tidx := make(map[string]int)

	donet := func(t string, id uuid) *kmsg.FetchResponseTopic {
		key := t
		if t == "" {
			key = string(id[:]) // unresolvable v13 IDs each get their own entry
		}
		if i, ok := tidx[key]; ok {
			return &resp.Topics[i]
		}
		id2t[id] = t
		tidx[key] = len(resp.Topics)
		st := kmsg.NewFetchResponseTopic()
		st.Topic = t
		st.TopicID = id
		resp.Topics = append(resp.Topics, st)
		return &resp.Topics[len(resp.Topics)-1]
	}
	donep := func(t string, id uuid, p int32, errCode int16) *kmsg.FetchResponseTopicPartition {
		sp := kmsg.NewFetchResponseTopicPartition()
		sp.Partition = p
		sp.ErrorCode = errCode
		// Encode empty record sets as an empty-but-present byte slice rather
		// than null. Technically the protocol allows nullable records here,
		// but some clients (e.g. librdkafka) interpret a nil slice (length -1)
		// as an invalid MessageSet size.
		sp.RecordBatches = []byte{}
		st := donet(t, id)
		st.Partitions = append(st.Partitions, sp)
		return &st.Partitions[len(st.Partitions)-1]
	}

	var includeBrokers bool
	defer func() {
		if includeBrokers {
			for _, b := range c.bs {
				sb := kmsg.NewFetchResponseBroker()
				sb.NodeID = b.node
				sb.Host, sb.Port = b.hostport()
				sb.Rack = &brokerRack
				resp.Brokers = append(resp.Brokers, sb)
			}
		}
	}()

	var batchesAdded int
	nbytes = 0
full:
	for _, fp := range toFetch {
		if e := c.deny(creq, fp.topic, kmsg.ACLResourceTypeTopic, kmsg.ACLOperationRead, faultKey{topic: fp.topic, topicID: fp.topicID}.part(fp.partition)); e != nil {
			donep(fp.topic, fp.topicID, fp.partition, e.Code)
			continue
		}
		pd, ok := c.data.tps.getp(fp.topic, fp.partition)
		if !ok {
			// A v13 partition with a name got it from its session
			// entry; the entry reaches the log by that name, as on
			// a real broker, and fails as a missing partition.
			if req.Version >= 13 && fp.topic == "" {
				donep(fp.topic, fp.topicID, fp.partition, kerr.UnknownTopicID.Code)
			} else {
				donep(fp.topic, fp.topicID, fp.partition, kerr.UnknownTopicOrPartition.Code)
			}
			continue
		}
		if fp.staleID {
			// The session entry's name now belongs to a new
			// incarnation. A broker hosting it rejects the stale ID
			// as inconsistent; one that does not is simply not the
			// leader.
			if pd.leader != creq.cc.b && !slices.Contains(pd.followers, creq.cc.b.node) {
				donep(fp.topic, fp.topicID, fp.partition, kerr.NotLeaderForPartition.Code)
			} else {
				donep(fp.topic, fp.topicID, fp.partition, kerr.InconsistentTopicID.Code)
			}
			continue
		}
		if pd.leader != creq.cc.b && !slices.Contains(pd.followers, creq.cc.b.node) {
			p := donep(fp.topic, fp.topicID, fp.partition, kerr.NotLeaderForPartition.Code)
			p.CurrentLeader.LeaderID = pd.leader.node
			p.CurrentLeader.LeaderEpoch = pd.epoch
			includeBrokers = true
			continue
		}
		// CurrentLeaderEpoch validation (KIP-320, v9+)
		if le := fp.currentEpoch; le != -1 {
			if le < pd.epoch {
				donep(fp.topic, fp.topicID, fp.partition, kerr.FencedLeaderEpoch.Code)
				continue
			} else if le > pd.epoch {
				donep(fp.topic, fp.topicID, fp.partition, kerr.UnknownLeaderEpoch.Code)
				continue
			}
		}
		sp := donep(fp.topic, fp.topicID, fp.partition, 0)
		if syn != nil {
			sp.HighWatermark = syntheticEnd
			sp.LastStableOffset = syntheticEnd
			sp.LogStartOffset = 0
			room := min(int(fp.maxBytes), int(req.MaxBytes)-nbytes)
			sp.RecordBatches = syn.appendBatches(sp.RecordBatches, fp.fetchOffset, pd.epoch, room)
			nbytes += len(sp.RecordBatches)
			continue
		}
		sp.HighWatermark = pd.highWatermark
		sp.LastStableOffset = pd.lastStableOffset
		sp.LogStartOffset = pd.logStartOffset
		segIdx, metaIdx, ok, atEnd := pd.searchOffset(fp.fetchOffset)
		if atEnd {
			continue
		}
		if !ok {
			sp.ErrorCode = kerr.OffsetOutOfRange.Code
			continue
		}

		var pbytes, pBatchesAdded int
		var lastMeta *batchMeta
		// Walk batches starting from the found position (segIdx, metaIdx).
		// The first segment starts at metaIdx; subsequent segments start at 0.
	segments:
		for si := segIdx; si < len(pd.segments); si++ {
			seg := &pd.segments[si]
			startIdx := 0
			if si == segIdx {
				startIdx = metaIdx
			}
			for batchIdx := startIdx; batchIdx < len(seg.index); batchIdx++ {
				m := &seg.index[batchIdx]
				if readCommitted && m.firstOffset >= pd.lastStableOffset {
					break segments
				}
				if nbytes += int(m.nbytes); nbytes > int(req.MaxBytes) && batchesAdded > 0 {
					break full
				}
				if pbytes += int(m.nbytes); pbytes > int(fp.maxBytes) && batchesAdded > 0 {
					break segments
				}
				batchesAdded++
				pBatchesAdded++
				lastMeta = m
				raw, err := c.readBatchRaw(pd, si, m)
				if err != nil {
					sp.ErrorCode = kerr.UnknownServerError.Code
					break segments
				}
				sp.RecordBatches = append(sp.RecordBatches, raw...)
			}
		}

		// For read_committed, look up aborted transactions that overlap
		// the returned data range using the partition's aborted txn index.
		if readCommitted && req.Version >= 4 && pBatchesAdded > 0 {
			upperBound := lastMeta.firstOffset + int64(lastMeta.lastOffsetDelta) + 1
			// Binary search for the first entry whose abort marker
			// is at or after the fetch offset.
			j := sort.Search(len(pd.abortedTxns), func(k int) bool {
				return pd.abortedTxns[k].lastOffset >= fp.fetchOffset
			})
			for ; j < len(pd.abortedTxns); j++ {
				e := pd.abortedTxns[j]
				if e.firstOffset >= upperBound {
					continue
				}
				at := kmsg.NewFetchResponseTopicPartitionAbortedTransaction()
				at.ProducerID = e.producerID
				at.FirstOffset = e.firstOffset
				sp.AbortedTransactions = append(sp.AbortedTransactions, at)
			}
		}
	}

	// Record partition state in the session. For incremental fetches
	// (not new), also filter out unchanged partitions.
	session.updateAndFilterResponse(resp, !newSession)

	// Bump session epoch after successful fetch, but not for newly created
	// sessions. The client will send epoch=1 for its first incremental fetch
	// after receiving the new session ID.
	if !newSession {
		session.bumpEpoch()
	}

	return resp, nil
}

type watchFetch struct {
	need  int
	needp tps[int]
	creq  *clientReq

	in []*partData
	cb func()
	t  *time.Timer

	readCommitted bool

	once    sync.Once
	cleaned bool
}

func (w *watchFetch) push(pd *partData, nbytes int, inTx bool) {
	// For readCommitted consumers, skip counting bytes from uncommitted transactional batches.
	// These bytes will be counted when the transaction commits via addBytes.
	if w.readCommitted && inTx {
		return
	}
	w.addBytes(pd, nbytes)
}

// addBytes counts nbytes toward MinBytes satisfaction for a partition.
// Called by push() for normal produces, and directly from txns.go when
// a transaction commits (to count previously-skipped transactional bytes).
func (w *watchFetch) addBytes(pd *partData, nbytes int) {
	w.need -= nbytes
	needp, _ := w.needp.getp(pd.t, pd.p)
	if needp != nil {
		*needp -= nbytes
	}
	if w.need <= 0 || needp != nil && *needp <= 0 {
		w.do()
	}
}

func (w *watchFetch) deleted() { w.do() }

func (w *watchFetch) do() {
	w.once.Do(func() {
		go w.cb()
	})
}

func (w *watchFetch) cleanup() {
	w.cleaned = true
	for _, in := range w.in {
		delete(in.watch, w)
	}
	w.t.Stop()
}

// Fetch sessions (KIP-227)

// fetchSessions manages fetch sessions per KIP-227. Sessions are scoped per
// broker; since kfake simulates multiple brokers in one process, we key by
// broker node ID.
type fetchSessions struct {
	nextID   atomic.Int32
	sessions map[int32]map[int32]*fetchSession // broker node -> session ID -> session
}

// fetchSession tracks the state of a single fetch session.
type fetchSession struct {
	id         int32
	epoch      int32
	partitions map[tp]fetchSessionPartition
	// unknownIDs holds v13+ entries whose topic ID did not resolve when
	// they were added. Kafka caches these with no name and answers
	// UNKNOWN_TOPIC_ID for them on every fetch of the session; a client
	// that keeps such an entry in its session sees the error on every
	// fetch rather than once.
	unknownIDs map[idp]fetchSessionPartition
	lastUsed   time.Time
}

// idp keys a session entry by topic ID and partition.
type idp struct {
	id uuid
	p  int32
}

// fetchSessionPartition tracks per-partition state within a session.
type fetchSessionPartition struct {
	// topicID is what the requester addressed this partition by (zero
	// below fetch v13). v13+ sessions are topic-ID addressed: if this ID
	// stops resolving (topic deleted, or recreated under a new ID), the
	// entry answers UNKNOWN_TOPIC_ID rather than re-addressing by name.
	topicID      uuid
	fetchOffset  int64
	maxBytes     int32
	currentEpoch int32

	// Cached from last response sent. Used to detect changes for
	// incremental filtering. Initialized to -1 to force inclusion
	// in the first response after the partition is added.
	lastHighWatermark  int64
	lastLogStartOffset int64
}

const defMaxFetchSessionCacheSlots = 1000

func (fs *fetchSessions) init(brokerNode int32) {
	if fs.sessions == nil {
		fs.sessions = make(map[int32]map[int32]*fetchSession)
		fs.nextID.Store(1)
	}
	if fs.sessions[brokerNode] == nil {
		fs.sessions[brokerNode] = make(map[int32]*fetchSession)
	}
}

// getOrCreate returns an existing session or creates a new one based on the
// request's SessionID and SessionEpoch. Returns (nil, false, 0) for legacy
// sessionless fetches.
func (fs *fetchSessions) getOrCreate(brokerNode, sessionID, sessionEpoch int32, maxSlots int) (*fetchSession, bool, int16) {
	fs.init(brokerNode)

	// SessionEpoch=-1: Full fetch, no session (legacy mode).
	// If sessionID>0, this is the client unregistering/killing the session
	// (e.g., during source shutdown via killSessionOnClose).
	if sessionEpoch == -1 {
		if sessionID > 0 {
			delete(fs.sessions[brokerNode], sessionID)
		}
		return nil, false, 0
	}

	// SessionEpoch=0: Create new session (or unregister existing and create new).
	// When sessionID>0 with epoch=0, the client is requesting to unregister
	// the old session and start fresh (per KIP-227).
	if sessionEpoch == 0 {
		if sessionID > 0 {
			delete(fs.sessions[brokerNode], sessionID)
		}
		// Evict the oldest session if we're at the limit.
		brokerSessions := fs.sessions[brokerNode]
		if len(brokerSessions) >= maxSlots {
			var oldestID int32
			var oldestTime time.Time
			for id, s := range brokerSessions {
				if oldestTime.IsZero() || s.lastUsed.Before(oldestTime) {
					oldestID = id
					oldestTime = s.lastUsed
				}
			}
			delete(brokerSessions, oldestID)
		}
		now := time.Now()
		id := fs.nextID.Add(1) - 1
		session := &fetchSession{
			id:         id,
			epoch:      1,
			partitions: make(map[tp]fetchSessionPartition),
			unknownIDs: make(map[idp]fetchSessionPartition),
			lastUsed:   now,
		}
		fs.sessions[brokerNode][id] = session
		return session, true, 0
	}

	// SessionID>0, SessionEpoch>0: Look up existing session
	session, ok := fs.sessions[brokerNode][sessionID]
	if !ok {
		return nil, false, kerr.FetchSessionIDNotFound.Code
	}
	if sessionEpoch != session.epoch {
		return nil, false, kerr.InvalidFetchSessionEpoch.Code
	}
	session.lastUsed = time.Now()
	return session, false, 0
}

func (s *fetchSession) updatePartition(topic string, topicID uuid, partition int32, fetchOffset int64, maxBytes, currentEpoch int32) {
	if s == nil {
		return
	}
	key := tp{topic, partition}
	if existing, ok := s.partitions[key]; ok {
		existing.topicID = topicID
		existing.fetchOffset = fetchOffset
		existing.maxBytes = maxBytes
		existing.currentEpoch = currentEpoch
		s.partitions[key] = existing
	} else {
		s.partitions[key] = fetchSessionPartition{
			topicID:            topicID,
			fetchOffset:        fetchOffset,
			maxBytes:           maxBytes,
			currentEpoch:       currentEpoch,
			lastHighWatermark:  -1,
			lastLogStartOffset: -1,
		}
	}
}

func (s *fetchSession) forgetPartition(topic string, partition int32) {
	if s == nil {
		return
	}
	delete(s.partitions, tp{topic, partition})
}

// nameOfID returns the name of the session entry added under topicID for
// partition, if any.
func (s *fetchSession) nameOfID(topicID uuid, partition int32) string {
	if s == nil {
		return ""
	}
	for key, sp := range s.partitions {
		if sp.topicID == topicID && key.p == partition {
			return key.t
		}
	}
	return ""
}

func (s *fetchSession) updateUnknownID(topicID uuid, partition int32, fetchOffset int64, maxBytes, currentEpoch int32) {
	if s == nil {
		return
	}
	s.unknownIDs[idp{topicID, partition}] = fetchSessionPartition{
		topicID:            topicID,
		fetchOffset:        fetchOffset,
		maxBytes:           maxBytes,
		currentEpoch:       currentEpoch,
		lastHighWatermark:  -1,
		lastLogStartOffset: -1,
	}
}

func (s *fetchSession) forgetUnknownID(topicID uuid, partition int32) {
	if s == nil {
		return
	}
	delete(s.unknownIDs, idp{topicID, partition})
}

func (s *fetchSession) bumpEpoch() {
	if s == nil {
		return
	}
	s.epoch++
	if s.epoch < 0 {
		s.epoch = 1
	}
}

// updateAndFilterResponse records the HWM and logStartOffset from each
// partition as baseline state. When filter is true, unchanged partitions
// (no records, same HWM/logStartOffset, no error) are removed from the
// response for incremental fetch.
func (s *fetchSession) updateAndFilterResponse(resp *kmsg.FetchResponse, filter bool) {
	if s == nil {
		return
	}
	n := 0
	for i := range resp.Topics {
		rt := &resp.Topics[i]
		np := 0
		for j := range rt.Partitions {
			rp := &rt.Partitions[j]
			key := tp{rt.Topic, rp.Partition}
			sp, ok := s.partitions[key]

			include := !filter || !ok ||
				len(rp.RecordBatches) > 0 ||
				rp.ErrorCode != 0 ||
				rp.HighWatermark != sp.lastHighWatermark ||
				rp.LogStartOffset != sp.lastLogStartOffset

			if ok {
				if rp.ErrorCode != 0 {
					sp.lastHighWatermark = -1
					sp.lastLogStartOffset = -1
				} else {
					sp.lastHighWatermark = rp.HighWatermark
					sp.lastLogStartOffset = rp.LogStartOffset
				}
				s.partitions[key] = sp
			}

			if include {
				rt.Partitions[np] = *rp
				np++
			}
		}
		rt.Partitions = rt.Partitions[:np]
		if len(rt.Partitions) > 0 {
			resp.Topics[n] = *rt
			n++
		}
	}
	resp.Topics = resp.Topics[:n]
}
