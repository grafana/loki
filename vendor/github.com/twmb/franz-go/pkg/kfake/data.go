package kfake

import (
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"math/rand"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"
)

var (
	noID   uuid
	crc32c = crc32.MakeTable(crc32.Castagnoli)
)

type (
	uuid [16]byte

	data struct {
		c   *Cluster
		tps tps[partData]

		id2t      map[uuid]string               // topic IDs => topic name
		t2id      map[string]uuid               // topic name => topic IDs
		treplicas map[string]int                // topic name => # replicas
		tcfgs     map[string]map[string]*string // topic name => config name => config value
		tnorms    map[string]string             // normalized name (. replaced with _) => topic name
	}

	abortedTxnEntry struct {
		producerID  int64
		firstOffset int64 // first data offset of this txn on this partition
		lastOffset  int64 // offset of the abort control record
	}

	// segmentInfo tracks per-segment metadata. Designed for future
	// LRU eviction of per-segment indexes (seg.index = nil to evict,
	// rebuild by reading segment file).
	//
	// File handle strategy: readFile is a lazily-opened read handle
	// for sealed (non-active) segments. The active segment uses
	// pd.activeSegFile for both writes and reads. All I/O runs in
	// the single Cluster.run() goroutine so no locking is needed.
	// readFile is closed on segment trim (trimLeft) and topic delete.
	segmentInfo struct {
		base     int64       // base offset of first batch
		endOff   int64       // offset after last batch (exclusive), 0 while active
		minEpoch int32       // min epoch across all batches
		maxEpoch int32       // max epoch across all batches
		size     int64       // file size in bytes
		index    []batchMeta // per-batch metadata, nil = evicted
		readFile file        // cached read handle for sealed segments, nil = not yet opened
	}

	// batchMeta is the in-memory index entry for a batch stored in a segment file.
	batchMeta struct {
		firstOffset         int64 // first record offset
		segPos              int64 // byte position within segment file
		maxEarlierTimestamp int64 // for ListOffsets binary search
		firstTimestamp      int64
		maxTimestamp        int64
		producerID          int64 // needed for isBatchAborted in compaction
		lastOffsetDelta     int32
		nbytes              int32 // serialized batch size (RecordBatch wire bytes)
		epoch               int32
		numRecords          int32
		inTx                bool // in an uncommitted transaction
	}

	partData struct {
		abortedTxns []abortedTxnEntry // sorted by lastOffset, for read_committed fetch
		t           string
		p           int32
		dir         string

		highWatermark     int64
		lastStableOffset  int64
		logStartOffset    int64
		epoch             int32 // current epoch
		maxFirstTimestamp int64 // max FirstTimestamp seen (for maxEarlierTimestamp optimization)
		nbytes            int64

		// PID-based LSO tracking: maps producer ID to earliest
		// uncommitted offset on this partition. LSO = min of all
		// values, or HWM if empty. Replaces per-batch inTx scanning.
		uncommittedPIDs map[int64]int64

		// For ListOffsets timestamp -3 (KIP-734): track the batch with max timestamp.
		// Indexes into the flattened batchMeta across all segments.
		maxTimestampSeg int // segment index, -1 if none
		maxTimestampIdx int // index within that segment's batchMeta

		leader    *broker
		followers followers

		watch      map[*watchFetch]struct{}
		shareWatch map[*watchShareFetch]struct{}

		createdAt time.Time

		// Segment state - used in all modes (memFS for in-memory, osFS for disk).
		// Segment files (.dat) contain pure RecordBatch wire bytes.
		// Index files (.idx) contain per-batch metadata (epoch, timestamp, flags).
		activeSegFile file          // open handle for active segment
		activeIdxFile file          // open handle for active index
		segments      []segmentInfo // all segments, sorted by base offset
	}

	followers []int32

	partBatch struct {
		kmsg.RecordBatch
		nbytes int
		epoch  int32 // epoch when appended

		// For list offsets, we may need to return the first offset
		// after a given requested timestamp. Client provided
		// timestamps can go forwards and backwards. We answer list
		// offsets with a binary search: even if this batch has a small
		// timestamp, this is produced _after_ a potentially higher
		// timestamp, so it is after it in the list offset response.
		//
		// When we drop the earlier timestamp, we update all following
		// firstMaxTimestamps that match the dropped timestamp.
		maxEarlierTimestamp int64

		inTx bool
	}
)

// MarshalText implements encoding.TextMarshaler, encoding uuid as base64.
// This enables uuid as JSON map keys and struct fields.
func (u uuid) MarshalText() ([]byte, error) {
	dst := make([]byte, base64.StdEncoding.EncodedLen(len(u)))
	base64.StdEncoding.Encode(dst, u[:])
	return dst, nil
}

// UnmarshalText implements encoding.TextUnmarshaler, decoding uuid from base64.
func (u *uuid) UnmarshalText(text []byte) error {
	_, err := base64.StdEncoding.Decode(u[:], text)
	return err
}

func (b *partBatch) meta(segPos int64) batchMeta {
	return batchMeta{
		firstOffset:         b.FirstOffset,
		segPos:              segPos,
		maxEarlierTimestamp: b.maxEarlierTimestamp,
		firstTimestamp:      b.FirstTimestamp,
		maxTimestamp:        b.MaxTimestamp,
		producerID:          b.ProducerID,
		lastOffsetDelta:     b.LastOffsetDelta,
		nbytes:              int32(b.nbytes),
		epoch:               b.epoch,
		numRecords:          b.NumRecords,
		inTx:                b.inTx,
	}
}

// normalizeTopicName normalizes a topic name for collision detection.
// Kafka considers topics that differ only in . vs _ as colliding.
func normalizeTopicName(t string) string {
	return strings.ReplaceAll(t, ".", "_")
}

func (d *data) mkt(t string, nparts, nreplicas int, configs map[string]*string) {
	if d.tps != nil {
		if _, exists := d.tps[t]; exists {
			panic("should have checked existence already")
		}
	}
	var id uuid
	for {
		id = randUUID()
		if _, exists := d.id2t[id]; !exists {
			break
		}
	}

	if nparts < 0 {
		nparts = d.c.cfg.defaultNumParts
	}
	if nreplicas < 0 {
		nreplicas = min(3, len(d.c.bs)) // cluster default
	}
	d.id2t[id] = t
	d.t2id[t] = id
	d.treplicas[t] = nreplicas
	d.tnorms[normalizeTopicName(t)] = t
	if configs != nil {
		d.tcfgs[t] = configs
	}
	for i := 0; i < nparts; i++ {
		p := int32(i)
		pd := d.tps.mkp(t, p, d.c.newPartData(p))
		pd.t = t
	}
}

func (c *Cluster) noLeader() *broker {
	return &broker{
		c:    c,
		node: -1,
	}
}

func (c *Cluster) newPartData(p int32) func() *partData {
	return func() *partData {
		return &partData{
			p:               p,
			dir:             defLogDir,
			maxTimestampSeg: -1,
			maxTimestampIdx: -1,
			leader:          c.bs[rand.Intn(len(c.bs))],
			watch:           make(map[*watchFetch]struct{}),
			shareWatch:      make(map[*watchShareFetch]struct{}),
			createdAt:       time.Now(),
		}
	}
}

// pushBatch appends a batch to the partition, writing it to the active
// segment file and building a batchMeta index entry. Returns the
// firstOffset assigned to the batch, or -1 if the segment write failed.
//
// If transactional, the producer's PID is registered in uncommittedPIDs
// so the LSO stays at the earliest uncommitted offset.
func (c *Cluster) pushBatch(pd *partData, nbytes int, b kmsg.RecordBatch, inTx bool) int64 {
	maxEarlierTimestamp := b.FirstTimestamp
	if maxEarlierTimestamp < pd.maxFirstTimestamp {
		maxEarlierTimestamp = pd.maxFirstTimestamp
	} else {
		pd.maxFirstTimestamp = maxEarlierTimestamp
	}
	b.FirstOffset = pd.highWatermark
	b.PartitionLeaderEpoch = pd.epoch

	// Build the partBatch for segment encoding
	pb := &partBatch{
		RecordBatch:         b,
		nbytes:              nbytes,
		epoch:               pd.epoch,
		maxEarlierTimestamp: maxEarlierTimestamp,
		inTx:                inTx,
	}

	// Write to segment file and build index entry.
	// persistBatchToSegment creates the segment if needed.
	segPos := c.persistBatchToSegment(pd, pb)
	if segPos < 0 {
		return -1
	}
	active := &pd.segments[len(pd.segments)-1]
	active.index = append(active.index, pb.meta(segPos))
	active.updateEpochRange(pd.epoch)

	// Track max timestamp batch for ListOffsets -3 (KIP-734)
	segIdx := len(pd.segments) - 1
	metaIdx := len(active.index) - 1
	if pd.maxTimestampSeg < 0 || b.MaxTimestamp >= pd.maxTimestampBatch().maxTimestamp {
		pd.maxTimestampSeg = segIdx
		pd.maxTimestampIdx = metaIdx
	}

	firstOffset := b.FirstOffset
	pd.highWatermark += int64(b.NumRecords)
	if inTx {
		if pd.uncommittedPIDs == nil {
			pd.uncommittedPIDs = make(map[int64]int64)
		}
		if existing, ok := pd.uncommittedPIDs[b.ProducerID]; !ok || b.FirstOffset < existing {
			pd.uncommittedPIDs[b.ProducerID] = b.FirstOffset
		}
	}
	if len(pd.uncommittedPIDs) == 0 {
		pd.lastStableOffset += int64(b.NumRecords)
	}
	pd.nbytes += int64(nbytes)
	for w := range pd.watch {
		w.push(pd, nbytes, inTx)
	}
	for w := range pd.shareWatch {
		w.fire()
	}
	return firstOffset
}

// updateEpochRange updates the segment's min/max epoch from a batch epoch.
func (si *segmentInfo) updateEpochRange(epoch int32) {
	if len(si.index) <= 1 {
		si.minEpoch = epoch
		si.maxEpoch = epoch
	} else {
		if epoch < si.minEpoch {
			si.minEpoch = epoch
		}
		if epoch > si.maxEpoch {
			si.maxEpoch = epoch
		}
	}
}

// maxTimestampBatch returns the batchMeta for the max-timestamp batch.
func (pd *partData) maxTimestampBatch() *batchMeta {
	if pd.maxTimestampSeg < 0 {
		return nil
	}
	return &pd.segments[pd.maxTimestampSeg].index[pd.maxTimestampIdx]
}

// rebuildMaxTimestampMeta rebuilds maxTimestampSeg/maxTimestampIdx from the
// batchMeta index. Called after loading segments from disk.
func (pd *partData) rebuildMaxTimestampMeta() {
	pd.maxTimestampSeg = -1
	pd.maxTimestampIdx = -1
	for si := range pd.segments {
		seg := &pd.segments[si]
		for mi := range seg.index {
			m := &seg.index[mi]
			if pd.maxTimestampSeg < 0 || m.maxTimestamp >= pd.maxTimestampBatch().maxTimestamp {
				pd.maxTimestampSeg = si
				pd.maxTimestampIdx = mi
			}
		}
	}
}

// hasBatches returns true if there is at least one batch in any segment.
func (pd *partData) hasBatches() bool {
	for i := range pd.segments {
		if len(pd.segments[i].index) > 0 {
			return true
		}
	}
	return false
}

// hasNBatches returns true if there are at least n batches across all segments.
func (pd *partData) hasNBatches(n int) bool {
	count := 0
	for i := range pd.segments {
		count += len(pd.segments[i].index)
		if count >= n {
			return true
		}
	}
	return false
}

// totalBatches returns the total number of batches across all segments.
func (pd *partData) totalBatches() int {
	n := 0
	for i := range pd.segments {
		n += len(pd.segments[i].index)
	}
	return n
}

// pruneEmptySegments removes segments with no index entries (entirely
// corrupt segment files). Must be called after loading before any code
// that indexes into segment indices.
func (pd *partData) pruneEmptySegments() {
	n := 0
	for i := range pd.segments {
		if len(pd.segments[i].index) > 0 {
			pd.segments[n] = pd.segments[i]
			n++
		}
	}
	pd.segments = pd.segments[:n]
}

// eachBatchMeta calls fn for each batchMeta across all segments.
func (pd *partData) eachBatchMeta(fn func(segIdx, metaIdx int, m *batchMeta) bool) {
	for si := range pd.segments {
		seg := &pd.segments[si]
		for mi := range seg.index {
			if !fn(si, mi, &seg.index[mi]) {
				return
			}
		}
	}
}

// findBatchMeta does a two-level binary search for the first batch where
// field(batch) >= target. The field must be monotonically non-decreasing
// across batches (e.g. epoch, maxTimestamp). Returns (-1, -1, nil) if no
// batch satisfies the condition.
func (pd *partData) findBatchMeta(target int64, field func(*batchMeta) int64) (segIdx, metaIdx int, meta *batchMeta) {
	// Level 1: find first segment whose last batch has field >= target.
	si := sort.Search(len(pd.segments), func(i int) bool {
		seg := &pd.segments[i]
		return len(seg.index) > 0 && field(&seg.index[len(seg.index)-1]) >= target
	})
	if si >= len(pd.segments) {
		return -1, -1, nil
	}
	// Level 2: find first batch in that segment with field >= target.
	seg := &pd.segments[si]
	mi := sort.Search(len(seg.index), func(i int) bool {
		return field(&seg.index[i]) >= target
	})
	if mi >= len(seg.index) {
		return -1, -1, nil
	}
	return si, mi, &seg.index[mi]
}

// searchOffset finds the batch containing offset o in the batchMeta index.
// Uses a two-level binary search: first find the segment (by base offset),
// then search within that segment's index.
//
// segIdx, metaIdx: position of the found batch within pd.segments
// found: if the offset was found
// atEnd: if true, the requested offset is at or past the HWM - "requesting at the end, wait"
func (pd *partData) searchOffset(o int64) (segIdx, metaIdx int, found, atEnd bool) {
	if o < pd.logStartOffset || o > pd.highWatermark {
		return 0, 0, false, false
	}
	if len(pd.segments) == 0 || !pd.hasBatches() {
		return 0, 0, false, o == pd.logStartOffset
	}

	// Check if at the end (at or past last batch's end offset).
	lastSeg := &pd.segments[len(pd.segments)-1]
	if len(lastSeg.index) > 0 {
		lastMeta := &lastSeg.index[len(lastSeg.index)-1]
		if endOff := lastMeta.firstOffset + int64(lastMeta.lastOffsetDelta) + 1; o >= endOff {
			return 0, 0, false, true
		}
	}

	// Level 1: binary search for the segment containing offset o.
	// sort.Search returns the first segment whose base is > o; we want
	// the segment before that (the last segment whose base <= o).
	si := max(sort.Search(len(pd.segments), func(i int) bool {
		return pd.segments[i].base > o
	})-1, 0)

	// Level 2: binary search within the segment's index.
	idx := &pd.segments[si].index
	mi, ok := sort.Find(len(*idx), func(i int) int {
		m := &(*idx)[i]
		if o < m.firstOffset {
			return -1
		}
		if o >= m.firstOffset+int64(m.lastOffsetDelta)+1 {
			return 1
		}
		return 0
	})

	// After compaction, offsets may land in gaps between batches.
	// Skip forward to the next available batch in this or later segments.
	if !ok {
		if mi < len(*idx) {
			return si, mi, true, false
		}
		// Past end of this segment's index - try next segment.
		for si++; si < len(pd.segments); si++ {
			if len(pd.segments[si].index) > 0 {
				return si, 0, true, false
			}
		}
		return 0, 0, false, false
	}
	return si, mi, true, false
}

func (c *Cluster) trimLeft(pd *partData) {
	// Trim batchMeta segments, deleting fully-trimmed segment + index files.
	pdir := partDir(c.storageDir, pd.t, pd.p)
outer:
	for len(pd.segments) > 0 {
		seg := &pd.segments[0]
		for len(seg.index) > 0 {
			m := &seg.index[0]
			finRec := m.firstOffset + int64(m.lastOffsetDelta)
			if finRec >= pd.logStartOffset {
				break outer
			}
			pd.nbytes -= int64(m.nbytes)
			seg.index = seg.index[1:]
		}
		if seg.readFile != nil {
			seg.readFile.Close()
		}
		c.fs.Remove(filepath.Join(pdir, segmentFileName(seg.base)))
		c.fs.Remove(filepath.Join(pdir, indexFileName(seg.base)))
		pd.segments = pd.segments[1:]
	}
	// If all segments were trimmed, close active handles
	// so the next produce creates fresh files.
	if len(pd.segments) == 0 {
		pd.closeActiveFiles(false)
	}
	pd.rebuildMaxTimestampMeta()
	pd.trimAbortedTxns()
}

func (pd *partData) closeActiveFiles(doSync bool) {
	if pd.activeSegFile != nil {
		if doSync {
			pd.activeSegFile.Sync()
		}
		pd.activeSegFile.Close()
		pd.activeSegFile = nil
	}
	if pd.activeIdxFile != nil {
		if doSync {
			pd.activeIdxFile.Sync()
		}
		pd.activeIdxFile.Close()
		pd.activeIdxFile = nil
	}
}

// closeAllFiles closes active write handles and all cached read handles.
// Used on shutdown and topic deletion.
func (pd *partData) closeAllFiles(doSync bool) {
	pd.closeActiveFiles(doSync)
	for i := range pd.segments {
		if pd.segments[i].readFile != nil {
			pd.segments[i].readFile.Close()
			pd.segments[i].readFile = nil
		}
	}
}

// recalculateLSO recalculates the last stable offset from uncommittedPIDs.
// LSO = min of all uncommittedPIDs values, or HWM if empty.
func (pd *partData) recalculateLSO() {
	if len(pd.uncommittedPIDs) == 0 {
		pd.lastStableOffset = pd.highWatermark
		return
	}
	lso := pd.highWatermark
	for _, off := range pd.uncommittedPIDs {
		if off < lso {
			lso = off
		}
	}
	pd.lastStableOffset = lso
}

/////////////
// CONFIGS //
/////////////

// brokerConfigs calls fn for all:
//   - static broker configs (read only)
//   - default configs
//   - dynamic broker configs
func (c *Cluster) brokerConfigs(node int32, fn func(k string, v *string, src kmsg.ConfigSource, sensitive bool)) {
	if node >= 0 {
		for _, b := range c.bs {
			if b.node == node {
				id := strconv.Itoa(int(node))
				fn("broker.id", &id, kmsg.ConfigSourceStaticBrokerConfig, false)
				break
			}
		}
	}
	for _, c := range []struct {
		k    string
		v    string
		sens bool
	}{
		{k: "broker.rack", v: "krack"},
		{k: "sasl.enabled.mechanisms", v: "PLAIN,SCRAM-SHA-256,SCRAM-SHA-512"},
		{k: "super.users", sens: true},
	} {
		v := c.v
		fn(c.k, &v, kmsg.ConfigSourceStaticBrokerConfig, c.sens)
	}

	for k, v := range configDefaults {
		if _, ok := validBrokerConfigs[k]; ok {
			v := v
			fn(k, &v, kmsg.ConfigSourceDefaultConfig, false)
		}
	}

	for k, v := range c.loadBcfgs() {
		fn(k, v, kmsg.ConfigSourceDynamicBrokerConfig, false)
	}
}

// configs calls fn for all
//   - static broker configs (read only)
//   - default configs
//   - dynamic broker configs
//   - dynamic topic configs
//
// This differs from brokerConfigs by also including dynamic topic configs.
func (d *data) configs(t string, fn func(k string, v *string, src kmsg.ConfigSource, sensitive bool)) {
	for k, v := range configDefaults {
		if _, ok := validTopicConfigs[k]; ok {
			v := v
			fn(k, &v, kmsg.ConfigSourceDefaultConfig, false)
		}
	}
	for k, v := range d.c.loadBcfgs() {
		if topicEquiv, ok := validBrokerConfigs[k]; ok && topicEquiv != "" {
			fn(k, v, kmsg.ConfigSourceDynamicBrokerConfig, false)
		}
	}
	for k, v := range d.tcfgs[t] {
		fn(k, v, kmsg.ConfigSourceDynamicTopicConfig, false)
	}
}

func (c *Cluster) loadBcfgs() map[string]*string {
	return *c.bcfgs.Load()
}

func (c *Cluster) storeBcfgs(m map[string]*string) {
	c.bcfgs.Store(&m)
}

// configListAppend appends val to a comma-separated list config value.
func configListAppend(current, val *string) *string {
	if val == nil {
		return current
	}
	if current == nil || *current == "" {
		return val
	}
	s := *current + "," + *val
	return &s
}

// configListSubtract removes val from a comma-separated list config value.
func configListSubtract(current, val *string) *string {
	if val == nil || current == nil {
		return current
	}
	parts := strings.Split(*current, ",")
	out := parts[:0]
	for _, p := range parts {
		if p != *val {
			out = append(out, p)
		}
	}
	s := strings.Join(out, ",")
	return &s
}

// isListConfig returns whether the named config is a list type.
func isListConfig(name string) bool {
	ct, ok := configTypes[name]
	return ok && ct == kmsg.ConfigTypeList
}

// validateBrokerConfig returns whether v is a valid value for broker config k.
func validateBrokerConfig(k string, v *string) bool {
	if v == nil {
		return true
	}
	if ct, ok := configTypes[k]; ok {
		switch ct {
		case kmsg.ConfigTypeInt, kmsg.ConfigTypeLong:
			if _, err := strconv.ParseInt(*v, 10, 64); err != nil {
				return false
			}
		default: // Boolean, String, List, etc. accept any string value
		}
	}
	return true
}

func (d *data) setTopicConfig(t, k string, v *string, dry bool) bool {
	if dry {
		return true
	}
	if _, ok := d.tcfgs[t]; !ok {
		d.tcfgs[t] = make(map[string]*string)
	}
	d.tcfgs[t][k] = v
	return true
}

// All valid topic configs we support, as well as the equivalent broker
// config if there is one.
var validTopicConfigs = map[string]string{
	"cleanup.policy":         "",
	"compression.type":       "compression.type",
	"delete.retention.ms":    "",
	"kfake.is_internal":      "",
	"max.message.bytes":      "log.message.max.bytes",
	"message.timestamp.type": "log.message.timestamp.type",
	"min.insync.replicas":    "min.insync.replicas",
	"retention.bytes":        "log.retention.bytes",
	"retention.ms":           "log.retention.ms",
	"segment.bytes":          "log.segment.bytes",
	"segment.ms":             "log.roll.ms",
}

// All valid broker configs we support, as well as their equivalent
// topic config if there is one.
var validBrokerConfigs = map[string]string{
	"broker.id":                  "",
	"broker.rack":                "",
	"compression.type":           "compression.type",
	"default.replication.factor": "",
	"fetch.max.bytes":            "",
	"max.incremental.fetch.session.cache.slots": "",
	"group.consumer.heartbeat.interval.ms":      "",
	"group.consumer.session.timeout.ms":         "",
	"group.max.size":                            "",
	"group.min.session.timeout.ms":              "",
	"group.max.session.timeout.ms":              "",
	"log.cleaner.backoff.ms":                    "",
	"log.dir":                                   "",
	"log.message.timestamp.type":                "message.timestamp.type",
	"transaction.max.timeout.ms":                "",
	"transactional.id.expiration.ms":            "",
	"log.retention.bytes":                       "retention.bytes",
	"log.retention.ms":                          "retention.ms",
	"log.segment.bytes":                         "segment.bytes",
	"log.roll.ms":                               "segment.ms",
	"message.max.bytes":                         "max.message.bytes",
	"min.insync.replicas":                       "min.insync.replicas",
	"offsets.retention.minutes":                 "",
	"offset.retention.ms":                       "",
	"offsets.retention.check.interval.ms":       "",
	"sasl.enabled.mechanisms":                   "",
	"group.share.heartbeat.interval.ms":         "",
	"group.share.session.timeout.ms":            "",
	"group.share.delivery.count.limit":          "",
	"group.share.record.lock.duration.ms":       "",
	"share.record.lock.sweep.interval.ms":       "",
	"group.share.partition.max.record.locks":    "",
	"group.share.max.share.sessions":            "",
	"group.share.max.size":                      "",
	"state.log.compact.bytes":                   "",
	"super.users":                               "",
}

// validGroupConfigs is the set of per-group configs accepted by
// IncrementalAlterConfigs with GROUP resource type. These are the
// UNPREFIXED names -- the "group." prefix belongs to the broker-level
// defaults in validBrokerConfigs. Real Kafka returns INVALID_CONFIG
// for unknown names here.
var validGroupConfigs = map[string]bool{
	"consumer.session.timeout.ms":      true,
	"consumer.heartbeat.interval.ms":   true,
	"share.auto.offset.reset":          true,
	"share.isolation.level":            true,
	"share.record.lock.duration.ms":    true,
	"share.delivery.count.limit":       true,
	"share.session.timeout.ms":         true,
	"share.heartbeat.interval.ms":      true,
	"share.partition.max.record.locks": true,
	"share.max.share.sessions":         true,
	"share.max.size":                   true,
}

const (
	defLogDir          = "/mem/kfake"
	defMaxMessageBytes = 1048588
)

// defHeartbeatInterval is the default group.consumer.heartbeat.interval.ms.
// Real Kafka defaults to 5s; in test binaries we use 100ms so that
// KIP-848 reconciliation completes quickly.
var defHeartbeatInterval = 5000

// defSessionTimeout is the default group.consumer.session.timeout.ms.
var defSessionTimeout = 45000

// Default topic and broker configs. Topic/broker pairs that share the same
// underlying setting (e.g. max.message.bytes / message.max.bytes) both
// appear here so that DescribeConfigs returns the correct default for
// whichever name is queried.
var configDefaults = map[string]string{
	"cleanup.policy":         "delete",
	"compression.type":       "producer",
	"delete.retention.ms":    "86400000",
	"max.message.bytes":      strconv.Itoa(defMaxMessageBytes),
	"message.timestamp.type": "CreateTime",
	"min.insync.replicas":    "1",
	"retention.bytes":        "-1",
	"retention.ms":           "604800000",

	"segment.bytes": strconv.Itoa(100 << 20),
	"segment.ms":    "604800000",

	"transaction.max.timeout.ms":     "900000",
	"transactional.id.expiration.ms": "604800000",

	"state.log.compact.bytes": "10485760",

	"default.replication.factor":                "3",
	"fetch.max.bytes":                           "57671680",
	"log.cleaner.backoff.ms":                    "3600000",
	"max.incremental.fetch.session.cache.slots": strconv.Itoa(defMaxFetchSessionCacheSlots),
	"group.consumer.heartbeat.interval.ms":      strconv.Itoa(defHeartbeatInterval),
	"group.consumer.session.timeout.ms":         strconv.Itoa(defSessionTimeout),
	"group.max.size":                            "2147483647",
	"group.min.session.timeout.ms":              "6000",
	"group.max.session.timeout.ms":              "300000",
	"log.dir":                                   defLogDir,
	"log.segment.bytes":                         strconv.Itoa(100 << 20),
	"log.roll.ms":                               "604800000",
	"log.message.timestamp.type":                "CreateTime",
	"log.retention.bytes":                       "-1",
	"log.retention.ms":                          "604800000",
	"message.max.bytes":                         strconv.Itoa(defMaxMessageBytes),
	"offsets.retention.minutes":                 "10080",
	"offsets.retention.check.interval.ms":       "600000",
	"group.share.heartbeat.interval.ms":         strconv.Itoa(defHeartbeatInterval),
	"group.share.session.timeout.ms":            "45000",
	"group.share.delivery.count.limit":          "5",
	"group.share.record.lock.duration.ms":       "30000",
	"share.record.lock.sweep.interval.ms":       "5000",
	"group.share.partition.max.record.locks":    "2000",
	"group.share.max.share.sessions":            "2000",
	"group.share.max.size":                      "200",
}

// configTypes maps config names to their data types for DescribeConfigs v3+.
var configTypes = map[string]kmsg.ConfigType{
	"broker.id":                  kmsg.ConfigTypeInt,
	"broker.rack":                kmsg.ConfigTypeString,
	"cleanup.policy":             kmsg.ConfigTypeList,
	"compression.type":           kmsg.ConfigTypeString,
	"default.replication.factor": kmsg.ConfigTypeInt,
	"delete.retention.ms":        kmsg.ConfigTypeLong,
	"fetch.max.bytes":            kmsg.ConfigTypeInt,
	"max.incremental.fetch.session.cache.slots": kmsg.ConfigTypeInt,
	"group.consumer.heartbeat.interval.ms":      kmsg.ConfigTypeInt,
	"group.consumer.session.timeout.ms":         kmsg.ConfigTypeInt,
	"group.max.size":                            kmsg.ConfigTypeInt,
	"group.min.session.timeout.ms":              kmsg.ConfigTypeInt,
	"group.max.session.timeout.ms":              kmsg.ConfigTypeInt,
	"log.cleaner.backoff.ms":                    kmsg.ConfigTypeLong,
	"log.dir":                                   kmsg.ConfigTypeString,
	"log.segment.bytes":                         kmsg.ConfigTypeInt,
	"log.roll.ms":                               kmsg.ConfigTypeLong,
	"log.message.timestamp.type":                kmsg.ConfigTypeString,
	"log.retention.bytes":                       kmsg.ConfigTypeLong,
	"log.retention.ms":                          kmsg.ConfigTypeLong,
	"max.message.bytes":                         kmsg.ConfigTypeInt,
	"message.max.bytes":                         kmsg.ConfigTypeInt,
	"message.timestamp.type":                    kmsg.ConfigTypeString,
	"min.insync.replicas":                       kmsg.ConfigTypeInt,
	"offset.retention.ms":                       kmsg.ConfigTypeLong,
	"offsets.retention.minutes":                 kmsg.ConfigTypeInt,
	"offsets.retention.check.interval.ms":       kmsg.ConfigTypeLong,
	"retention.bytes":                           kmsg.ConfigTypeLong,
	"retention.ms":                              kmsg.ConfigTypeLong,
	"segment.bytes":                             kmsg.ConfigTypeInt,
	"segment.ms":                                kmsg.ConfigTypeLong,
	"sasl.enabled.mechanisms":                   kmsg.ConfigTypeList,
	"group.share.heartbeat.interval.ms":         kmsg.ConfigTypeInt,
	"group.share.session.timeout.ms":            kmsg.ConfigTypeInt,
	"group.share.delivery.count.limit":          kmsg.ConfigTypeInt,
	"group.share.record.lock.duration.ms":       kmsg.ConfigTypeInt,
	"share.record.lock.sweep.interval.ms":       kmsg.ConfigTypeLong,
	"group.share.partition.max.record.locks":    kmsg.ConfigTypeInt,
	"group.share.max.share.sessions":            kmsg.ConfigTypeInt,
	"group.share.max.size":                      kmsg.ConfigTypeInt,
	"state.log.compact.bytes":                   kmsg.ConfigTypeLong,
	"super.users":                               kmsg.ConfigTypeList,
	"transaction.max.timeout.ms":                kmsg.ConfigTypeInt,
	"transactional.id.expiration.ms":            kmsg.ConfigTypeInt,
}

var brokerRack = "krack"

func (c *Cluster) brokerConfigInt(key string, def int) int32 {
	if v, ok := c.loadBcfgs()[key]; ok && v != nil {
		n, _ := strconv.Atoi(*v)
		return int32(n)
	}
	return int32(def)
}

// segmentBytes returns the max segment size for a topic.
func (c *Cluster) segmentBytes(topic string) int64 {
	if tcfg, ok := c.data.tcfgs[topic]; ok {
		if v, ok := tcfg["segment.bytes"]; ok && v != nil {
			if n, err := strconv.ParseInt(*v, 10, 64); err == nil {
				return n
			}
		}
	}
	if v, ok := c.loadBcfgs()["log.segment.bytes"]; ok && v != nil {
		if n, err := strconv.ParseInt(*v, 10, 64); err == nil {
			return n
		}
	}
	return 100 << 20 // 100MiB default
}

func (c *Cluster) consumerHeartbeatIntervalMs() int32 {
	return c.brokerConfigInt("group.consumer.heartbeat.interval.ms", defHeartbeatInterval)
}

func (c *Cluster) consumerSessionTimeoutMs() int32 {
	return c.brokerConfigInt("group.consumer.session.timeout.ms", defSessionTimeout)
}

func (c *Cluster) consumerSessionTimeoutMsForGroup(group string) int32 {
	if v := c.groupConfig(group, "consumer.session.timeout.ms"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return int32(n)
		}
	}
	return c.consumerSessionTimeoutMs()
}

func (c *Cluster) groupMaxSize() int32 {
	return c.brokerConfigInt("group.max.size", 2147483647)
}

func (c *Cluster) groupMinSessionTimeoutMs() int32 {
	return c.brokerConfigInt("group.min.session.timeout.ms", int(c.cfg.minSessionTimeout.Milliseconds()))
}

func (c *Cluster) groupMaxSessionTimeoutMs() int32 {
	return c.brokerConfigInt("group.max.session.timeout.ms", int(c.cfg.maxSessionTimeout.Milliseconds()))
}

const defTransactionMaxTimeoutMs = 900000

func (c *Cluster) transactionMaxTimeoutMs() int32 {
	return c.brokerConfigInt("transaction.max.timeout.ms", defTransactionMaxTimeoutMs)
}

const defTxnIDExpirationMs = 604800000 // 7 days

func (c *Cluster) txnIDExpirationMs() int32 {
	return c.brokerConfigInt("transactional.id.expiration.ms", defTxnIDExpirationMs)
}

func (c *Cluster) fetchSessionCacheSlots() int32 {
	return c.brokerConfigInt("max.incremental.fetch.session.cache.slots", defMaxFetchSessionCacheSlots)
}

const defOffsetsRetentionMinutes = 10080 // 7 days

func (c *Cluster) offsetsRetentionMs() int64 {
	// offset.retention.ms takes precedence when present (kfake extension for testing).
	if v, ok := c.loadBcfgs()["offset.retention.ms"]; ok && v != nil {
		if n, err := strconv.ParseInt(*v, 10, 64); err == nil {
			return n
		}
	}
	return int64(c.brokerConfigInt("offsets.retention.minutes", defOffsetsRetentionMinutes)) * 60 * 1000
}

func (c *Cluster) offsetsRetentionCheckIntervalMs() int64 {
	return int64(c.brokerConfigInt("offsets.retention.check.interval.ms", 600000))
}

func (c *Cluster) shareHeartbeatIntervalMs() int32 {
	return c.brokerConfigInt("group.share.heartbeat.interval.ms", defHeartbeatInterval)
}

func (c *Cluster) shareSessionTimeoutMs() int32 {
	return c.brokerConfigInt("group.share.session.timeout.ms", 45000)
}

func (c *Cluster) shareLockSweepIntervalMs() int32 {
	return c.brokerConfigInt("share.record.lock.sweep.interval.ms", 5000)
}

func (c *Cluster) shareRecordLockDurationMs() int32 {
	return c.brokerConfigInt("group.share.record.lock.duration.ms", 30000)
}

func (c *Cluster) shareMaxDeliveryAttempts() int32 {
	return c.brokerConfigInt("group.share.delivery.count.limit", 5)
}

func (c *Cluster) shareMaxRecordLocks() int32 {
	return c.brokerConfigInt("group.share.partition.max.record.locks", 2000)
}

func (c *Cluster) shareMaxSessions() int32 {
	return c.brokerConfigInt("group.share.max.share.sessions", 2000)
}

func (c *Cluster) shareMaxGroupSize() int32 {
	return c.brokerConfigInt("group.share.max.size", 200)
}

// groupConfig returns a per-group config value, or "" if the group or key
// is not configured. Must only be called from run() where c.groupConfigs
// is safe to read.
func (c *Cluster) groupConfig(group, key string) string {
	gc := c.groupConfigs[group]
	if gc == nil {
		return ""
	}
	v := gc[key]
	if v == nil {
		return ""
	}
	return *v
}

// maxMessageBytes returns the max.message.bytes for a topic, falling back to
// broker config then defaults.
func (d *data) maxMessageBytes(t string) int {
	if tcfg, ok := d.tcfgs[t]; ok {
		if v, ok := tcfg["max.message.bytes"]; ok && v != nil {
			n, _ := strconv.Atoi(*v)
			return n
		}
	}
	if v, ok := d.c.loadBcfgs()["message.max.bytes"]; ok && v != nil {
		n, _ := strconv.Atoi(*v)
		return n
	}
	return defMaxMessageBytes
}

// retentionMs returns the retention.ms for a topic, falling back to
// broker config then defaults.
func (d *data) retentionMs(t string) int64 {
	if tcfg, ok := d.tcfgs[t]; ok {
		if v, ok := tcfg["retention.ms"]; ok && v != nil {
			n, _ := strconv.ParseInt(*v, 10, 64)
			return n
		}
	}
	if v, ok := d.c.loadBcfgs()["log.retention.ms"]; ok && v != nil {
		n, _ := strconv.ParseInt(*v, 10, 64)
		return n
	}
	return 604800000
}

// retentionBytes returns the retention.bytes for a topic, falling back to
// broker config then defaults. -1 means no limit.
func (d *data) retentionBytes(t string) int64 {
	if tcfg, ok := d.tcfgs[t]; ok {
		if v, ok := tcfg["retention.bytes"]; ok && v != nil {
			n, _ := strconv.ParseInt(*v, 10, 64)
			return n
		}
	}
	if v, ok := d.c.loadBcfgs()["log.retention.bytes"]; ok && v != nil {
		n, _ := strconv.ParseInt(*v, 10, 64)
		return n
	}
	return -1
}

func forEachBatchRecord(batch kmsg.RecordBatch, cb func(kmsg.Record) error) error {
	records, err := kgo.DefaultDecompressor().Decompress(
		batch.Records,
		kgo.CompressionCodecType(batch.Attributes&0x0007),
	)
	if err != nil {
		return err
	}
	for range batch.NumRecords {
		rec := kmsg.NewRecord()
		err := rec.ReadFrom(records)
		if err != nil {
			return fmt.Errorf("corrupt batch: %w", err)
		}
		if err := cb(rec); err != nil {
			return err
		}
		length, amt := binary.Varint(records)
		records = records[length+int64(amt):]
	}
	if len(records) > 0 {
		return fmt.Errorf("corrupt batch, extra left over bytes after parsing batch: %v", len(records))
	}
	return nil
}

// BatchRecords returns the raw kmsg.Record's within a record batch, or an error
// if they could not be processed.
func BatchRecords(b kmsg.RecordBatch) ([]kmsg.Record, error) {
	var rs []kmsg.Record
	err := forEachBatchRecord(b, func(r kmsg.Record) error {
		rs = append(rs, r)
		return nil
	})
	return rs, err
}

/////////////////
// COMPACTION  //
/////////////////

// hasRetentionConfig returns true if the topic has retention.ms or
// retention.bytes explicitly set in its topic configs.
func (d *data) hasRetentionConfig(t string) bool {
	tcfg, ok := d.tcfgs[t]
	if !ok {
		return false
	}
	_, hasMs := tcfg["retention.ms"]
	_, hasBytes := tcfg["retention.bytes"]
	return hasMs || hasBytes
}

func (d *data) isCompactTopic(t string) bool {
	if tcfg, ok := d.tcfgs[t]; ok {
		if v, ok := tcfg["cleanup.policy"]; ok && v != nil {
			return strings.Contains(*v, "compact")
		}
	}
	return false
}

// isOffsetAborted returns true if the given offset (identified by its batch's
// firstOffset and producerID) belongs to an aborted transaction.
// abortedTxns is sorted by lastOffset, so we binary search to skip entries
// that end before this batch.
func (pd *partData) isOffsetAborted(firstOffset, producerID int64) bool {
	j := sort.Search(len(pd.abortedTxns), func(i int) bool {
		return pd.abortedTxns[i].lastOffset >= firstOffset
	})
	for ; j < len(pd.abortedTxns); j++ {
		a := pd.abortedTxns[j]
		if a.producerID == producerID && firstOffset >= a.firstOffset {
			return true
		}
	}
	return false
}

// isBatchAborted returns true if the batch belongs to an aborted transaction.
func (pd *partData) isBatchAborted(batch *partBatch) bool {
	return pd.isOffsetAborted(batch.FirstOffset, batch.ProducerID)
}

// compact removes superseded records from a compactable partition.
// The last batch is treated as the active segment and is never compacted.
func (c *Cluster) compact(pd *partData, topic string) {
	if !pd.hasNBatches(2) { // need at least 2 batches: compaction keeps the last batch unconditionally
		return
	}
	total := pd.totalBatches()

	// Read delete.retention.ms from topic config, falling back to default.
	deleteRetentionMs := int64(86400000)
	if tcfg, ok := c.data.tcfgs[topic]; ok {
		if v, ok := tcfg["delete.retention.ms"]; ok && v != nil {
			if n, err := strconv.ParseInt(*v, 10, 64); err == nil {
				deleteRetentionMs = n
			}
		}
	}
	now := time.Now().UnixMilli()

	// Cleanable range is all batches except the last (active) batch.
	// We stream batches from disk in each pass to avoid holding all
	// decoded batches in memory at once.
	cleanableEnd := total - 1

	// Pass 1: stream batches to build key => highest offset map.
	keyOffsets := make(map[string]int64)
	n := 0
	ok := true
	pd.eachBatchMeta(func(si, _ int, m *batchMeta) bool {
		if n >= cleanableEnd {
			return false
		}
		n++
		batch, err := c.readBatchFull(pd, si, m)
		if err != nil {
			c.cfg.logger.Logf(LogLevelWarn, "compact %s-%d: read batch pass 1: %v", pd.t, pd.p, err)
			ok = false
			return false
		}
		if batch.Attributes&0x0020 != 0 || pd.isBatchAborted(batch) {
			return true
		}
		_ = forEachBatchRecord(batch.RecordBatch, func(rec kmsg.Record) error {
			if rec.Key == nil {
				return nil
			}
			absOffset := batch.FirstOffset + int64(rec.OffsetDelta)
			k := string(rec.Key)
			if prev, exists := keyOffsets[k]; !exists || absOffset > prev {
				keyOffsets[k] = absOffset
			}
			return nil
		})
		return true
	})
	if !ok {
		return
	}

	// Pass 2: stream batches again, filtering and keeping survivors.
	// Also track which PIDs still have surviving data batches.
	survivingPIDs := make(map[int64]bool)
	var kept []*partBatch
	n = 0
	pd.eachBatchMeta(func(si, _ int, m *batchMeta) bool {
		if n >= cleanableEnd {
			return false
		}
		n++
		batch, err := c.readBatchFull(pd, si, m)
		if err != nil {
			c.cfg.logger.Logf(LogLevelWarn, "compact %s-%d: read batch pass 2: %v", pd.t, pd.p, err)
			ok = false
			return false
		}
		if batch.Attributes&0x0020 != 0 {
			// Stash control batches for pass 3 filtering.
			kept = append(kept, batch)
			return true
		}
		if pd.isBatchAborted(batch) {
			return true
		}

		var surviving []kmsg.Record
		_ = forEachBatchRecord(batch.RecordBatch, func(rec kmsg.Record) error {
			absOffset := batch.FirstOffset + int64(rec.OffsetDelta)

			if rec.Key == nil {
				return nil
			}

			// Drop superseded records (a later record has the same key).
			if keyOffsets[string(rec.Key)] > absOffset {
				return nil
			}

			// Drop expired tombstones (nil value).
			if rec.Value == nil {
				recTs := batch.FirstTimestamp + int64(rec.TimestampDelta)
				if now-recTs >= deleteRetentionMs {
					return nil
				}
			}

			surviving = append(surviving, rec)
			return nil
		})

		if len(surviving) == 0 {
			return true
		}

		survivingPIDs[batch.ProducerID] = true
		if len(surviving) == int(batch.NumRecords) {
			kept = append(kept, batch)
		} else {
			kept = append(kept, rebuildBatch(batch, surviving))
		}
		return true
	})
	if !ok {
		return
	}

	// Pass 3: filter control batches - keep only if PID has surviving data.
	// Control batches are functionally inert in kfake (read_committed is
	// driven by pd.abortedTxns and LSO, not control batches), but we
	// match real Kafka's compaction behavior for fidelity.
	filtered := kept[:0]
	for _, batch := range kept {
		isControl := batch.Attributes&0x0020 != 0
		if isControl && !survivingPIDs[batch.ProducerID] {
			continue
		}
		filtered = append(filtered, batch)
	}
	kept = filtered

	// Read the last (active) batch - always kept unconditionally.
	lastSeg := &pd.segments[len(pd.segments)-1]
	lastIdx := len(lastSeg.index) - 1
	lastBatch, err := c.readBatchFull(pd, len(pd.segments)-1, &lastSeg.index[lastIdx])
	if err != nil {
		c.cfg.logger.Logf(LogLevelWarn, "compact %s-%d: read last batch: %v", pd.t, pd.p, err)
		return
	}
	kept = append(kept, lastBatch)

	// Rebuild partition metadata.
	pd.nbytes = 0
	for _, b := range kept {
		pd.nbytes += int64(b.nbytes)
	}
	if len(kept) > 0 {
		pd.logStartOffset = kept[0].FirstOffset
	}

	// Rebuild segments and batchMeta from compacted batches.
	c.rebuildSegments(pd, kept)

	// Prune aborted txn entries that are now before logStartOffset.
	pd.trimAbortedTxns()
}

// trimAbortedTxns prunes aborted txn entries fully before logStartOffset.
func (pd *partData) trimAbortedTxns() {
	i := sort.Search(len(pd.abortedTxns), func(i int) bool {
		return pd.abortedTxns[i].lastOffset >= pd.logStartOffset
	})
	pd.abortedTxns = pd.abortedTxns[i:]
}

/////////////////
// RETENTION   //
/////////////////

// applyRetention advances logStartOffset past batches that are expired by
// retention.ms or that exceed retention.bytes, then trims them.
func (c *Cluster) applyRetention(pd *partData, topic string) {
	if !pd.hasBatches() {
		return
	}

	retMs := c.data.retentionMs(topic)
	retBytes := c.data.retentionBytes(topic)
	if retMs < 0 && retBytes < 0 {
		return
	}

	now := time.Now().UnixMilli()
	newLogStart := pd.logStartOffset

	// Time-based retention: drop batches whose max timestamp is older
	// than retention.ms.
	if retMs >= 0 {
		pd.eachBatchMeta(func(_, _ int, m *batchMeta) bool {
			if now-m.maxTimestamp >= retMs {
				end := m.firstOffset + int64(m.lastOffsetDelta) + 1
				if end > newLogStart {
					newLogStart = end
				}
			}
			return true
		})
	}

	// Size-based retention: drop oldest batches until total size is
	// within retention.bytes.
	if retBytes >= 0 && pd.nbytes > retBytes {
		excess := pd.nbytes - retBytes
		pd.eachBatchMeta(func(_, _ int, m *batchMeta) bool {
			if excess <= 0 {
				return false
			}
			end := m.firstOffset + int64(m.lastOffsetDelta) + 1
			if end > newLogStart {
				newLogStart = end
			}
			excess -= int64(m.nbytes)
			return true
		})
	}

	if newLogStart > pd.logStartOffset {
		pd.logStartOffset = newLogStart
		c.trimLeft(pd)
	}
}

// rebuildBatch creates a new partBatch with only the kept records,
// re-encoding them uncompressed with updated batch metadata.
func rebuildBatch(orig *partBatch, kept []kmsg.Record) *partBatch {
	// Re-encode records uncompressed.
	var rawRecords []byte
	for _, rec := range kept {
		rawRecords = rec.AppendTo(rawRecords)
	}

	b := kmsg.RecordBatch{
		FirstOffset:          orig.FirstOffset,
		PartitionLeaderEpoch: orig.PartitionLeaderEpoch,
		Magic:                2,
		Attributes:           orig.Attributes &^ 0x0007, // clear compression bits (uncompressed)
		LastOffsetDelta:      kept[len(kept)-1].OffsetDelta,
		FirstTimestamp:       orig.FirstTimestamp,
		MaxTimestamp:         orig.FirstTimestamp,
		ProducerID:           orig.ProducerID,
		ProducerEpoch:        orig.ProducerEpoch,
		FirstSequence:        orig.FirstSequence,
		NumRecords:           int32(len(kept)),
		Records:              rawRecords,
	}

	// Recompute timestamps.
	for _, rec := range kept {
		ts := orig.FirstTimestamp + int64(rec.TimestampDelta)
		if ts > b.MaxTimestamp {
			b.MaxTimestamp = ts
		}
	}

	benc := b.AppendTo(nil)
	b.Length = int32(len(benc) - 12)
	b.CRC = int32(crc32.Checksum(benc[21:], crc32c))

	return &partBatch{
		RecordBatch:         b,
		nbytes:              len(benc),
		epoch:               orig.epoch,
		maxEarlierTimestamp: orig.maxEarlierTimestamp,
	}
}
