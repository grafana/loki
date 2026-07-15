package kfake

import (
	"cmp"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"maps"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/twmb/franz-go/pkg/kmsg"
)

// FORMAT MIGRATION GUIDE
//
// Each file/entry embeds its own version. We support current version
// (currentPersistVersion) and one version back (N-1). On save, we
// always write currentPersistVersion.
//
// Append log entries are version-tagged per-entry, so old entries
// remain readable in-place. No need to rewrite large files on upgrade.
// New entries are appended at the current version.
//
// To evolve the format (bump from version N to N+1):
//
//   1. Bump currentPersistVersion from N to N+1.
//
//   2. For each type whose format changed, copy current type to
//      a versioned name (persistTopics -> persistTopicsVN), then
//      modify persistTopics for the new shape.
//
//   3. Write migration function: migrateTopicsVN(old) -> new
//
//   4. For JSON files: update loader to check version, decode old
//      type if version == N, call migration.
//      For append log entries: update entry decoder to handle both
//      version N and N+1 entries in the same file.
//
//   5. Delete v(N-1) migration code and types. Only keep one old
//      version. Users on v(N-1) must first load+save with v(N)
//      code, then upgrade to v(N+1).
//
//   6. Unchanged types need no migration.
//
// Segment files (.dat) use raw Kafka RecordBatch wire format and are
// version-independent. Index files (.idx) have a per-entry version
// byte, so .idx format changes follow the per-entry versioning
// pattern (step 4 above) without requiring segment file changes.

const currentPersistVersion = 1

// entryHeader is the framing for all append log entries.
// Format: [4 bytes length][4 bytes CRC32][2 bytes version][N bytes data]
// All multi-byte integers are little-endian.
const entryHeaderSize = 10 // 4 (length) + 4 (CRC) + 2 (version)

// writeEntry frames data with length+CRC+version and writes it to f
// in a single write call, then optionally fsyncs.
func writeEntry(f file, data []byte, syncW bool) error {
	bp := batchPool.Get().(*[]byte)
	buf := slices.Grow((*bp)[:0], entryHeaderSize+len(data))[:entryHeaderSize+len(data)]
	copy(buf[entryHeaderSize:], data)

	// length = len(version) + len(data)
	length := uint32(2 + len(data))
	binary.LittleEndian.PutUint32(buf[0:4], length)
	// CRC covers version + data (buf[8:] = version ++ data)
	binary.LittleEndian.PutUint16(buf[8:10], currentPersistVersion)
	binary.LittleEndian.PutUint32(buf[4:8], crc32.Checksum(buf[8:], crc32c))

	_, err := f.Write(buf)
	*bp = buf[:0]
	batchPool.Put(bp)
	if err != nil {
		return err
	}
	if syncW {
		return f.Sync()
	}
	return nil
}

// readEntries reads all valid framed entries from raw bytes.
// It returns the entries and the number of valid bytes consumed.
// Any trailing corrupt or partial entry is ignored (truncation point).
func readEntries(raw []byte) (entries []entryData, validBytes int) {
	pos := 0
	for pos+entryHeaderSize <= len(raw) {
		length := binary.LittleEndian.Uint32(raw[pos : pos+4])
		if length < 2 { // minimum is 2 bytes for version
			break
		}
		if pos+4+4+int(length) > len(raw) {
			break
		}

		storedCRC := binary.LittleEndian.Uint32(raw[pos+4 : pos+8])
		versionAndData := raw[pos+8 : pos+4+4+int(length)]

		if crc32.Checksum(versionAndData, crc32c) != storedCRC {
			break
		}

		version := binary.LittleEndian.Uint16(versionAndData[0:2])
		data := versionAndData[2:]

		entries = append(entries, entryData{
			version: version,
			data:    data,
		})
		pos += 4 + 4 + int(length)
	}
	return entries, pos
}

type (
	entryData struct {
		version uint16
		data    []byte
	}

	persistMeta struct {
		Version   int    `json:"version"`
		ClusterID string `json:"cluster_id"`
	}

	persistTopics struct {
		Version int            `json:"version"`
		Topics  []persistTopic `json:"topics"`
	}

	persistTopic struct {
		Name       string            `json:"name"`
		ID         uuid              `json:"id"`
		Partitions int               `json:"partitions"`
		Replicas   int               `json:"replicas"`
		Configs    map[string]string `json:"configs,omitempty"`
	}

	persistACLs struct {
		Version int          `json:"version"`
		ACLs    []persistACL `json:"acls"`
	}

	persistACL struct {
		Principal    string `json:"principal"`
		Host         string `json:"host"`
		ResourceType int8   `json:"resource_type"`
		ResourceName string `json:"resource_name"`
		Pattern      int8   `json:"pattern"`
		Operation    int8   `json:"operation"`
		Permission   int8   `json:"permission"`
	}

	persistSASL struct {
		Version  int                     `json:"version"`
		Plain    map[string]string       `json:"plain,omitempty"`
		Scram256 map[string]persistScram `json:"scram256,omitempty"`
		Scram512 map[string]persistScram `json:"scram512,omitempty"`
	}

	persistScram struct {
		Mechanism  string `json:"mechanism"`
		Salt       []byte `json:"salt"`
		SaltedPass []byte `json:"salted_pass"`
		Iterations int    `json:"iterations"`
	}

	persistBrokerConfigs struct {
		Version int               `json:"version"`
		Configs map[string]string `json:"configs"`
	}

	persistQuotas struct {
		Version int            `json:"version"`
		Quotas  []persistQuota `json:"quotas"`
	}

	persistQuota struct {
		Entity []persistQuotaEntityComponent `json:"entity"`
		Values map[string]float64            `json:"values"`
	}

	persistQuotaEntityComponent struct {
		Type string  `json:"type"`
		Name *string `json:"name"`
	}

	persistSeqWindows struct {
		Version int                     `json:"version"`
		Windows []persistSeqWindowEntry `json:"windows"`
	}

	persistSeqWindowEntry struct {
		PID   int64  `json:"pid"`
		Topic string `json:"topic"`
		Part  int32  `json:"partition"`

		// v2 format: independent entries.
		Entries [5]persistPidEntry `json:"entries,omitempty"`
		Count   uint8              `json:"count,omitempty"`

		// v1 format (legacy): overlapping-pair circular buffer.
		// Kept for backwards-compatible loading only.
		Seq     [5]int32 `json:"seq,omitempty"`
		Offsets [5]int64 `json:"offsets,omitempty"`

		At      uint8 `json:"at"`
		Epoch   int16 `json:"epoch"`
		Seen    bool  `json:"seen,omitempty"`
		NextSeq int32 `json:"next_seq,omitempty"`
	}

	persistPidEntry struct {
		FirstSeq int32 `json:"first_seq"`
		NextSeq  int32 `json:"next_seq"`
		Offset   int64 `json:"offset"`
	}

	persistPartSnapshot struct {
		Version          int                  `json:"version"`
		HighWatermark    int64                `json:"high_watermark"`
		LastStableOffset int64                `json:"last_stable_offset"`
		LogStartOffset   int64                `json:"log_start_offset"`
		Epoch            int32                `json:"epoch"`
		LeaderNode       int32                `json:"leader_node,omitempty"`
		MaxTimestamp     int64                `json:"max_timestamp"`
		CreatedAt        time.Time            `json:"created_at"`
		AbortedTxns      []persistAbortedTxn  `json:"aborted_txns,omitempty"`
		Segments         []persistSegmentInfo `json:"segments"`
	}

	persistAbortedTxn struct {
		ProducerID  int64 `json:"producer_id"`
		FirstOffset int64 `json:"first_offset"`
		LastOffset  int64 `json:"last_offset"`
	}

	persistSegmentInfo struct {
		BaseOffset int64 `json:"base_offset"`
		Size       int64 `json:"size"`
		MinEpoch   int32 `json:"min_epoch,omitempty"`
		MaxEpoch   int32 `json:"max_epoch,omitempty"`
	}
)

/////////////////////
// SEGMENT + INDEX
/////////////////////

// Segment files (.dat) contain pure RecordBatch wire bytes concatenated.
// Entry boundaries are found via RecordBatch Length (big-endian int32 at byte 8).
// RecordBatch has its own CRC for integrity.
//
// Index files (.idx) sit alongside each segment file and contain per-batch
// metadata as fixed-size 15-byte entries:
//   [1 byte: version]
//   [1 byte: checksum (low byte of crc32c of remaining 13 bytes)]
//   [4 bytes: epoch, little-endian]
//   [8 bytes: maxEarlierTimestamp, little-endian]
//   [1 byte: flags (bit 0 = inTx)]

const indexEntrySize = 15

func indexFileName(baseOffset int64) string {
	return fmt.Sprintf("%d.idx", baseOffset)
}

// batchPool pools []byte buffers for batch encoding (encodeBatch) and
// log entry framing (writeEntry). Used in both osFS and memFS modes -
// memFS copies the buffer into its in-memory map, so the pooled slice
// is safe to reuse after Write returns.
var batchPool = sync.Pool{
	New: func() any {
		b := make([]byte, 0, 256)
		return &b
	},
}

func encodeBatch(b *partBatch) *[]byte {
	bp := batchPool.Get().(*[]byte)
	*bp = b.RecordBatch.AppendTo((*bp)[:0])
	return bp
}

func encodeIndexEntry(epoch int32, maxEarlierTS int64, inTx bool) [indexEntrySize]byte {
	var buf [indexEntrySize]byte
	buf[0] = currentPersistVersion
	binary.LittleEndian.PutUint32(buf[2:6], uint32(epoch))
	binary.LittleEndian.PutUint64(buf[6:14], uint64(maxEarlierTS))
	if inTx {
		buf[14] = 1
	}
	buf[1] = byte(crc32.Checksum(buf[2:], crc32c))
	return buf
}

func decodeIndexEntry(buf []byte) (epoch int32, maxEarlierTS int64, inTx, ok bool) {
	if len(buf) < indexEntrySize {
		return 0, 0, false, false
	}
	if byte(crc32.Checksum(buf[2:indexEntrySize], crc32c)) != buf[1] {
		return 0, 0, false, false
	}
	epoch = int32(binary.LittleEndian.Uint32(buf[2:6]))
	maxEarlierTS = int64(binary.LittleEndian.Uint64(buf[6:14]))
	inTx = buf[14]&1 != 0
	return epoch, maxEarlierTS, inTx, true
}

// decodeBatchRaw validates a RecordBatch CRC and parses it.
func decodeBatchRaw(raw []byte) (*kmsg.RecordBatch, error) {
	if len(raw) >= 21 {
		stored := binary.BigEndian.Uint32(raw[17:21])
		if computed := crc32.Checksum(raw[21:], crc32c); stored != computed {
			return nil, fmt.Errorf("RecordBatch CRC mismatch: stored=%08x computed=%08x", stored, computed)
		}
	}
	var rb kmsg.RecordBatch
	if err := rb.ReadFrom(raw); err != nil {
		return nil, fmt.Errorf("decoding RecordBatch: %w", err)
	}
	return &rb, nil
}

// readBatchRaw reads the raw RecordBatch wire bytes for a batch from its
// segment file. The returned bytes can be appended directly to a fetch
// response. Segment files contain pure RecordBatch bytes.
func (c *Cluster) readBatchRaw(pd *partData, segIdx int, meta *batchMeta) ([]byte, error) {
	if meta.segPos < 0 {
		return nil, fmt.Errorf("batch at offset %d was not persisted", meta.firstOffset)
	}
	f, err := c.segReadFile(pd, segIdx)
	if err != nil {
		return nil, err
	}
	if _, err := f.Seek(meta.segPos, io.SeekStart); err != nil {
		return nil, err
	}
	buf := make([]byte, meta.nbytes)
	if _, err := io.ReadFull(f, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

// segReadFile returns a read handle for the given segment. For the
// active segment (last in pd.segments), it reuses pd.activeSegFile
// which is opened O_RDWR|O_APPEND so it supports both reads and writes.
// For sealed segments, it lazily opens and caches seg.readFile.
// Safe because all I/O runs in the single Cluster.run() goroutine,
// and O_APPEND writes always seek to end regardless of read position.
func (c *Cluster) segReadFile(pd *partData, segIdx int) (file, error) {
	if segIdx == len(pd.segments)-1 && pd.activeSegFile != nil {
		return pd.activeSegFile, nil
	}
	seg := &pd.segments[segIdx]
	if seg.readFile != nil {
		return seg.readFile, nil
	}
	path := filepath.Join(partDir(c.storageDir, pd.t, pd.p), segmentFileName(seg.base))
	f, err := c.fs.OpenFile(path, os.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	seg.readFile = f
	return f, nil
}

// readBatchFull reads and decodes a full partBatch from its segment file.
// Metadata (epoch, maxEarlierTimestamp, inTx) comes from the in-memory
// batchMeta. Used by compaction and ListOffsets timestamp search.
func (c *Cluster) readBatchFull(pd *partData, segIdx int, meta *batchMeta) (*partBatch, error) {
	raw, err := c.readBatchRaw(pd, segIdx, meta)
	if err != nil {
		return nil, err
	}
	rb, err := decodeBatchRaw(raw)
	if err != nil {
		return nil, err
	}
	return &partBatch{
		RecordBatch:         *rb,
		nbytes:              len(raw),
		epoch:               meta.epoch,
		maxEarlierTimestamp: meta.maxEarlierTimestamp,
		inTx:                meta.inTx,
	}, nil
}

//////////////////////////
// APPEND LOG ENTRIES
//////////////////////////

type (
	groupLogEntry struct {
		Type     string  `json:"type"`
		Group    string  `json:"g"`
		Topic    string  `json:"t,omitempty"`
		Part     int32   `json:"p,omitempty"`
		Offset   int64   `json:"o,omitempty"`
		Epoch    int32   `json:"e,omitempty"`
		Metadata *string `json:"m,omitempty"`

		// For commit entries: unix millis of last commit time (KIP-211)
		LastCommit *int64 `json:"lc,omitempty"`

		// For meta entries (classic groups)
		GroupType  string `json:"typ,omitempty"`
		ProtoType  string `json:"pt,omitempty"`
		Protocol   string `json:"pr,omitempty"`
		Generation int32  `json:"gen,omitempty"`

		// For meta848 entries (consumer groups)
		Assignor   string `json:"assignor,omitempty"`
		GroupEpoch int32  `json:"epoch848,omitempty"`

		// For static member entries
		InstanceID string `json:"instance,omitempty"`
		MemberID   string `json:"member,omitempty"`
	}

	pidLogEntry struct {
		Type    string `json:"type"` // init, endtx, timeout
		PID     int64  `json:"pid"`
		Epoch   int16  `json:"epoch"`
		TxID    string `json:"txid,omitempty"`
		Timeout int32  `json:"timeout,omitempty"`
		Commit  *bool  `json:"commit,omitempty"`
	}
)

type (
	glCommitKey struct {
		group, topic string
		part         int32
	}
	glStaticKey struct {
		group, instance string
	}
	glReplay struct {
		metas   map[string][]byte
		commits map[glCommitKey][]byte
		statics map[glStaticKey][]byte
	}
)

// replayGroupsLog replays append log entries keeping the latest per key.
// Used by both loadGroupsLog (startup) and compactGroupsLog (live).
func replayGroupsLog(entries []entryData) glReplay {
	r := glReplay{
		metas:   make(map[string][]byte),
		commits: make(map[glCommitKey][]byte),
		statics: make(map[glStaticKey][]byte),
	}
	for _, e := range entries {
		var entry groupLogEntry
		if err := json.Unmarshal(e.data, &entry); err != nil {
			continue
		}
		switch entry.Type {
		case "commit":
			r.commits[glCommitKey{entry.Group, entry.Topic, entry.Part}] = e.data
		case "delete":
			delete(r.commits, glCommitKey{entry.Group, entry.Topic, entry.Part})
		case "meta", "meta848":
			r.metas[entry.Group] = e.data
		case "static":
			if entry.MemberID == "" {
				delete(r.statics, glStaticKey{entry.Group, entry.InstanceID})
			} else {
				r.statics[glStaticKey{entry.Group, entry.InstanceID}] = e.data
			}
		}
	}
	return r
}

/////////////////////
// WRITE HELPERS
/////////////////////

// writeJSONFile atomically writes v as JSON to path by writing to a
// temporary file, fsyncing, and renaming. This ensures readers never
// see a partially-written file even if the process crashes mid-write.
func writeJSONFile(fsys fs, path string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshaling %s: %w", path, err)
	}
	tmpPath := path + ".tmp"
	f, err := fsys.OpenFile(tmpPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("creating %s: %w", tmpPath, err)
	}
	defer f.Close()
	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("writing %s: %w", tmpPath, err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("syncing %s: %w", tmpPath, err)
	}
	return fsys.Rename(tmpPath, path)
}

// readJSONFile reads and unmarshals a JSON file. Returns false if the
// file does not exist. Mirrors writeJSONFile for save/load symmetry.
func readJSONFile(fsys fs, path string, v any) (bool, error) {
	data, err := fsys.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if err := json.Unmarshal(data, v); err != nil {
		return false, fmt.Errorf("parsing %s: %w", filepath.Base(path), err)
	}
	return true, nil
}

// appendLogEntry encodes v as JSON and appends it as a framed entry.
// Returns the total bytes written (header + data).
func appendLogEntry(f file, v any, syncW bool) (int, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return 0, err
	}
	return entryHeaderSize + len(data), writeEntry(f, data, syncW)
}

// partDir returns the partition directory path, URL-escaping for fs safety.
func partDir(dataDir, topic string, part int32) string {
	escaped := url.PathEscape(topic)
	return filepath.Join(dataDir, "partitions", fmt.Sprintf("%s-%d", escaped, part))
}

// segmentFileName returns the segment file name for a given base offset.
func segmentFileName(baseOffset int64) string {
	return fmt.Sprintf("%d.dat", baseOffset)
}

//////////////////////
// SAVE (SHUTDOWN)
//////////////////////

// saveToDisk writes all cluster state to disk. Called during orderly shutdown.
func (c *Cluster) saveToDisk() error {
	fsys := c.fs
	dir := c.cfg.dataDir

	if err := fsys.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating data dir: %w", err)
	}

	return errors.Join(
		writeJSONFile(fsys, filepath.Join(dir, "meta.json"), persistMeta{
			Version:   currentPersistVersion,
			ClusterID: c.cfg.clusterID,
		}),
		c.saveTopics(fsys, dir),
		c.saveACLs(fsys, dir),
		c.saveSASL(fsys, dir),
		c.saveBrokerConfigs(fsys, dir),
		c.saveQuotas(fsys, dir),
		c.savePartitions(fsys, dir),
		c.saveGroupsLog(fsys, dir),
		c.savePIDsLog(fsys, dir),
		c.saveSeqWindows(fsys, dir),
	)
}

func (c *Cluster) saveTopics(fsys fs, dir string) error {
	var pts []persistTopic
	for t, id := range c.data.t2id {
		var cfgs map[string]string
		for k, v := range c.data.tcfgs[t] {
			if v != nil {
				if cfgs == nil {
					cfgs = make(map[string]string)
				}
				cfgs[k] = *v
			}
		}
		nparts := 0
		if ps, ok := c.data.tps[t]; ok {
			nparts = len(ps)
		}
		pts = append(pts, persistTopic{
			Name:       t,
			ID:         id,
			Partitions: nparts,
			Replicas:   c.data.treplicas[t],
			Configs:    cfgs,
		})
	}
	return writeJSONFile(fsys, filepath.Join(dir, "topics.json"), persistTopics{
		Version: currentPersistVersion,
		Topics:  pts,
	})
}

func (c *Cluster) saveACLs(fsys fs, dir string) error {
	var pa []persistACL
	for _, a := range c.acls.acls {
		pa = append(pa, persistACL{
			Principal:    a.principal,
			Host:         a.host,
			ResourceType: int8(a.resourceType),
			ResourceName: a.resourceName,
			Pattern:      int8(a.pattern),
			Operation:    int8(a.operation),
			Permission:   int8(a.permission),
		})
	}
	return writeJSONFile(fsys, filepath.Join(dir, "acls.json"), persistACLs{
		Version: currentPersistVersion,
		ACLs:    pa,
	})
}

func scramToPersist(m map[string]scramAuth) map[string]persistScram {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]persistScram, len(m))
	for k, v := range m {
		out[k] = persistScram{Mechanism: v.mechanism, Salt: v.salt, SaltedPass: v.saltedPass, Iterations: v.iterations}
	}
	return out
}

func persistToScram(m map[string]persistScram) map[string]scramAuth {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]scramAuth, len(m))
	for k, v := range m {
		out[k] = scramAuth{mechanism: v.Mechanism, salt: v.Salt, saltedPass: v.SaltedPass, iterations: v.Iterations}
	}
	return out
}

func (c *Cluster) saveSASL(fsys fs, dir string) error {
	ps := persistSASL{Version: currentPersistVersion}
	if len(c.sasls.plain) > 0 {
		ps.Plain = maps.Clone(c.sasls.plain)
	}
	ps.Scram256 = scramToPersist(c.sasls.scram256)
	ps.Scram512 = scramToPersist(c.sasls.scram512)
	return writeJSONFile(fsys, filepath.Join(dir, "sasl.json"), ps)
}

func (c *Cluster) saveBrokerConfigs(fsys fs, dir string) error {
	cfgs := make(map[string]string)
	for k, v := range c.loadBcfgs() {
		if v != nil {
			cfgs[k] = *v
		}
	}
	return writeJSONFile(fsys, filepath.Join(dir, "broker_configs.json"), persistBrokerConfigs{
		Version: currentPersistVersion,
		Configs: cfgs,
	})
}

func (c *Cluster) saveQuotas(fsys fs, dir string) error {
	var pqs []persistQuota
	for _, qe := range c.quotas {
		var entity []persistQuotaEntityComponent
		for _, ec := range qe.entity {
			entity = append(entity, persistQuotaEntityComponent{
				Type: ec.entityType,
				Name: ec.name,
			})
		}
		pqs = append(pqs, persistQuota{
			Entity: entity,
			Values: qe.values,
		})
	}
	return writeJSONFile(fsys, filepath.Join(dir, "quotas.json"), persistQuotas{
		Version: currentPersistVersion,
		Quotas:  pqs,
	})
}

// forEachPartition runs fn concurrently on every partition, returning
// the first error encountered.
func (c *Cluster) forEachPartition(fn func(topic string, part int32, pd *partData) error) error {
	type partJob struct {
		topic string
		part  int32
		pd    *partData
	}
	var jobs []partJob
	c.data.tps.each(func(t string, p int32, pd *partData) {
		jobs = append(jobs, partJob{t, p, pd})
	})

	var mu sync.Mutex
	var firstErr error
	setErr := func(err error) {
		mu.Lock()
		if firstErr == nil {
			firstErr = err
		}
		mu.Unlock()
	}

	var wg sync.WaitGroup
	for _, j := range jobs {
		wg.Add(1)
		go func(j partJob) {
			defer wg.Done()
			if err := fn(j.topic, j.part, j.pd); err != nil {
				setErr(fmt.Errorf("partition %s-%d: %w", j.topic, j.part, err))
			}
		}(j)
	}
	wg.Wait()
	return firstErr
}

func (c *Cluster) savePartitions(fsys fs, dir string) error {
	return c.forEachPartition(func(topic string, part int32, pd *partData) error {
		return c.savePartition(fsys, dir, topic, part, pd)
	})
}

func (c *Cluster) savePartition(fsys fs, dir, topic string, part int32, pd *partData) error {
	pdir := partDir(dir, topic, part)
	if err := fsys.MkdirAll(pdir, 0o755); err != nil {
		return err
	}

	pd.closeAllFiles(c.cfg.syncWrites)

	// Build snapshot segment metadata from pd.segments.
	// Segment files are already on disk (written by persistBatchToSegment
	// on every produce, and updated by rebuildSegments after compaction).
	var snapSegments []persistSegmentInfo
	for _, si := range pd.segments {
		psi := persistSegmentInfo{
			BaseOffset: si.base,
			Size:       si.size,
			MinEpoch:   si.minEpoch,
			MaxEpoch:   si.maxEpoch,
		}
		// If size is not tracked (e.g., loaded from older snapshot),
		// stat the file to get it.
		if psi.Size == 0 {
			path := filepath.Join(pdir, segmentFileName(si.base))
			if info, err := fsys.Stat(path); err == nil {
				psi.Size = info.Size()
			}
		}
		snapSegments = append(snapSegments, psi)
	}

	// Write partition snapshot
	var abortedTxns []persistAbortedTxn
	for _, a := range pd.abortedTxns {
		abortedTxns = append(abortedTxns, persistAbortedTxn{
			ProducerID:  a.producerID,
			FirstOffset: a.firstOffset,
			LastOffset:  a.lastOffset,
		})
	}
	snap := persistPartSnapshot{
		Version:          currentPersistVersion,
		HighWatermark:    pd.highWatermark,
		LastStableOffset: pd.lastStableOffset,
		LogStartOffset:   pd.logStartOffset,
		Epoch:            pd.epoch,
		LeaderNode:       pd.leader.node,
		MaxTimestamp:     pd.maxFirstTimestamp,
		CreatedAt:        pd.createdAt,
		AbortedTxns:      abortedTxns,
		Segments:         snapSegments,
	}
	return writeJSONFile(fsys, filepath.Join(pdir, "snapshot.json"), snap)
}

// rebuildSegments deletes existing segment files for a partition and rewrites
// them from the given batches, rebuilding pd.segments with batchMeta. Called
// after compaction changes the batch set. Also resets active file handles.
func (c *Cluster) rebuildSegments(pd *partData, batches []*partBatch) {
	pdir := partDir(c.storageDir, pd.t, pd.p)

	pd.closeAllFiles(false)

	// Delete old segment + index files
	for _, seg := range pd.segments {
		c.fs.Remove(filepath.Join(pdir, segmentFileName(seg.base)))
		c.fs.Remove(filepath.Join(pdir, indexFileName(seg.base)))
	}
	pd.segments = nil

	if len(batches) == 0 {
		pd.rebuildMaxTimestampMeta()
		return
	}

	// Group batches into segments by size
	segMaxBytes := c.segmentBytes(pd.t)
	type segGroup struct {
		base    int64
		batches []*partBatch
		size    int64
	}
	var groups []segGroup
	for _, b := range batches {
		if len(groups) == 0 || groups[len(groups)-1].size >= segMaxBytes {
			groups = append(groups, segGroup{base: b.FirstOffset})
		}
		g := &groups[len(groups)-1]
		g.batches = append(g.batches, b)
		g.size += int64(b.nbytes)
	}

	// Write segment (.dat) and index (.idx) files.
	c.fs.MkdirAll(pdir, 0o755)
	for _, g := range groups {
		si := segmentInfo{base: g.base}
		segPath := filepath.Join(pdir, segmentFileName(g.base))
		idxPath := filepath.Join(pdir, indexFileName(g.base))
		sf, err := c.fs.OpenFile(segPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
		if err != nil {
			c.cfg.logger.Logf(LogLevelWarn, "rebuildSegments %s-%d: create %s: %v", pd.t, pd.p, segPath, err)
			continue
		}
		xf, err := c.fs.OpenFile(idxPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
		if err != nil {
			sf.Close()
			c.cfg.logger.Logf(LogLevelWarn, "rebuildSegments %s-%d: create %s: %v", pd.t, pd.p, idxPath, err)
			continue
		}
		var pos int64
		for _, b := range g.batches {
			bp := encodeBatch(b)
			meta := b.meta(pos)
			_, err := sf.Write(*bp)
			batchSize := int64(len(*bp))
			*bp = (*bp)[:0]
			batchPool.Put(bp)
			if err != nil {
				c.cfg.logger.Logf(LogLevelWarn, "rebuildSegments %s-%d: write: %v", pd.t, pd.p, err)
				break
			}
			idx := encodeIndexEntry(b.epoch, b.maxEarlierTimestamp, b.inTx)
			if _, err := xf.Write(idx[:]); err != nil {
				c.cfg.logger.Logf(LogLevelWarn, "rebuildSegments %s-%d: write index: %v", pd.t, pd.p, err)
				break
			}
			pos += batchSize
			si.size += batchSize
			si.index = append(si.index, meta)
			si.updateEpochRange(b.epoch)
		}
		sf.Sync()
		sf.Close()
		xf.Sync()
		xf.Close()
		pd.segments = append(pd.segments, si)
	}
	pd.rebuildMaxTimestampMeta()
}

// saveGroupsLog writes a compacted groups.log from live group state.
// Called only at shutdown, where run() is blocked in the admin function
// and no new requests can be dispatched to group reqCh channels.
func (c *Cluster) saveGroupsLog(fsys fs, dir string) error {
	var allEntries []groupLogEntry
	for _, g := range c.groups.gs {
		var entries []groupLogEntry
		g.waitControl(func() {
			g.drainReqCh()
			entries = c.collectGroupEntries(g)
		})
		allEntries = append(allEntries, entries...)
	}

	path := filepath.Join(dir, "groups.log")
	tmpPath := path + ".tmp"
	f, err := fsys.OpenFile(tmpPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	for _, e := range allEntries {
		if _, err := appendLogEntry(f, e, c.cfg.syncWrites); err != nil {
			return err
		}
	}
	if c.groupsLogFile != nil {
		c.groupsLogFile.Close()
		c.groupsLogFile = nil
	}
	return fsys.Rename(tmpPath, path)
}

// collectGroupEntries gathers all persistable state from a group.
// This must be called from within the group's manage goroutine via
// waitControl, OR after the group has been stopped (shutdown).
func (*Cluster) collectGroupEntries(g *group) []groupLogEntry {
	var entries []groupLogEntry

	// Group metadata
	switch g.typ {
	case "consumer":
		entries = append(entries, g.meta848Entry())
	default:
		entries = append(entries, g.classicMetaEntry())
	}

	// Static members
	for instanceID, memberID := range g.staticMembers {
		entries = append(entries, g.staticMemberEntry(instanceID, memberID))
	}

	// Committed offsets
	g.commits.each(func(topic string, part int32, oc *offsetCommit) {
		entries = append(entries, g.commitEntry(topic, part, *oc))
	})

	return entries
}

func (c *Cluster) savePIDsLog(fsys fs, dir string) error {
	// Close the live append handle before rewriting. No race here
	// (pids are single-threaded from run()), but prevents a stale
	// file descriptor.
	if c.pidsLogFile != nil {
		c.pidsLogFile.Close()
		c.pidsLogFile = nil
	}

	path := filepath.Join(dir, "pids.log")
	tmpPath := path + ".tmp"
	f, err := fsys.OpenFile(tmpPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	for _, pidinf := range c.pids.ids {
		// Write the latest state for each PID
		entry := pidLogEntry{
			Type:  "init",
			PID:   pidinf.id,
			Epoch: pidinf.epoch,
			TxID:  pidinf.txid,
		}
		if pidinf.txid != "" {
			entry.Timeout = pidinf.txTimeout
			entry.Commit = &pidinf.lastWasCommit
		}
		if _, err := appendLogEntry(f, entry, c.cfg.syncWrites); err != nil {
			return err
		}
	}
	return fsys.Rename(tmpPath, path)
}

func (c *Cluster) saveSeqWindows(fsys fs, dir string) error {
	var windows []persistSeqWindowEntry
	for pid, pidinf := range c.pids.ids {
		pidinf.windows.each(func(topic string, part int32, pw *pidwindow) {
			var entries [5]persistPidEntry
			for i, e := range pw.entries {
				entries[i] = persistPidEntry{
					FirstSeq: e.firstSeq,
					NextSeq:  e.nextSeq,
					Offset:   e.offset,
				}
			}
			windows = append(windows, persistSeqWindowEntry{
				PID:     pid,
				Topic:   topic,
				Part:    part,
				Entries: entries,
				Count:   pw.count,
				At:      pw.at,
				Epoch:   pw.epoch,
				Seen:    pw.seen,
				NextSeq: pw.nextSeq,
			})
		})
	}
	return writeJSONFile(fsys, filepath.Join(dir, "seq_windows.json"), persistSeqWindows{
		Version: currentPersistVersion,
		Windows: windows,
	})
}

//////////////////////
// LOAD (STARTUP)
//////////////////////

// cleanupTmpFiles removes orphaned .tmp files left by interrupted
// writeJSONFile calls. These are incomplete writes that should be discarded.
// Checks both the top-level dir and partition subdirectories.
func cleanupTmpFiles(fsys fs, dir string) {
	removeTmpIn := func(d string) {
		entries, err := fsys.ReadDir(d)
		if err != nil {
			return
		}
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".tmp") {
				fsys.Remove(filepath.Join(d, e.Name()))
			}
		}
	}
	removeTmpIn(dir)
	partsDir := filepath.Join(dir, "partitions")
	pdirs, err := fsys.ReadDir(partsDir)
	if err != nil {
		return
	}
	for _, d := range pdirs {
		if d.IsDir() {
			removeTmpIn(filepath.Join(partsDir, d.Name()))
		}
	}
}

// loadFromDisk loads persisted state into the cluster.
// Returns true if persisted state was found and loaded, false if no data dir exists.
func (c *Cluster) loadFromDisk() (bool, error) {
	fsys := c.fs
	dir := c.cfg.dataDir

	// Check if meta.json exists
	metaBytes, err := fsys.ReadFile(filepath.Join(dir, "meta.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("reading meta.json: %w", err)
	}

	var meta persistMeta
	if err := json.Unmarshal(metaBytes, &meta); err != nil {
		return false, fmt.Errorf("parsing meta.json: %w", err)
	}
	if meta.Version > currentPersistVersion {
		return false, fmt.Errorf("meta.json version %d is newer than supported %d", meta.Version, currentPersistVersion)
	}
	c.cfg.clusterID = meta.ClusterID

	// Clean up orphaned .tmp files from interrupted writeJSONFile calls
	cleanupTmpFiles(fsys, dir)

	// Load broker configs first (segment configs needed by topics),
	// then topics (needed by partitions), then partitions (may
	// detect crash-aborted PIDs).
	var (
		crashAbortedMu   sync.Mutex
		crashAbortedPIDs = make(map[int64]struct{})
	)
	if err := errors.Join(
		c.loadBrokerConfigs(fsys, dir),
		c.loadTopics(fsys, dir),
		c.loadPartitions(fsys, dir, &crashAbortedMu, crashAbortedPIDs),
	); err != nil {
		return false, err
	}

	// Load PIDs, then bump epoch for PIDs that had in-flight
	// transactions at crash time.
	if err := c.loadPIDsLog(fsys, dir); err != nil {
		return false, err
	}
	for pid := range crashAbortedPIDs {
		if pidinf, ok := c.pids.ids[pid]; ok {
			pidinf.epoch++
		}
	}

	if err := errors.Join(
		c.loadGroupsLog(fsys, dir),
		c.loadACLs(fsys, dir),
		c.loadSASL(fsys, dir),
		c.loadQuotas(fsys, dir),
		c.loadSeqWindows(fsys, dir),
	); err != nil {
		return false, err
	}

	if err := c.loadSessionState(); err != nil {
		c.cfg.logger.Logf(LogLevelWarn, "load session state: %v", err)
	}

	return true, nil
}

func (c *Cluster) loadBrokerConfigs(fsys fs, dir string) error {
	var pbc persistBrokerConfigs
	if found, err := readJSONFile(fsys, filepath.Join(dir, "broker_configs.json"), &pbc); err != nil || !found {
		return err
	}
	// Merge: config overrides from NewCluster opts take precedence
	m := make(map[string]*string, len(pbc.Configs))
	for k, v := range pbc.Configs {
		m[k] = &v
	}
	// Apply overrides from cfg.brokerConfigs
	for k, v := range c.cfg.brokerConfigs {
		if v == "" {
			m[k] = nil
		} else {
			m[k] = &v
		}
	}
	c.storeBcfgs(m)
	return nil
}

func (c *Cluster) loadTopics(fsys fs, dir string) error {
	var pt persistTopics
	if found, err := readJSONFile(fsys, filepath.Join(dir, "topics.json"), &pt); err != nil || !found {
		return err
	}

	for _, t := range pt.Topics {
		c.data.id2t[t.ID] = t.Name
		c.data.t2id[t.Name] = t.ID
		c.data.treplicas[t.Name] = t.Replicas
		c.data.tnorms[normalizeTopicName(t.Name)] = t.Name

		if len(t.Configs) > 0 {
			cfgs := make(map[string]*string, len(t.Configs))
			for k, v := range t.Configs {
				cfgs[k] = &v
			}
			c.data.tcfgs[t.Name] = cfgs
		}

		// Create partition data entries (batches loaded separately)
		for p := range t.Partitions {
			pd := c.data.tps.mkp(t.Name, int32(p), c.newPartData(int32(p)))
			pd.t = t.Name
		}
	}
	return nil
}

func (c *Cluster) loadPartitions(fsys fs, dir string, crashAbortedMu *sync.Mutex, crashAbortedPIDs map[int64]struct{}) error {
	return c.forEachPartition(func(topic string, part int32, pd *partData) error {
		return c.loadPartition(fsys, dir, topic, part, pd, crashAbortedMu, crashAbortedPIDs)
	})
}

func (c *Cluster) loadPartition(fsys fs, dir, topic string, part int32, pd *partData, crashAbortedMu *sync.Mutex, crashAbortedPIDs map[int64]struct{}) error {
	pdir := partDir(dir, topic, part)

	// Try snapshot first
	snapData, err := fsys.ReadFile(filepath.Join(pdir, "snapshot.json"))
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	// Find segment files
	entries, err := fsys.ReadDir(pdir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // no data yet
		}
		return err
	}

	var segFiles []int64
	for _, e := range entries {
		name := e.Name()
		if len(name) > 4 && name[len(name)-4:] == ".dat" {
			base, err := strconv.ParseInt(name[:len(name)-4], 10, 64)
			if err == nil {
				segFiles = append(segFiles, base)
			}
		}
	}
	slices.Sort(segFiles)

	// Try using snapshot for fast startup
	if snapData != nil && len(segFiles) > 0 {
		var snap persistPartSnapshot
		if err := json.Unmarshal(snapData, &snap); err == nil && snap.Version <= currentPersistVersion {
			if snapshotMatchesSegments(snap, segFiles, fsys, pdir) {
				return c.loadPartitionFromSnapshot(pd, snap, segFiles, fsys, pdir)
			}
		}
	}

	// Full replay - scan all segments
	return c.loadPartitionFullReplay(pd, segFiles, fsys, pdir, crashAbortedMu, crashAbortedPIDs)
}

func snapshotMatchesSegments(snap persistPartSnapshot, segFiles []int64, fsys fs, pdir string) bool {
	if len(snap.Segments) != len(segFiles) {
		return false
	}
	for i, ss := range snap.Segments {
		if ss.BaseOffset != segFiles[i] {
			return false
		}
		info, err := fsys.Stat(filepath.Join(pdir, segmentFileName(ss.BaseOffset)))
		if err != nil || info.Size() != ss.Size {
			return false
		}
	}
	return true
}

func (c *Cluster) loadPartitionFromSnapshot(pd *partData, snap persistPartSnapshot, segFiles []int64, fsys fs, pdir string) error {
	pd.highWatermark = snap.HighWatermark
	pd.lastStableOffset = snap.LastStableOffset
	pd.logStartOffset = snap.LogStartOffset
	pd.epoch = snap.Epoch
	if snap.LeaderNode >= 0 && int(snap.LeaderNode) < len(c.bs) {
		pd.leader = c.bs[snap.LeaderNode]
	}
	pd.maxFirstTimestamp = snap.MaxTimestamp
	pd.createdAt = snap.CreatedAt
	for i, base := range segFiles {
		ss := snap.Segments[i]
		pd.segments = append(pd.segments, segmentInfo{
			base:     base,
			size:     ss.Size,
			minEpoch: ss.MinEpoch,
			maxEpoch: ss.MaxEpoch,
		})
		if _, err := c.loadSegmentBatches(pd, fsys, pdir, base); err != nil {
			return err
		}
	}

	for _, a := range snap.AbortedTxns {
		pd.abortedTxns = append(pd.abortedTxns, abortedTxnEntry{
			producerID:  a.ProducerID,
			firstOffset: a.FirstOffset,
			lastOffset:  a.LastOffset,
		})
	}
	pd.pruneEmptySegments()

	// Accumulate nbytes from batchMeta, skipping entries before
	// logStartOffset (partially-trimmed segments may have stale entries).
	pd.eachBatchMeta(func(_, _ int, m *batchMeta) bool {
		finRec := m.firstOffset + int64(m.lastOffsetDelta)
		if finRec >= pd.logStartOffset {
			pd.nbytes += int64(m.nbytes)
		}
		return true
	})

	pd.rebuildMaxTimestampMeta()

	// Validate HWM, LSO, and logStartOffset against actual loaded
	// batches. A truncated segment could leave fewer batches than the
	// snapshot expected.
	if pd.hasBatches() {
		lastSeg := &pd.segments[len(pd.segments)-1]
		last := &lastSeg.index[len(lastSeg.index)-1]
		maxHWM := last.firstOffset + int64(last.lastOffsetDelta) + 1
		if pd.highWatermark > maxHWM {
			pd.highWatermark = maxHWM
		}
		if pd.lastStableOffset > maxHWM {
			pd.lastStableOffset = maxHWM
		}
	} else {
		pd.highWatermark = pd.logStartOffset
		pd.lastStableOffset = pd.logStartOffset
	}
	if pd.logStartOffset > pd.highWatermark {
		pd.logStartOffset = pd.highWatermark
	}

	// Initialize active segment state so persistBatchToSegment
	// appends to the last segment instead of segment 0.
	c.initActiveSegment(pd, fsys, pdir)

	return nil
}

func (c *Cluster) loadPartitionFullReplay(pd *partData, segFiles []int64, fsys fs, pdir string, crashAbortedMu *sync.Mutex, crashAbortedPIDs map[int64]struct{}) error {
	// Load segments, getting full batches for crash recovery txn state.
	var batches []*partBatch
	for _, base := range segFiles {
		pd.segments = append(pd.segments, segmentInfo{base: base})
		loaded, err := c.loadSegmentBatches(pd, fsys, pdir, base)
		if err != nil {
			return err
		}
		batches = append(batches, loaded...)
	}
	pd.pruneEmptySegments()

	// Reconstruct partition metadata from batchMeta
	if pd.hasBatches() {
		firstSeg := &pd.segments[0]
		first := &firstSeg.index[0]
		lastSeg := &pd.segments[len(pd.segments)-1]
		last := &lastSeg.index[len(lastSeg.index)-1]
		pd.highWatermark = last.firstOffset + int64(last.lastOffsetDelta) + 1

		// logStartOffset: lossy reconstruction. The true value
		// (set by DeleteRecords) is only in the snapshot. On full
		// replay, the first batch's offset is a conservative lower
		// bound - the true logStartOffset may point into the middle
		// of this batch, so some "deleted" records may reappear.
		// This matches Kafka unclean leader election behavior.
		pd.logStartOffset = first.firstOffset

		// epoch: use the last batch's epoch (highest seen).
		pd.epoch = last.epoch
	}

	// Rebuild abortedTxns from control batches. On disk, data batches
	// are written with inTx=true at produce time; endTx clears it in
	// memory but not on disk. During full replay we identify completed
	// transactions by their control batches.
	activeTxns := make(map[int64]int64) // producerID -> firstOffset
	for _, b := range batches {
		isControl := b.Attributes&0x0020 != 0
		if !isControl {
			if b.inTx {
				if _, ok := activeTxns[b.ProducerID]; !ok {
					activeTxns[b.ProducerID] = b.FirstOffset
				}
			}
			continue
		}
		// Control batch: decode the key to determine commit vs abort.
		// Key is 4 bytes: version(2) + type(2). Type 0=ABORT, 1=COMMIT.
		// If the control record is unreadable, treat as abort (fail-safe).
		isAbort := true
		if len(b.Records) >= 4 {
			var rec kmsg.Record
			if err := rec.ReadFrom(b.Records); err == nil && len(rec.Key) >= 4 {
				controlType := int16(binary.BigEndian.Uint16(rec.Key[2:4]))
				isAbort = controlType == 0
			} else {
				c.cfg.logger.Logf(LogLevelWarn, "partition %s-%d: malformed control batch at offset %d, treating as abort",
					pd.t, pd.p, b.FirstOffset)
			}
		} else {
			c.cfg.logger.Logf(LogLevelWarn, "partition %s-%d: empty control batch at offset %d, treating as abort",
				pd.t, pd.p, b.FirstOffset)
		}
		firstOff, ok := activeTxns[b.ProducerID]
		if isAbort && ok {
			pd.abortedTxns = append(pd.abortedTxns, abortedTxnEntry{
				producerID:  b.ProducerID,
				firstOffset: firstOff,
				lastOffset:  b.FirstOffset,
			})
		}
		delete(activeTxns, b.ProducerID)
	}

	// Implicitly abort in-flight transactions. Any producer still in
	// activeTxns had a transaction open when the crash happened. We
	// treat these as aborted so the LSO is not stuck forever.
	if len(activeTxns) > 0 {
		crashAbortedMu.Lock()
		for pid, firstOff := range activeTxns {
			last := firstOff
			for _, b := range batches {
				if b.ProducerID == pid && b.inTx {
					end := b.FirstOffset + int64(b.LastOffsetDelta)
					if end > last {
						last = end
					}
				}
			}
			pd.abortedTxns = append(pd.abortedTxns, abortedTxnEntry{
				producerID:  pid,
				firstOffset: firstOff,
				lastOffset:  last,
			})
			crashAbortedPIDs[pid] = struct{}{}
		}
		crashAbortedMu.Unlock()
	}

	// abortedTxns must be sorted by lastOffset for binary search in
	// isBatchAborted and trimLeft. Implicit aborts (appended above) can
	// have earlier lastOffset values than control-batch-derived entries,
	// so we re-sort.
	slices.SortFunc(pd.abortedTxns, func(a, b abortedTxnEntry) int {
		return cmp.Compare(a.lastOffset, b.lastOffset)
	})

	// Rebuild LSO and other metadata
	pd.recalculateLSO()

	// Rebuild maxTimestamp and nbytes from batchMeta
	pd.eachBatchMeta(func(_, _ int, m *batchMeta) bool {
		if m.firstTimestamp > pd.maxFirstTimestamp {
			pd.maxFirstTimestamp = m.firstTimestamp
		}
		pd.nbytes += int64(m.nbytes)
		return true
	})
	pd.rebuildMaxTimestampMeta()

	// Restore createdAt from the first batch. newPartData sets createdAt
	// to time.Now() but on full replay we want the original creation time.
	if pd.hasBatches() {
		pd.createdAt = time.UnixMilli(pd.segments[0].index[0].firstTimestamp)
	}

	// Initialize active segment state so persistBatchToSegment
	// appends to the last segment instead of segment 0.
	c.initActiveSegment(pd, fsys, pdir)

	return nil
}

// initActiveSegment sets the last segment's size so that persistBatchToSegment
// appends to the right file and knows when to roll.
func (c *Cluster) initActiveSegment(pd *partData, fsys fs, pdir string) {
	if len(pd.segments) == 0 {
		return
	}
	active := &pd.segments[len(pd.segments)-1]
	if active.size > 0 {
		return // already set from snapshot
	}
	path := filepath.Join(pdir, segmentFileName(active.base))
	if info, err := fsys.Stat(path); err != nil {
		c.cfg.logger.Logf(LogLevelWarn, "partition %s-%d: stat active segment %d: %v", pd.t, pd.p, active.base, err)
	} else {
		active.size = info.Size()
	}
}

// loadSegmentBatches reads batches from a segment file (.dat) and metadata
// from the parallel index file (.idx). Segment files contain pure RecordBatch
// wire bytes; entry boundaries are found via RecordBatch Length (big-endian
// int32 at byte 8). Index entries are fixed-size (15 bytes each).
func (c *Cluster) loadSegmentBatches(pd *partData, fsys fs, pdir string, base int64) ([]*partBatch, error) {
	raw, err := fsys.ReadFile(filepath.Join(pdir, segmentFileName(base)))
	if err != nil {
		return nil, err
	}
	idxRaw, _ := fsys.ReadFile(filepath.Join(pdir, indexFileName(base)))

	// Find the segmentInfo for this base to rebuild epoch ranges and index
	// from the actual batch data. The snapshot stores segment metadata
	// (base, size, epoch ranges), but on load we rebuild from the segment
	// file contents for crash safety - the snapshot may be stale.
	var seg *segmentInfo
	for i := range pd.segments {
		if pd.segments[i].base == base {
			seg = &pd.segments[i]
			break
		}
	}

	// Parse RecordBatch entries from segment file.
	var result []*partBatch
	pos := 0
	batchIdx := 0
	for {
		// Need at least FirstOffset(8) + Length(4) = 12 bytes.
		if pos+12 > len(raw) {
			break
		}
		batchLength := int(binary.BigEndian.Uint32(raw[pos+8 : pos+12]))
		if batchLength < 0 || batchLength > 1<<30 {
			break
		}
		batchSize := 12 + batchLength
		if pos+batchSize > len(raw) {
			break
		}
		rb, err := decodeBatchRaw(raw[pos : pos+batchSize])
		if err != nil {
			break // corruption - truncate
		}

		// Read metadata from index file (if available).
		var epoch int32
		var maxEarlierTS int64
		var inTx bool
		idxOff := batchIdx * indexEntrySize
		if idxOff+indexEntrySize <= len(idxRaw) {
			epoch, maxEarlierTS, inTx, _ = decodeIndexEntry(idxRaw[idxOff : idxOff+indexEntrySize])
		}

		batch := &partBatch{
			RecordBatch:         *rb,
			nbytes:              batchSize,
			epoch:               epoch,
			maxEarlierTimestamp: maxEarlierTS,
			inTx:                inTx,
		}
		result = append(result, batch)
		if seg != nil {
			seg.index = append(seg.index, batch.meta(int64(pos)))
			seg.updateEpochRange(batch.epoch)
		}
		pos += batchSize
		batchIdx++
	}

	if pos < len(raw) {
		c.cfg.logger.Logf(LogLevelWarn, "partition %s-%d segment %d: truncating %d corrupt bytes",
			pd.t, pd.p, base, len(raw)-pos)
		path := filepath.Join(pdir, segmentFileName(base))
		if f, err := fsys.OpenFile(path, os.O_WRONLY, 0o644); err != nil {
			c.cfg.logger.Logf(LogLevelWarn, "partition %s-%d segment %d: open for truncate: %v", pd.t, pd.p, base, err)
		} else {
			defer f.Close()
			if err := f.Truncate(int64(pos)); err != nil {
				c.cfg.logger.Logf(LogLevelWarn, "partition %s-%d segment %d: truncate: %v", pd.t, pd.p, base, err)
			}
		}
	}

	// Truncate orphaned index entries that may remain from a prior
	// crash where the index write succeeded but the segment did not.
	expectedIdxBytes := int64(batchIdx * indexEntrySize)
	if int64(len(idxRaw)) > expectedIdxBytes {
		idxPath := filepath.Join(pdir, indexFileName(base))
		if f, err := fsys.OpenFile(idxPath, os.O_WRONLY, 0o644); err == nil {
			f.Truncate(expectedIdxBytes)
			f.Close()
		}
	}

	return result, nil
}

func (c *Cluster) loadPIDsLog(fsys fs, dir string) error {
	raw, err := fsys.ReadFile(filepath.Join(dir, "pids.log"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	c.pidsLogSize.Store(int64(len(raw)))

	entries, validBytes := readEntries(raw)
	if validBytes < len(raw) {
		c.cfg.logger.Logf(LogLevelWarn, "pids.log: discarding %d corrupt trailing bytes", len(raw)-validBytes)
	}
	for _, e := range entries {
		var entry pidLogEntry
		if err := json.Unmarshal(e.data, &entry); err != nil {
			c.cfg.logger.Logf(LogLevelWarn, "pids.log: skipping corrupt entry: %v", err)
			continue
		}
		switch entry.Type {
		case "init":
			pidinf := &pidinfo{
				pids:       &c.pids,
				id:         entry.PID,
				epoch:      entry.Epoch,
				txid:       entry.TxID,
				txTimeout:  entry.Timeout,
				lastActive: time.Now(),
			}
			if entry.Commit != nil {
				pidinf.lastWasCommit = *entry.Commit
			}
			c.pids.ids[entry.PID] = pidinf
			if entry.TxID != "" {
				c.pids.byTxid[entry.TxID] = pidinf
			}
		case "endtx":
			pidinf, ok := c.pids.ids[entry.PID]
			if ok {
				pidinf.epoch = entry.Epoch
				pidinf.inTx = false
				if entry.Commit != nil {
					pidinf.lastWasCommit = *entry.Commit
				}
			}
		case "timeout":
			pidinf, ok := c.pids.ids[entry.PID]
			if ok {
				pidinf.epoch = entry.Epoch
				pidinf.inTx = false
			}
		}
	}
	return nil
}

func (c *Cluster) loadGroupsLog(fsys fs, dir string) error {
	raw, err := fsys.ReadFile(filepath.Join(dir, "groups.log"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	c.groupsLogSize.Store(int64(len(raw)))

	entries, validBytes := readEntries(raw)
	if validBytes < len(raw) {
		c.cfg.logger.Logf(LogLevelWarn, "groups.log: discarding %d corrupt trailing bytes", len(raw)-validBytes)
	}
	r := replayGroupsLog(entries)

	// Initialize groups from replayed state
	if c.groups.gs == nil {
		c.groups.gs = make(map[string]*group)
	}
	topicSnap := c.snapshotTopicMeta()
	for name, data := range r.metas {
		var meta groupLogEntry
		json.Unmarshal(data, &meta)
		g := c.groups.newGroup(name)
		switch meta.Type {
		case "meta848":
			g.typ = "consumer"
			g.assignorName = meta.Assignor
			g.groupEpoch = meta.GroupEpoch
			g.consumerMembers = make(map[string]*consumerMember)
			g.partitionEpochs = make(map[uuid]map[int32]int32)
			g.lastTopicMeta = topicSnap
		default:
			g.typ = meta.GroupType
			g.protocolType = meta.ProtoType
			g.protocol = meta.Protocol
			g.generation = meta.Generation
		}
		c.groups.gs[name] = g
		go g.manage(nil) // loaded from disk - no firstJoin cleanup
	}

	// Apply commits
	for ck, data := range r.commits {
		var entry groupLogEntry
		json.Unmarshal(data, &entry)
		g, ok := c.groups.gs[ck.group]
		if !ok {
			g = c.groups.newGroup(ck.group)
			c.groups.gs[ck.group] = g
			go g.manage(nil) // loaded from disk - no firstJoin cleanup
		}
		oc := offsetCommit{
			offset:      entry.Offset,
			leaderEpoch: entry.Epoch,
			metadata:    entry.Metadata,
		}
		if entry.LastCommit != nil {
			oc.lastCommit = time.UnixMilli(*entry.LastCommit)
		}
		g.waitControl(func() {
			g.commits.set(ck.topic, ck.part, oc)
		})
	}

	// Apply static members
	for sk, data := range r.statics {
		var entry groupLogEntry
		json.Unmarshal(data, &entry)
		g, ok := c.groups.gs[sk.group]
		if !ok {
			continue
		}
		g.waitControl(func() {
			if g.staticMembers == nil {
				g.staticMembers = make(map[string]string)
			}
			g.staticMembers[sk.instance] = entry.MemberID
		})
	}

	return nil
}

func (c *Cluster) loadACLs(fsys fs, dir string) error {
	var pa persistACLs
	if found, err := readJSONFile(fsys, filepath.Join(dir, "acls.json"), &pa); err != nil || !found {
		return err
	}
	for _, a := range pa.ACLs {
		c.acls.add(acl{
			principal:    a.Principal,
			host:         a.Host,
			resourceType: kmsg.ACLResourceType(a.ResourceType),
			resourceName: a.ResourceName,
			pattern:      kmsg.ACLResourcePatternType(a.Pattern),
			operation:    kmsg.ACLOperation(a.Operation),
			permission:   kmsg.ACLPermissionType(a.Permission),
		})
	}
	return nil
}

func (c *Cluster) loadSASL(fsys fs, dir string) error {
	var ps persistSASL
	if found, err := readJSONFile(fsys, filepath.Join(dir, "sasl.json"), &ps); err != nil || !found {
		return err
	}
	if len(ps.Plain) > 0 {
		c.sasls.plain = ps.Plain
	}
	c.sasls.scram256 = persistToScram(ps.Scram256)
	c.sasls.scram512 = persistToScram(ps.Scram512)
	return nil
}

func (c *Cluster) loadQuotas(fsys fs, dir string) error {
	var pq persistQuotas
	if found, err := readJSONFile(fsys, filepath.Join(dir, "quotas.json"), &pq); err != nil || !found {
		return err
	}
	for _, q := range pq.Quotas {
		entity := make(quotaEntity, len(q.Entity))
		for i, ec := range q.Entity {
			entity[i] = quotaEntityComponent{
				entityType: ec.Type,
				name:       ec.Name,
			}
		}
		key := entity.key()
		c.quotas[key] = quotaEntry{
			entity: entity,
			values: q.Values,
		}
	}
	return nil
}

func (c *Cluster) loadSeqWindows(fsys fs, dir string) error {
	var psw persistSeqWindows
	if found, err := readJSONFile(fsys, filepath.Join(dir, "seq_windows.json"), &psw); err != nil || !found {
		return err
	}
	for _, w := range psw.Windows {
		pidinf, ok := c.pids.ids[w.PID]
		if !ok {
			continue
		}

		var pw pidwindow
		if w.Count > 0 {
			// v2 format: independent entries.
			for i, e := range w.Entries {
				pw.entries[i] = pidEntry{
					firstSeq: e.FirstSeq,
					nextSeq:  e.NextSeq,
					offset:   e.Offset,
				}
			}
			pw.count = w.Count
			pw.at = w.At
			pw.epoch = w.Epoch
			pw.seen = w.Seen
			pw.nextSeq = w.NextSeq
		} else if w.Seq != [5]int32{} {
			// v1 format (legacy overlapping pairs): convert to
			// independent entries by walking the circular buffer.
			pw.epoch = w.Epoch
			pw.seen = w.Seen
			pw.nextSeq = w.NextSeq
			if !pw.seen {
				pw.seen = true
				pw.nextSeq = w.Seq[w.At]
			}
			// Reconstruct entries from the overlapping pairs.
			// The old buffer stored seq[i]=firstSeq, seq[(i+1)%5]=nextSeq.
			// Walk from the oldest entry (at) backwards.
			for i := range 5 {
				idx := (int(w.At) - 1 - i + 10) % 5
				next := (idx + 1) % 5
				if w.Seq[idx] == 0 && w.Seq[next] == 0 && i > 0 {
					break
				}
				pw.entries[pw.count] = pidEntry{
					firstSeq: w.Seq[idx],
					nextSeq:  w.Seq[next],
					offset:   w.Offsets[idx],
				}
				pw.count++
			}
			pw.at = pw.count % 5
		}
		pidinf.windows.set(w.Topic, w.Part, pw)
	}
	return nil
}

///////////////////////////////
// LIVE SYNC PERSIST HELPERS
///////////////////////////////

// openSegmentFiles opens the active segment (O_RDWR for read+write) and
// index file (O_WRONLY) for the last segment in pd.segments.
func (c *Cluster) openSegmentFiles(pd *partData, pdir string) error {
	base := pd.segments[len(pd.segments)-1].base
	segPath := filepath.Join(pdir, segmentFileName(base))
	sf, err := c.fs.OpenFile(segPath, os.O_CREATE|os.O_APPEND|os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	idxPath := filepath.Join(pdir, indexFileName(base))
	idxF, err := c.fs.OpenFile(idxPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		sf.Close()
		return err
	}
	pd.activeSegFile = sf
	pd.activeIdxFile = idxF
	return nil
}

// persistBatchToSegment writes a batch to the active segment file.
// Returns the byte position of the entry within the segment file
// (for batchMeta.segPos), or -1 on failure.
func (c *Cluster) persistBatchToSegment(pd *partData, b *partBatch) int64 {
	fsys := c.fs
	pdir := partDir(c.storageDir, pd.t, pd.p)

	// Ensure segment exists in memory before any I/O that might fail.
	if len(pd.segments) == 0 {
		pd.segments = append(pd.segments, segmentInfo{base: b.FirstOffset})
	}

	if pd.activeSegFile == nil {
		if err := fsys.MkdirAll(pdir, 0o755); err != nil {
			c.cfg.logger.Logf(LogLevelWarn, "persist batch mkdir %s: %v", pdir, err)
			return -1
		}
		if err := c.openSegmentFiles(pd, pdir); err != nil {
			c.cfg.logger.Logf(LogLevelWarn, "persist batch open %s-%d: %v", pd.t, pd.p, err)
			return -1
		}
	}

	// Roll the segment if needed.
	active := &pd.segments[len(pd.segments)-1]
	if active.size >= c.segmentBytes(pd.t) {
		active.endOff = b.FirstOffset
		pd.closeActiveFiles(c.cfg.syncWrites)
		pd.segments = append(pd.segments, segmentInfo{base: b.FirstOffset})
		if err := c.openSegmentFiles(pd, pdir); err != nil {
			c.cfg.logger.Logf(LogLevelWarn, "persist batch open %s-%d: %v", pd.t, pd.p, err)
			return -1
		}
		active = &pd.segments[len(pd.segments)-1]
	}

	// Write RecordBatch to segment file, then index entry.
	segPos := active.size
	bp := encodeBatch(b)
	defer func() { *bp = (*bp)[:0]; batchPool.Put(bp) }()
	batchSize := int64(len(*bp))
	_, segErr := pd.activeSegFile.Write(*bp)
	if segErr == nil {
		idx := encodeIndexEntry(b.epoch, b.maxEarlierTimestamp, b.inTx)
		_, segErr = pd.activeIdxFile.Write(idx[:])
	}
	if segErr != nil {
		c.cfg.logger.Logf(LogLevelWarn, "persist batch %s-%d: %v", pd.t, pd.p, segErr)
		pd.activeSegFile.Truncate(active.size)
		pd.closeActiveFiles(false)
		return -1
	}

	// With SyncWrites, fsync every batch for immediate durability.
	// Without it, we rely on the OS page cache and sync only on
	// segment roll and shutdown (like real Kafka's default).
	if c.cfg.syncWrites {
		pd.activeSegFile.Sync()
		pd.activeIdxFile.Sync()
	}

	active.size += batchSize
	return segPos
}

// persistGroupEntry appends a group log entry.
// Called from group handlers when dataDir is set.
// Multiple group manage() goroutines may call this concurrently,
// so writes are serialized with groupsLogMu.
func (c *Cluster) persistGroupEntry(entry groupLogEntry) error {
	if !c.persist() || c.dead.Load() {
		return nil
	}
	c.groupsLogMu.Lock()
	defer c.groupsLogMu.Unlock()
	if c.groupsLogFile == nil {
		path := filepath.Join(c.cfg.dataDir, "groups.log")
		f, err := c.fs.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			c.cfg.logger.Logf(LogLevelWarn, "persist group entry open: %v", err)
			return err
		}
		c.groupsLogFile = f
	}
	n, err := appendLogEntry(c.groupsLogFile, entry, c.cfg.syncWrites)
	if err != nil {
		c.cfg.logger.Logf(LogLevelWarn, "persist group entry write: %v", err)
		c.groupsLogFile.Close()
		c.groupsLogFile = nil
		return err
	}
	if c.groupsLogSize.Add(int64(n)) >= c.stateLogCompactBytes() {
		c.needsGroupsCompact.Store(true)
	}
	return nil
}

// persistPIDEntry appends a PID log entry.
// Called from txn handlers when dataDir is set.
func (c *Cluster) persistPIDEntry(entry pidLogEntry) error {
	if !c.persist() || c.dead.Load() {
		return nil
	}
	if c.pidsLogFile == nil {
		path := filepath.Join(c.cfg.dataDir, "pids.log")
		f, err := c.fs.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			c.cfg.logger.Logf(LogLevelWarn, "persist pid entry open: %v", err)
			return err
		}
		c.pidsLogFile = f
	}
	n, err := appendLogEntry(c.pidsLogFile, entry, c.cfg.syncWrites)
	if err != nil {
		c.cfg.logger.Logf(LogLevelWarn, "persist pid entry write: %v", err)
		c.pidsLogFile.Close()
		c.pidsLogFile = nil
		return err
	}
	if c.pidsLogSize.Add(int64(n)) >= c.stateLogCompactBytes() {
		c.compactPIDsLog()
	}
	return nil
}

// persistState is a shared helper for live-sync state file writes.
// It calls the given save function and logs on error.
func (c *Cluster) persistState(name string, fn func(fs, string) error) {
	if !c.persist() {
		return
	}
	if err := fn(c.fs, c.cfg.dataDir); err != nil {
		c.cfg.logger.Logf(LogLevelWarn, "persist %s: %v", name, err)
	}
}

func (c *Cluster) stateLogCompactBytes() int64 {
	if v, ok := c.loadBcfgs()["state.log.compact.bytes"]; ok && v != nil {
		if n, err := strconv.ParseInt(*v, 10, 64); err == nil {
			return n
		}
	}
	return 10 << 20
}

func (c *Cluster) compactPIDsLog() {
	if err := c.savePIDsLog(c.fs, c.cfg.dataDir); err != nil {
		c.cfg.logger.Logf(LogLevelWarn, "compact pids.log: %v", err)
		return
	}
	path := filepath.Join(c.cfg.dataDir, "pids.log")
	if info, err := c.fs.Stat(path); err == nil {
		c.pidsLogSize.Store(info.Size())
	}
}

// compactGroupsLog rewrites groups.log keeping only the latest entry per
// key. Unlike saveGroupsLog (which collects from live group state via
// waitControl), this compacts from the file itself under groupsLogMu,
// ensuring no entries are lost to a race with persistGroupEntry.
func (c *Cluster) compactGroupsLog() {
	c.needsGroupsCompact.Store(false)

	c.groupsLogMu.Lock()
	defer c.groupsLogMu.Unlock()

	// Close the live append handle so any pending data is flushed
	// before we read the file.
	if c.groupsLogFile != nil {
		if c.cfg.syncWrites {
			c.groupsLogFile.Sync()
		}
		c.groupsLogFile.Close()
		c.groupsLogFile = nil
	}

	path := filepath.Join(c.cfg.dataDir, "groups.log")
	raw, err := c.fs.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			c.cfg.logger.Logf(LogLevelWarn, "compact groups.log: read: %v", err)
		}
		return
	}

	entries, _ := readEntries(raw)
	r := replayGroupsLog(entries)

	// Write compacted entries to tmp, then atomic rename.
	tmpPath := path + ".tmp"
	f, err := c.fs.OpenFile(tmpPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		c.cfg.logger.Logf(LogLevelWarn, "compact groups.log: create: %v", err)
		return
	}
	defer f.Close()
	var writeErr error
	write := func(data []byte) {
		if writeErr == nil {
			writeErr = writeEntry(f, data, false)
		}
	}
	for _, data := range r.metas {
		write(data)
	}
	for _, data := range r.statics {
		write(data)
	}
	for _, data := range r.commits {
		write(data)
	}
	if writeErr != nil {
		c.cfg.logger.Logf(LogLevelWarn, "compact groups.log: write: %v", writeErr)
		c.fs.Remove(tmpPath)
		return
	}
	if c.cfg.syncWrites {
		if err := f.Sync(); err != nil {
			c.cfg.logger.Logf(LogLevelWarn, "compact groups.log: sync: %v", err)
			c.fs.Remove(tmpPath)
			return
		}
	}
	if err := c.fs.Rename(tmpPath, path); err != nil {
		c.cfg.logger.Logf(LogLevelWarn, "compact groups.log: rename: %v", err)
		return
	}
	if info, err := c.fs.Stat(path); err == nil {
		c.groupsLogSize.Store(info.Size())
	}
}

func (c *Cluster) persistTopicsState()        { c.persistState("topics", c.saveTopics) }
func (c *Cluster) persistACLsState()          { c.persistState("acls", c.saveACLs) }
func (c *Cluster) persistSASLState()          { c.persistState("sasl", c.saveSASL) }
func (c *Cluster) persistBrokerConfigsState() { c.persistState("broker configs", c.saveBrokerConfigs) }
func (c *Cluster) persistQuotasState()        { c.persistState("quotas", c.saveQuotas) }

// Session state types - written as a one-shot JSON file on clean shutdown,
// loaded on startup to restore ephemeral group member state.
type (
	sessionState struct {
		ShutdownAt     time.Time                       `json:"shutdownAt"`
		ClassicGroups  map[string]sessionClassicGroup  `json:"classicGroups,omitempty"`
		ConsumerGroups map[string]sessionConsumerGroup `json:"consumerGroups,omitempty"`
		ShareGroups    map[string]sessionShareGroup    `json:"shareGroups,omitempty"`
		GroupConfigs   map[string]map[string]*string   `json:"groupConfigs,omitempty"`
		InProgressTxns []sessionInProgressTxn          `json:"inProgressTxns,omitempty"`
		FetchSessions  map[int32][]sessionFetchSession `json:"fetchSessions,omitempty"` // broker node -> sessions
	}

	// sessionFetchSession persists a KIP-227 fetch session. Real Kafka
	// drops fetch session state on broker restart (a multi-minute affair),
	// so clients always see SessionIDNotFound and fall back to a full
	// fetch. kfake's sub-second restart makes that fallback overhead
	// dominate test throughput, so we persist the session so the client's
	// incremental fetch continues to work across restart.
	sessionFetchSession struct {
		ID         int32                     `json:"id"`
		Epoch      int32                     `json:"e"`
		Partitions []sessionFetchSessionPart `json:"parts,omitempty"`
	}
	sessionFetchSessionPart struct {
		Topic              string `json:"t"`
		Partition          int32  `json:"p"`
		FetchOffset        int64  `json:"fo"`
		MaxBytes           int32  `json:"mb"`
		CurrentEpoch       int32  `json:"ce,omitempty"`
		LastHighWatermark  int64  `json:"lhw,omitempty"`
		LastLogStartOffset int64  `json:"llso,omitempty"`
	}

	sessionInProgressTxn struct {
		PID                int64                                                       `json:"pid"`
		TxStart            time.Time                                                   `json:"txStart"`
		TxPartFirstOffsets map[string]map[int32]int64                                  `json:"txPartFirstOffsets"`
		TxGroups           []string                                                    `json:"txGroups,omitempty"`
		TxOffsets          map[string]map[string]map[int32]sessionTxStagedOffsetCommit `json:"txOffsets,omitempty"`
	}

	// sessionTxStagedOffsetCommit persists an offsetCommit staged via
	// TxnOffsetCommit but not yet applied (pending EndTxn with commit=true).
	// Without this persisting across restart, a client that sent
	// TxnOffsetCommit on broker N and EndTxn to broker N+1 (after restart)
	// would see its EndTxn succeed without actually applying the offsets --
	// endTx's txOffsets loop runs on an empty map. The broker sends success,
	// the client believes its commit landed, and the next rebalance sends
	// the partition to a new owner that reads from the (stale) prior
	// committed offset -- re-delivering records and tripping strict
	// saw-double checks under run_tests.sh --restart.
	sessionTxStagedOffsetCommit struct {
		Offset      int64   `json:"o"`
		LeaderEpoch int32   `json:"e,omitempty"`
		Metadata    *string `json:"m,omitempty"`
	}

	sessionClassicGroup struct {
		Leader  string                 `json:"leader"`
		Members []sessionClassicMember `json:"members"`
		// State captured at shutdown. On reload we re-run
		// rebalance() if State != groupStable so that partitions
		// that were orphaned by an in-progress rebalance (e.g. a
		// LeaveGroup request arrived and transitioned to
		// PreparingRebalance but the rebalance hadn't completed)
		// get properly re-assigned. Zero value (groupEmpty) is
		// treated as "no info" and does NOT force rebalance; empty
		// groups re-initialize naturally as members join.
		State groupState `json:"state,omitempty"`
	}

	sessionClassicMember struct {
		ID                 string    `json:"id"`
		InstanceID         *string   `json:"instance,omitempty"`
		ClientID           string    `json:"clientID"`
		ClientHost         string    `json:"clientHost"`
		Protocols          []string  `json:"protocols"`
		Assignment         []byte    `json:"assignment,omitempty"`
		SessionTimeoutMs   int32     `json:"sessionTimeoutMs"`
		RebalanceTimeoutMs int32     `json:"rebalanceTimeoutMs"`
		LastHeartbeat      time.Time `json:"lastHeartbeat,omitzero"`
	}

	sessionConsumerGroup struct {
		PartitionEpochs       map[uuid]map[int32]int32 `json:"partitionEpochs"`
		TargetAssignmentEpoch int32                    `json:"targetAssignmentEpoch"`
		Members               []sessionConsumerMember  `json:"members"`
	}

	sessionConsumerMember struct {
		ID                   string                   `json:"id"`
		InstanceID           *string                  `json:"instance,omitempty"`
		ClientID             string                   `json:"clientID"`
		ClientHost           string                   `json:"clientHost"`
		Epoch                int32                    `json:"epoch"`
		PrevEpoch            int32                    `json:"prevEpoch"`
		Topics               []string                 `json:"topics"`
		Reconciled           map[uuid][]int32         `json:"reconciled"`
		PendingRevoke        map[uuid][]int32         `json:"pendingRevoke,omitempty"`
		Target               map[uuid][]int32         `json:"target"`
		PartAssignmentEpochs map[uuid]map[int32]int32 `json:"partAssignmentEpochs,omitempty"`
		CmState              int8                     `json:"cmState"`
		Rack                 *string                  `json:"rack,omitempty"`
		Assignor             string                   `json:"assignor"`
		RebalanceTimeoutMs   int32                    `json:"rebalanceTimeoutMs"`
		LastHeartbeat        time.Time                `json:"lastHeartbeat,omitzero"`
	}

	// Share group state: partition SPSO, per-record acquisition state,
	// and members. Persisting members lets a rejoin after restart land
	// on the existing shareMember instead of triggering a full join +
	// rebalance; under run_tests.sh --restart that difference is the
	// difference between net-forward progress per cycle and no progress.
	sessionShareGroup struct {
		GroupEpoch int32                                      `json:"groupEpoch"`
		Partitions map[string]map[int32]sessionSharePartition `json:"partitions"` // topic -> partition -> state
		Members    []sessionShareMember                       `json:"members,omitempty"`
	}

	sessionSharePartition struct {
		SPSO    int64                        `json:"spso"`
		Records map[int64]sessionShareRecord `json:"records,omitempty"`
	}

	sessionShareRecord struct {
		State         int8   `json:"s"`
		DeliveryCount int32  `json:"dc"`
		AcquiredBy    string `json:"ab,omitempty"`
	}

	sessionShareMember struct {
		ID               string           `json:"id"`
		ClientID         string           `json:"clientID,omitempty"`
		ClientHost       string           `json:"clientHost,omitempty"`
		Rack             *string          `json:"rack,omitempty"`
		Epoch            int32            `json:"epoch"`
		PrevEpoch        int32            `json:"prevEpoch,omitempty"`
		SubscribedTopics []string         `json:"subs,omitempty"`
		Assignment       map[uuid][]int32 `json:"asgn,omitempty"`
		LastHeartbeat    time.Time        `json:"last,omitzero"`
	}
)

func (c *Cluster) saveSessionState() error {
	ss := sessionState{ShutdownAt: time.Now()}
	for _, g := range c.groups.gs {
		g.waitControl(func() {
			g.drainReqCh()
			c.cfg.logger.Logf(LogLevelDebug, "saveSessionState: group=%s state=%s members=%d consumerMembers=%d",
				g.name, g.state, len(g.members), len(g.consumerMembers))
			switch {
			case len(g.members) > 0:
				sg := sessionClassicGroup{Leader: g.leader, State: g.state}
				for _, m := range g.members {
					sm := sessionClassicMember{
						ID:                 m.memberID,
						InstanceID:         m.instanceID,
						ClientID:           m.clientID,
						ClientHost:         m.clientHost,
						Assignment:         m.assignment,
						SessionTimeoutMs:   m.join.SessionTimeoutMillis,
						RebalanceTimeoutMs: m.join.RebalanceTimeoutMillis,
						LastHeartbeat:      m.last,
					}
					for _, p := range m.join.Protocols {
						sm.Protocols = append(sm.Protocols, p.Name)
					}
					sg.Members = append(sg.Members, sm)
				}
				if ss.ClassicGroups == nil {
					ss.ClassicGroups = make(map[string]sessionClassicGroup)
				}
				ss.ClassicGroups[g.name] = sg

			case len(g.consumerMembers) > 0:
				sg := sessionConsumerGroup{
					PartitionEpochs:       g.partitionEpochs,
					TargetAssignmentEpoch: g.targetAssignmentEpoch,
				}
				for _, m := range g.consumerMembers {
					sm := sessionConsumerMember{
						ID:                   m.memberID,
						InstanceID:           m.instanceID,
						ClientID:             m.clientID,
						ClientHost:           m.clientHost,
						Epoch:                m.memberEpoch,
						PrevEpoch:            m.previousMemberEpoch,
						Topics:               m.subscribedTopics,
						Reconciled:           m.lastReconciledSent,
						PendingRevoke:        m.partitionsPendingRevocation,
						Target:               m.targetAssignment,
						PartAssignmentEpochs: m.partAssignmentEpochs,
						CmState:              int8(m.state),
						Rack:                 m.rackID,
						Assignor:             m.serverAssignor,
						RebalanceTimeoutMs:   m.rebalanceTimeoutMs,
						LastHeartbeat:        m.last,
					}
					sg.Members = append(sg.Members, sm)
				}
				if ss.ConsumerGroups == nil {
					ss.ConsumerGroups = make(map[string]sessionConsumerGroup)
				}
				ss.ConsumerGroups[g.name] = sg
			}
		})
	}
	// Save share group partition state (SPSO + per-record tracking)
	// AND members. Persisting members is what lets a post-restart
	// heartbeat land as a rejoin on the existing member instead of
	// triggering a fresh join + full-group rebalance on every --restart
	// cycle, which otherwise starves net-forward consumption progress.
	for name, sg := range c.shareGroups.gs {
		if !sg.waitControl(func() {
			// No drainReqCh here: share group heartbeats are the
			// only request type dispatched to sg.reqCh.
			// Contrast with group.drainReqCh which must flush
			// OffsetCommit requests before snapshotting.
			ssg := sessionShareGroup{
				GroupEpoch: sg.groupEpoch,
				Partitions: make(map[string]map[int32]sessionSharePartition),
			}
			sg.mu.Lock()
			sg.partitions.each(func(topic string, partition int32, sp *sharePartition) {
				if _, ok := ssg.Partitions[topic]; !ok {
					ssg.Partitions[topic] = make(map[int32]sessionSharePartition)
				}
				ssp := sessionSharePartition{
					SPSO: sp.spso,
				}
				if len(sp.records) > 0 {
					ssp.Records = make(map[int64]sessionShareRecord, len(sp.records))
					for offset, sr := range sp.records {
						ssp.Records[offset] = sessionShareRecord{
							State:         int8(sr.state),
							DeliveryCount: sr.deliveryCount,
							AcquiredBy:    sr.acquiredBy,
						}
					}
				}
				ssg.Partitions[topic][partition] = ssp
			})
			sg.mu.Unlock()
			for _, m := range sg.members {
				sm := sessionShareMember{
					ID:               m.memberID,
					ClientID:         m.clientID,
					ClientHost:       m.clientHost,
					Rack:             m.rackID,
					Epoch:            m.memberEpoch,
					PrevEpoch:        m.previousMemberEpoch,
					SubscribedTopics: slices.Clone(m.subscribedTopics),
					LastHeartbeat:    m.last,
				}
				if len(m.assignment) > 0 {
					sm.Assignment = make(map[uuid][]int32, len(m.assignment))
					for tid, parts := range m.assignment {
						sm.Assignment[tid] = slices.Clone(parts)
					}
				}
				ssg.Members = append(ssg.Members, sm)
			}
			if len(ssg.Partitions) > 0 || len(ssg.Members) > 0 {
				if ss.ShareGroups == nil {
					ss.ShareGroups = make(map[string]sessionShareGroup)
				}
				ss.ShareGroups[name] = ssg
			}
		}) {
			c.cfg.logger.Logf(LogLevelDebug, "saveSessionState: share group %s manage loop exited, skipping", name)
		}
	}

	if len(c.groupConfigs) > 0 {
		ss.GroupConfigs = c.groupConfigs
	}

	// Save in-progress transaction state so records survive restart.
	for pidinf := range c.pids.txs {
		if !pidinf.inTx {
			continue
		}
		st := sessionInProgressTxn{
			PID:                pidinf.id,
			TxStart:            pidinf.txStart,
			TxPartFirstOffsets: make(map[string]map[int32]int64),
		}
		pidinf.txPartFirstOffsets.each(func(t string, p int32, off *int64) {
			if _, ok := st.TxPartFirstOffsets[t]; !ok {
				st.TxPartFirstOffsets[t] = make(map[int32]int64)
			}
			st.TxPartFirstOffsets[t][p] = *off
		})
		// Save staged TxnOffsetCommit offsets (pending EndTxn commit).
		// Without this, after restart endTx has no offsets to apply,
		// responds success to the client, and re-delivery bites on the
		// next rebalance.
		if len(pidinf.txGroups) > 0 {
			st.TxGroups = slices.Clone(pidinf.txGroups)
		}
		if len(pidinf.txOffsets) > 0 {
			st.TxOffsets = make(map[string]map[string]map[int32]sessionTxStagedOffsetCommit, len(pidinf.txOffsets))
			for group, groupOffsets := range pidinf.txOffsets {
				topicMap := make(map[string]map[int32]sessionTxStagedOffsetCommit)
				groupOffsets.each(func(t string, p int32, oc *offsetCommit) {
					if _, ok := topicMap[t]; !ok {
						topicMap[t] = make(map[int32]sessionTxStagedOffsetCommit)
					}
					topicMap[t][p] = sessionTxStagedOffsetCommit{
						Offset:      oc.offset,
						LeaderEpoch: oc.leaderEpoch,
						Metadata:    oc.metadata,
					}
				})
				st.TxOffsets[group] = topicMap
			}
		}
		ss.InProgressTxns = append(ss.InProgressTxns, st)
	}

	// Save fetch sessions (KIP-227) so clients' incremental fetches
	// continue working across restart without a full-fetch fallback.
	// Sessions idle for >1min are NOT saved: real Kafka evicts them
	// via its eviction timer, and without a bound here, accumulated
	// dead sessions from long-running test harnesses keep re-saving
	// and re-loading indefinitely.
	saveCutoff := time.Now().Add(-time.Minute)
	var fetchSessionCount int
	for brokerNode, bs := range c.fetchSessions.sessions {
		var saved []sessionFetchSession
		for _, fs := range bs {
			if fs.lastUsed.Before(saveCutoff) {
				continue
			}
			sf := sessionFetchSession{
				ID:    fs.id,
				Epoch: fs.epoch,
			}
			for k, p := range fs.partitions {
				sf.Partitions = append(sf.Partitions, sessionFetchSessionPart{
					Topic:              k.t,
					Partition:          k.p,
					FetchOffset:        p.fetchOffset,
					MaxBytes:           p.maxBytes,
					CurrentEpoch:       p.currentEpoch,
					LastHighWatermark:  p.lastHighWatermark,
					LastLogStartOffset: p.lastLogStartOffset,
				})
			}
			saved = append(saved, sf)
			fetchSessionCount++
		}
		if len(saved) > 0 {
			if ss.FetchSessions == nil {
				ss.FetchSessions = make(map[int32][]sessionFetchSession)
			}
			ss.FetchSessions[brokerNode] = saved
		}
	}

	if len(ss.ClassicGroups) == 0 && len(ss.ConsumerGroups) == 0 && len(ss.ShareGroups) == 0 && len(ss.GroupConfigs) == 0 && len(ss.InProgressTxns) == 0 && len(ss.FetchSessions) == 0 {
		c.cfg.logger.Logf(LogLevelDebug, "saveSessionState: nothing to save")
		return nil
	}
	c.cfg.logger.Logf(LogLevelDebug, "saveSessionState: saving classic=%d consumer=%d share=%d groupConfigs=%d inProgressTxns=%d fetchSessions=%d",
		len(ss.ClassicGroups), len(ss.ConsumerGroups), len(ss.ShareGroups), len(ss.GroupConfigs), len(ss.InProgressTxns), fetchSessionCount)
	return writeJSONFile(c.fs, filepath.Join(c.cfg.dataDir, "session_state.json"), ss)
}

func (c *Cluster) loadSessionState() error {
	path := filepath.Join(c.cfg.dataDir, "session_state.json")
	data, err := c.fs.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer c.fs.Remove(path)

	var ss sessionState
	if err := json.Unmarshal(data, &ss); err != nil {
		c.cfg.logger.Logf(LogLevelWarn, "session_state.json: corrupt, ignoring: %v", err)
		return nil
	}

	c.cfg.logger.Logf(LogLevelDebug, "loadSessionState: classic=%d consumer=%d inProgressTxns=%d shutdownAt=%v elapsed=%v",
		len(ss.ClassicGroups), len(ss.ConsumerGroups), len(ss.InProgressTxns), ss.ShutdownAt, time.Since(ss.ShutdownAt))

	for name, sg := range ss.ClassicGroups {
		g, ok := c.groups.gs[name]
		if !ok {
			c.cfg.logger.Logf(LogLevelInfo, "loadSessionState: classic group %s not found in groups.gs", name)
			continue
		}
		g.waitControl(func() {
			g.restoreClassicMembers(ss.ShutdownAt, sg)
			c.cfg.logger.Logf(LogLevelDebug, "loadSessionState: restored classic group=%s members=%d state=%s",
				name, len(g.members), g.state)
		})
	}

	for name, sg := range ss.ConsumerGroups {
		g, ok := c.groups.gs[name]
		if !ok {
			c.cfg.logger.Logf(LogLevelInfo, "loadSessionState: consumer group %s not found in groups.gs", name)
			continue
		}
		g.waitControl(func() {
			g.restoreConsumerMembers(ss.ShutdownAt, sg)
			c.cfg.logger.Logf(LogLevelDebug, "loadSessionState: restored consumer group=%s members=%d state=%s",
				name, len(g.consumerMembers), g.state)
		})
	}

	// Restore share group partition state AND members. The share group
	// manage goroutine is created on demand (getOrCreate), so we create
	// it here to restore state into.
	//
	// If the member set is restored, acquisitions are preserved as
	// "acquired" against their original memberID. When the client's
	// ShareGroupHeartbeat arrives post-restart, handleJoin finds the
	// existing member and fast-paths the rejoin with the prior
	// assignment instead of triggering a full group rebalance, which is
	// what otherwise starves net-forward consumption progress per
	// --restart cycle. Members whose last heartbeat is older than the
	// configured share session timeout are skipped -- same pattern as
	// restoreClassicMembers and the in-progress-txn auto-abort: a
	// long-enough outage should let fresh members claim the group.
	// Acquisitions whose save-time is older than the configured record
	// lock duration are released too, so fresh members can re-acquire
	// records the prior owner never got to ack/release.
	shareSessionTimeout := time.Duration(c.shareSessionTimeoutMs()) * time.Millisecond
	shareLockDuration := time.Duration(c.shareRecordLockDurationMs()) * time.Millisecond
	restoreMembers := func(sg *shareGroup, members []sessionShareMember) map[string]struct{} {
		restored := make(map[string]struct{}, len(members))
		now := time.Now()
		for _, sm := range members {
			lastHB := sm.LastHeartbeat
			if lastHB.IsZero() {
				lastHB = ss.ShutdownAt
			}
			if time.Since(lastHB) >= shareSessionTimeout {
				continue // matches restoreClassicMembers: silently drop expired
			}
			m := &shareMember{
				memberID:            sm.ID,
				clientID:            sm.ClientID,
				clientHost:          sm.ClientHost,
				rackID:              sm.Rack,
				memberEpoch:         sm.Epoch,
				previousMemberEpoch: sm.PrevEpoch,
				subscribedTopics:    slices.Clone(sm.SubscribedTopics),
				assignment:          make(map[uuid][]int32, len(sm.Assignment)),
				last:                lastHB,
			}
			if m.last.IsZero() {
				m.last = now
			}
			for tid, parts := range sm.Assignment {
				m.assignment[tid] = slices.Clone(parts)
			}
			sg.members[sm.ID] = m
			sg.resetSessionTimeout(m)
			restored[sm.ID] = struct{}{}
		}
		return restored
	}
	acquisitionStale := time.Since(ss.ShutdownAt) >= shareLockDuration
	for name, ssg := range ss.ShareGroups {
		sg := c.shareGroups.getOrCreate(name)
		sg.waitControl(func() {
			sg.groupEpoch = ssg.GroupEpoch
			restoredMembers := restoreMembers(sg, ssg.Members)
			sg.mu.Lock()
			for topic, parts := range ssg.Partitions {
				for partition, ssp := range parts {
					sp := sg.partitions.mkp(topic, partition, func() *sharePartition {
						return &sharePartition{
							spso:    ssp.SPSO,
							records: make(map[int64]shareRecord),
						}
					})
					sp.spso = ssp.SPSO
					sp.scanOffset = ssp.SPSO
					for offset, ssr := range ssp.Records {
						state := shareRecordState(ssr.State)
						acquiredBy := ssr.AcquiredBy
						// Release the acquisition if the member that
						// held it did not survive the save-to-load
						// window, or if we have sat past the lock
						// duration already. Either case lets a fresh
						// owner re-acquire on the next fetch.
						_, memberSurvived := restoredMembers[acquiredBy]
						if state == shareRecordAcquired && (!memberSurvived || acquisitionStale) {
							if ssr.DeliveryCount >= c.shareMaxDeliveryAttempts() {
								state = shareRecordArchived
							} else {
								state = shareRecordAvailable
							}
							acquiredBy = ""
						}
						sp.records[offset] = shareRecord{
							state:         state,
							deliveryCount: ssr.DeliveryCount,
							acquiredBy:    acquiredBy,
						}
						// Track acquireEnd as one past the highest restored offset.
						if offset+1 > sp.acquireEnd {
							sp.acquireEnd = offset + 1
						}
					}
					sp.advanceSPSO()
				}
			}
			sg.mu.Unlock()
			c.cfg.logger.Logf(LogLevelDebug, "loadSessionState: restored share group=%s epoch=%d members=%d",
				name, sg.groupEpoch, len(sg.members))
		})
	}

	if len(ss.GroupConfigs) > 0 {
		c.groupConfigs = ss.GroupConfigs
		c.cfg.logger.Logf(LogLevelDebug, "loadSessionState: restored %d group configs", len(ss.GroupConfigs))
	}

	// Restore in-progress transaction state: reconstruct txParts,
	// txPartFirstOffsets, and pd.uncommittedPIDs from the saved
	// per-partition first offsets. This lets endTx properly commit
	// or abort records that were produced before the restart.
	//
	// The "effective elapsed" time of a txn excludes broker downtime:
	// we shift txStart forward by time.Since(ShutdownAt) so a client
	// that started a txn pre-restart gets back the same remaining
	// budget, not a shortened one that counts the restart gap as
	// in-flight time. Without this, cumulative downtime across many
	// --restart cycles can drive a live txn past its TransactionTimeout
	// and trigger auto-abort on the NEXT restart, which fences the
	// producer epoch and fails any subsequent Produce with
	// INVALID_PRODUCER_EPOCH (fatal post-KIP-890).
	outage := time.Since(ss.ShutdownAt)
	if outage < 0 {
		outage = 0
	}
	for _, st := range ss.InProgressTxns {
		pidinf, ok := c.pids.ids[st.PID]
		if !ok {
			continue
		}
		// Auto-abort if absolute elapsed time since the txn was opened
		// exceeds its timeout. Unlike session timeouts (where we want to
		// exclude broker downtime so members aren't unfairly kicked),
		// txn timeouts are absolute: real Kafka expires a txn that has
		// been open too long regardless of broker availability.
		txTimeout := time.Duration(pidinf.txTimeout) * time.Millisecond
		if !st.TxStart.IsZero() && time.Since(st.TxStart) > txTimeout {
			c.cfg.logger.Logf(LogLevelDebug, "loadSessionState: auto-aborting expired txn pid=%d epoch=%d (start %v ago, timeout %v)",
				pidinf.id, pidinf.epoch, time.Since(st.TxStart), txTimeout)
			// Reconstruct just enough state for endTx to abort.
			pidinf.inTx = true
			c.pids.txs[pidinf] = struct{}{}
			for topic, parts := range st.TxPartFirstOffsets {
				for part, firstOff := range parts {
					pd, ok := c.data.tps.getp(topic, part)
					if !ok || pd == nil {
						continue
					}
					ps := pidinf.txParts.mkt(topic)
					ps[part] = pd
					ptr := pidinf.txPartFirstOffsets.mkp(topic, part, func() *int64 {
						v := int64(-1)
						return &v
					})
					*ptr = firstOff
					if pd.uncommittedPIDs == nil {
						pd.uncommittedPIDs = make(map[int64]int64)
					}
					existing, exists := pd.uncommittedPIDs[pidinf.id]
					if !exists || firstOff < existing {
						pd.uncommittedPIDs[pidinf.id] = firstOff
					}
					pd.recalculateLSO()
				}
			}
			pidinf.endTx(false)
			continue
		}

		pidinf.inTx = true
		// When restoring a non-expired txn, shift its start time
		// forward by the outage so the txn's effective clock pauses
		// during broker downtime. The timeout check above uses
		// absolute elapsed time (matching real Kafka); this shift
		// only affects the post-restart timer, giving the client a
		// fair chance to complete the txn without the clock counting
		// broker downtime it had no control over.
		pidinf.txStart = st.TxStart
		if !pidinf.txStart.IsZero() {
			pidinf.txStart = pidinf.txStart.Add(outage)
		}
		if pidinf.txStart.IsZero() {
			pidinf.txStart = time.Now()
		}
		c.pids.txs[pidinf] = struct{}{}
		for topic, parts := range st.TxPartFirstOffsets {
			for part, firstOff := range parts {
				pd, ok := c.data.tps.getp(topic, part)
				if !ok || pd == nil {
					continue
				}
				// Reconstruct txParts
				ps := pidinf.txParts.mkt(topic)
				ps[part] = pd
				// Reconstruct txPartFirstOffsets
				ptr := pidinf.txPartFirstOffsets.mkp(topic, part, func() *int64 {
					v := int64(-1)
					return &v
				})
				*ptr = firstOff
				// Reconstruct pd.uncommittedPIDs
				if pd.uncommittedPIDs == nil {
					pd.uncommittedPIDs = make(map[int64]int64)
				}
				existing, exists := pd.uncommittedPIDs[pidinf.id]
				if !exists || firstOff < existing {
					pd.uncommittedPIDs[pidinf.id] = firstOff
				}
				pd.recalculateLSO()
			}
		}
		// Restore staged TxnOffsetCommit state so a post-restart EndTxn
		// can apply the offsets the client staged pre-restart. Without
		// this, the client's EndTxn succeeds but nothing commits --
		// rebalance re-delivery then trips the test's saw-double check.
		if len(st.TxGroups) > 0 {
			pidinf.txGroups = slices.Clone(st.TxGroups)
		}
		if len(st.TxOffsets) > 0 {
			pidinf.txOffsets = make(map[string]tps[offsetCommit], len(st.TxOffsets))
			for group, topicMap := range st.TxOffsets {
				var groupOffsets tps[offsetCommit]
				for topic, parts := range topicMap {
					for part, s := range parts {
						groupOffsets.set(topic, part, offsetCommit{
							offset:      s.Offset,
							leaderEpoch: s.LeaderEpoch,
							metadata:    s.Metadata,
						})
					}
				}
				pidinf.txOffsets[group] = groupOffsets
			}
		}
		c.cfg.logger.Logf(LogLevelDebug, "loadSessionState: restored in-progress txn pid=%d epoch=%d parts=%d groups=%d stagedOffsets=%d (started %v ago)",
			pidinf.id, pidinf.epoch, len(st.TxPartFirstOffsets), len(pidinf.txGroups), len(pidinf.txOffsets), time.Since(pidinf.txStart))
	}
	if len(ss.InProgressTxns) > 0 {
		c.pids.updateTimer()
	}

	// Restore fetch sessions. Without this, every client's first
	// post-restart fetch sees SessionIDNotFound and falls back to a full
	// fetch -- fine once but expensive under rapid --restart cycles.
	// Sessions older than fetch.max.session.idle.ms (default 2min) are
	// skipped -- same elapsed-time discipline as restoreClassicMembers
	// and in-progress-txn auto-abort. A long-enough outage should let
	// the client's SessionIDNotFound fallback reset the session rather
	// than rehydrating stale bookkeeping.
	fetchSessionMaxIdle := 2 * time.Minute
	var restoredFetchSessions, expiredFetchSessions int
	var maxFetchSessionID int32
	sinceShutdown := time.Since(ss.ShutdownAt)
	for brokerNode, saved := range ss.FetchSessions {
		c.fetchSessions.init(brokerNode)
		for _, sf := range saved {
			if sinceShutdown >= fetchSessionMaxIdle {
				expiredFetchSessions++
				if sf.ID > maxFetchSessionID {
					maxFetchSessionID = sf.ID
				}
				continue
			}
			fs := &fetchSession{
				id:         sf.ID,
				epoch:      sf.Epoch,
				partitions: make(map[tp]fetchSessionPartition, len(sf.Partitions)),
				lastUsed:   time.Now(),
			}
			for _, p := range sf.Partitions {
				fs.partitions[tp{p.Topic, p.Partition}] = fetchSessionPartition{
					fetchOffset:        p.FetchOffset,
					maxBytes:           p.MaxBytes,
					currentEpoch:       p.CurrentEpoch,
					lastHighWatermark:  p.LastHighWatermark,
					lastLogStartOffset: p.LastLogStartOffset,
				}
			}
			c.fetchSessions.sessions[brokerNode][sf.ID] = fs
			restoredFetchSessions++
			if sf.ID > maxFetchSessionID {
				maxFetchSessionID = sf.ID
			}
		}
	}
	if maxFetchSessionID > 0 {
		// Advance nextID past any saved IDs so we do not reuse one,
		// even for sessions we dropped as expired.
		c.fetchSessions.nextID.Store(maxFetchSessionID + 1)
	}
	if restoredFetchSessions > 0 || expiredFetchSessions > 0 {
		c.cfg.logger.Logf(LogLevelDebug, "loadSessionState: restored %d fetch sessions (%d expired) across %d brokers",
			restoredFetchSessions, expiredFetchSessions, len(ss.FetchSessions))
	}

	return nil
}

// closeOpenFiles closes all open file handles for persistence.
func (c *Cluster) closeOpenFiles() {
	c.groupsLogMu.Lock()
	if c.groupsLogFile != nil {
		if c.cfg.syncWrites {
			c.groupsLogFile.Sync()
		}
		c.groupsLogFile.Close()
		c.groupsLogFile = nil
	}
	c.groupsLogMu.Unlock()
	if c.pidsLogFile != nil {
		if c.cfg.syncWrites {
			c.pidsLogFile.Sync()
		}
		c.pidsLogFile.Close()
		c.pidsLogFile = nil
	}
	c.data.tps.each(func(_ string, _ int32, pd *partData) {
		pd.closeAllFiles(c.cfg.syncWrites)
	})
}
