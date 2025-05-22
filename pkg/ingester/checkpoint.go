package ingester

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/gogo/protobuf/proto"
	tsdb_errors "github.com/prometheus/prometheus/tsdb/errors"
	"github.com/prometheus/prometheus/tsdb/fileutil"
	"github.com/prometheus/prometheus/tsdb/wlog"
	prompool "github.com/prometheus/prometheus/util/pool"

	"github.com/grafana/loki/v3/pkg/chunkenc"
	"github.com/grafana/loki/v3/pkg/ingester/wal"
	"github.com/grafana/loki/v3/pkg/logproto"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
	"github.com/grafana/loki/v3/pkg/util/pool"
)

var (
	// todo(ctovena) those pools should be in factor of the actual configuration (blocksize, targetsize).
	// Starting with something sane first then we can refine with more experience.

	// Buckets [1KB 2KB 4KB 16KB 32KB  to 4MB] by 2
	chunksBufferPool = pool.NewBuffer(1024, 4*1024*1024, 2)
	// Buckets [64B 128B 256B 512B... to 2MB] by 2
	headBufferPool = pool.NewBuffer(64, 2*1024*1024, 2)
)

type chunkWithBuffer struct {
	blocks, head *bytes.Buffer
	Chunk
}

// The passed wireChunks slice is for re-use.
func toWireChunks(descs []chunkDesc, wireChunks []chunkWithBuffer) ([]chunkWithBuffer, error) {
	// release memory from previous list of chunks.
	for _, wc := range wireChunks {
		chunksBufferPool.Put(wc.blocks)
		headBufferPool.Put(wc.head)
		wc.Data = nil
		wc.Head = nil
	}

	if cap(wireChunks) < len(descs) {
		wireChunks = make([]chunkWithBuffer, len(descs))
	} else {
		wireChunks = wireChunks[:len(descs)]
	}

	for i, d := range descs {
		from, to := d.chunk.Bounds()
		chunkSize, headSize := d.chunk.CheckpointSize()

		wireChunk := chunkWithBuffer{
			Chunk: Chunk{
				From:        from,
				To:          to,
				Closed:      d.closed,
				FlushedAt:   d.flushed,
				LastUpdated: d.lastUpdated,
				Synced:      d.synced,
			},
			blocks: chunksBufferPool.Get(chunkSize),
			head:   headBufferPool.Get(headSize),
		}

		err := d.chunk.SerializeForCheckpointTo(
			wireChunk.blocks,
			wireChunk.head,
		)
		if err != nil {
			return nil, err
		}

		wireChunk.Data = wireChunk.blocks.Bytes()
		wireChunk.Head = wireChunk.head.Bytes()
		wireChunks[i] = wireChunk
	}
	return wireChunks, nil
}

func fromWireChunks(conf *Config, headfmt chunkenc.HeadBlockFmt, wireChunks []Chunk) ([]chunkDesc, error) {
	descs := make([]chunkDesc, 0, len(wireChunks))
	for _, c := range wireChunks {
		desc := chunkDesc{
			closed:      c.Closed,
			synced:      c.Synced,
			flushed:     c.FlushedAt,
			lastUpdated: c.LastUpdated,
		}

		mc, err := chunkenc.MemchunkFromCheckpoint(c.Data, c.Head, headfmt, conf.BlockSize, conf.TargetChunkSize)
		if err != nil {
			return nil, err
		}
		desc.chunk = mc

		descs = append(descs, desc)
	}
	return descs, nil
}

// nolint:interfacer
func decodeCheckpointRecord(rec []byte, s *Series) error {
	// TODO(owen-d): reduce allocs
	// The proto unmarshaling code will retain references to the underlying []byte it's passed
	// in order to reduce allocs. This is harmful to us because when reading from a WAL, the []byte
	// is only guaranteed to be valid between calls to Next().
	// Therefore, we copy it to avoid this problem.
	cpy := make([]byte, len(rec))
	copy(cpy, rec)

	switch wal.RecordType(cpy[0]) {
	case wal.CheckpointRecord:
		return proto.Unmarshal(cpy[1:], s)
	default:
		return fmt.Errorf("unexpected record type: %d", rec[0])
	}
}

func encodeWithTypeHeader(m *Series, typ wal.RecordType, buf []byte) ([]byte, error) {
	size := m.Size()
	if cap(buf) < size+1 {
		buf = make([]byte, size+1)
	}
	_, err := m.MarshalTo(buf[1 : size+1])
	if err != nil {
		return nil, err
	}
	buf[0] = byte(typ)
	return buf[:size+1], nil
}

type SeriesWithErr struct {
	Err    error
	Series *Series
}

type SeriesIter interface {
	Count() int
	Iter() *streamIterator
	Stop()
}

type ingesterSeriesIter struct {
	ing ingesterInstances

	done chan struct{}
}

type ingesterInstances interface {
	getInstances() []*instance
}

func newIngesterSeriesIter(ing ingesterInstances) *ingesterSeriesIter {
	return &ingesterSeriesIter{
		ing:  ing,
		done: make(chan struct{}),
	}
}

func (i *ingesterSeriesIter) Count() (ct int) {
	for _, inst := range i.ing.getInstances() {
		ct += inst.numStreams()
	}
	return ct
}

func (i *ingesterSeriesIter) Stop() {
	close(i.done)
}

func (i *ingesterSeriesIter) Iter() *streamIterator {
	return newStreamsIterator(i.ing)
}

type streamInstance struct {
	id      string
	streams []*stream
}

type streamIterator struct {
	instances []streamInstance

	current Series
	buffer  []chunkWithBuffer
	err     error
}

// newStreamsIterator returns a new stream iterators that iterates over one instance at a time, then
// each stream per instances.
func newStreamsIterator(ing ingesterInstances) *streamIterator {
	instances := ing.getInstances()
	streamInstances := make([]streamInstance, len(instances))
	for i, inst := range instances {
		streams := make([]*stream, 0, inst.streams.Len())
		_ = inst.forAllStreams(context.Background(), func(s *stream) error {
			streams = append(streams, s)
			return nil
		})
		streamInstances[i] = streamInstance{
			streams: streams,
			id:      inst.instanceID,
		}
	}
	return &streamIterator{
		instances: streamInstances,
	}
}

// Next loads the next stream of the current instance.
// If the instance is empty, it moves to the next instance until there is no more.
// Return true if there's a next stream, each successful calls will replace the current stream.
func (s *streamIterator) Next() bool {
	if len(s.instances) == 0 {
		s.instances = nil
		return false
	}
	currentInstance := s.instances[0]
	if len(currentInstance.streams) == 0 {
		s.instances = s.instances[1:]
		return s.Next()
	}

	// current stream
	stream := currentInstance.streams[0]

	// remove the first stream
	s.instances[0].streams = s.instances[0].streams[1:]

	stream.chunkMtx.RLock()
	defer stream.chunkMtx.RUnlock()

	if len(stream.chunks) < 1 {
		// it's possible the stream has been flushed to storage
		// in between starting the checkpointing process and
		// checkpointing this stream.
		return s.Next()
	}
	chunks, err := toWireChunks(stream.chunks, s.buffer)
	if err != nil {
		s.err = err
		return false
	}
	s.buffer = chunks

	s.current.Chunks = s.current.Chunks[:0]
	if cap(s.current.Chunks) == 0 {
		s.current.Chunks = make([]Chunk, 0, len(chunks))
	}

	for _, c := range chunks {
		s.current.Chunks = append(s.current.Chunks, c.Chunk)
	}

	s.current.UserID = currentInstance.id
	s.current.Fingerprint = uint64(stream.fp)
	s.current.Labels = logproto.FromLabelsToLabelAdapters(stream.labels)

	s.current.To = stream.lastLine.ts
	s.current.LastLine = stream.lastLine.content
	s.current.EntryCt = stream.entryCt
	s.current.HighestTs = stream.highestTs

	return true
}

// Err returns an errors thrown while iterating over the streams.
func (s *streamIterator) Error() error {
	return s.err
}

// Stream is serializable (for checkpointing) stream of chunks.
// NOTE: the series is re-used between successful Next calls.
// This means you should make a copy or use the data before calling Next.
func (s *streamIterator) Stream() *Series {
	return &s.current
}

type CheckpointWriter interface {
	// Advances current checkpoint, can also signal a no-op.
	Advance() (noop bool, err error)
	Write(*Series) error
	// Closes current checkpoint.
	Close(abort bool) error
}

type walLogger interface {
	Log(recs ...[]byte) error
	Close() error
	Dir() string
}

type WALCheckpointWriter struct {
	metrics    *ingesterMetrics
	segmentWAL *wlog.WL

	checkpointWAL walLogger
	lastSegment   int    // name of the last segment guaranteed to be covered by the checkpoint
	final         string // filename to atomically rotate upon completion
	bufSize       int
	recs          [][]byte
}

func (w *WALCheckpointWriter) Advance() (bool, error) {
	_, lastSegment, err := wlog.Segments(w.segmentWAL.Dir())
	if err != nil {
		return false, err
	}

	if lastSegment < 0 {
		// There are no WAL segments. No need of checkpoint yet.
		return true, nil
	}

	// First we advance the wal segment internally to ensure we don't overlap a previous checkpoint in
	// low throughput scenarios and to minimize segment replays on top of checkpoints.
	if _, err := w.segmentWAL.NextSegment(); err != nil {
		return false, err
	}

	// Checkpoint is named after the last WAL segment present so that when replaying the WAL
	// we can start from that particular WAL segment.
	checkpointDir := filepath.Join(w.segmentWAL.Dir(), fmt.Sprintf(checkpointPrefix+"%06d", lastSegment))
	level.Info(util_log.Logger).Log("msg", "attempting checkpoint for", "dir", checkpointDir)
	checkpointDirTemp := checkpointDir + ".tmp"

	// cleanup any old partial checkpoints
	if _, err := os.Stat(checkpointDirTemp); err == nil {
		if err := os.RemoveAll(checkpointDirTemp); err != nil {
			level.Error(util_log.Logger).Log("msg", "unable to cleanup old tmp checkpoint", "dir", checkpointDirTemp)
			return false, err
		}
	}

	if err := os.MkdirAll(checkpointDirTemp, 0750); err != nil {
		return false, fmt.Errorf("create checkpoint dir: %w", err)
	}

	checkpoint, err := wlog.NewSize(util_log.SlogFromGoKit(log.With(util_log.Logger, "component", "checkpoint_wal")), nil, checkpointDirTemp, walSegmentSize, wlog.CompressionNone)
	if err != nil {
		return false, fmt.Errorf("open checkpoint: %w", err)
	}

	w.checkpointWAL = checkpoint
	w.lastSegment = lastSegment
	w.final = checkpointDir

	return false, nil
}

// Buckets [64KB to 256MB] by 2
var recordBufferPool = prompool.New(1<<16, 1<<28, 2, func(size int) interface{} { return make([]byte, 0, size) })

func (w *WALCheckpointWriter) Write(s *Series) error {
	size := s.Size() + 1 // +1 for header
	buf := recordBufferPool.Get(size).([]byte)[:size]

	b, err := encodeWithTypeHeader(s, wal.CheckpointRecord, buf)
	if err != nil {
		return err
	}

	w.recs = append(w.recs, b)
	w.bufSize += len(b)
	level.Debug(util_log.Logger).Log("msg", "writing series", "size", humanize.Bytes(uint64(len(b))))

	// 1MB
	if w.bufSize > 1<<20 {
		if err := w.flush(); err != nil {
			return err
		}
	}
	return nil
}

func (w *WALCheckpointWriter) flush() error {
	level.Debug(util_log.Logger).Log("msg", "flushing series", "totalSize", humanize.Bytes(uint64(w.bufSize)), "series", len(w.recs))
	if err := w.checkpointWAL.Log(w.recs...); err != nil {
		return err
	}
	w.metrics.checkpointLoggedBytesTotal.Add(float64(w.bufSize))
	for _, b := range w.recs {
		recordBufferPool.Put(b)
	}
	w.recs = w.recs[:0]
	w.bufSize = 0
	return nil
}

const checkpointPrefix = "checkpoint."

var checkpointRe = regexp.MustCompile("^" + regexp.QuoteMeta(checkpointPrefix) + "(\\d+)(\\.tmp)?$")

// checkpointIndex returns the index of a given checkpoint file. It handles
// both regular and temporary checkpoints according to the includeTmp flag. If
// the file is not a checkpoint it returns an error.
func checkpointIndex(filename string, includeTmp bool) (int, error) {
	result := checkpointRe.FindStringSubmatch(filename)
	if len(result) < 2 {
		return 0, errors.New("file is not a checkpoint")
	}
	// Filter out temporary checkpoints if desired.
	if !includeTmp && len(result) == 3 && result[2] != "" {
		return 0, errors.New("temporary checkpoint")
	}
	return strconv.Atoi(result[1])
}

// lastCheckpoint returns the directory name and index of the most recent checkpoint.
// If dir does not contain any checkpoints, -1 is returned as index.
func lastCheckpoint(dir string) (string, int, error) {
	dirs, err := os.ReadDir(dir)
	if err != nil {
		return "", -1, err
	}
	var (
		maxIdx        = -1
		checkpointDir string
	)
	// There may be multiple checkpoints left, so select the one with max index.
	for i := 0; i < len(dirs); i++ {
		di := dirs[i]

		idx, err := checkpointIndex(di.Name(), false)
		if err != nil {
			continue
		}
		if !di.IsDir() {
			return "", -1, fmt.Errorf("checkpoint %s is not a directory", di.Name())
		}
		if idx > maxIdx {
			checkpointDir = di.Name()
			maxIdx = idx
		}
	}
	if maxIdx >= 0 {
		return filepath.Join(dir, checkpointDir), maxIdx, nil
	}
	return "", -1, nil
}

// deleteCheckpoints deletes all checkpoints in a directory which is < maxIndex.
func (w *WALCheckpointWriter) deleteCheckpoints(maxIndex int) (err error) {
	w.metrics.checkpointDeleteTotal.Inc()
	defer func() {
		if err != nil {
			w.metrics.checkpointDeleteFail.Inc()
		}
	}()

	errs := tsdb_errors.NewMulti()

	files, err := os.ReadDir(w.segmentWAL.Dir())
	if err != nil {
		return err
	}
	for _, fi := range files {
		index, err := checkpointIndex(fi.Name(), true)
		if err != nil || index >= maxIndex {
			continue
		}
		if err := os.RemoveAll(filepath.Join(w.segmentWAL.Dir(), fi.Name())); err != nil {
			errs.Add(err)
		}
	}
	return errs.Err()
}

func (w *WALCheckpointWriter) Close(abort bool) error {
	if len(w.recs) > 0 {
		if err := w.flush(); err != nil {
			return err
		}
	}
	if err := w.checkpointWAL.Close(); err != nil {
		return err
	}

	if abort {
		return os.RemoveAll(w.checkpointWAL.Dir())
	}

	if err := fileutil.Replace(w.checkpointWAL.Dir(), w.final); err != nil {
		return fmt.Errorf("rename checkpoint directory: %w", err)
	}
	level.Info(util_log.Logger).Log("msg", "atomic checkpoint finished", "old", w.checkpointWAL.Dir(), "new", w.final)
	// We delete the WAL segments which are before the previous checkpoint and not before the
	// current checkpoint created. This is because if the latest checkpoint is corrupted for any reason, we
	// should be able to recover from the older checkpoint which would need the older WAL segments.
	if err := w.segmentWAL.Truncate(w.lastSegment + 1); err != nil {
		// It is fine to have old WAL segments hanging around if deletion failed.
		// We can try again next time.
		level.Error(util_log.Logger).Log("msg", "error deleting old WAL segments", "err", err, "lastSegment", w.lastSegment)
	}

	if w.lastSegment >= 0 {
		if err := w.deleteCheckpoints(w.lastSegment); err != nil {
			// It is fine to have old checkpoints hanging around if deletion failed.
			// We can try again next time.
			level.Error(util_log.Logger).Log("msg", "error deleting old checkpoint", "err", err)
		}
	}

	return nil
}

type Checkpointer struct {
	dur     time.Duration
	iter    SeriesIter
	writer  CheckpointWriter
	metrics *ingesterMetrics

	quit <-chan struct{}
}

func NewCheckpointer(dur time.Duration, iter SeriesIter, writer CheckpointWriter, metrics *ingesterMetrics, quit <-chan struct{}) *Checkpointer {
	return &Checkpointer{
		dur:     dur,
		iter:    iter,
		writer:  writer,
		metrics: metrics,
		quit:    quit,
	}
}

func (c *Checkpointer) PerformCheckpoint() (err error) {
	noop, err := c.writer.Advance()
	if err != nil {
		return err
	}
	if noop {
		return nil
	}

	c.metrics.checkpointCreationTotal.Inc()
	defer func() {
		if err != nil {
			c.metrics.checkpointCreationFail.Inc()
		}
	}()
	// signal whether checkpoint writes should be amortized or burst
	var immediate bool
	n := c.iter.Count()
	if n < 1 {
		return c.writer.Close(false)
	}

	// Give a 10% buffer to the checkpoint duration in order to account for
	// new series, slow writes, etc.
	perSeriesDuration := (90 * c.dur) / (100 * time.Duration(n))

	ticker := time.NewTicker(perSeriesDuration)
	defer ticker.Stop()
	start := time.Now()
	defer func() {
		elapsed := time.Since(start)
		level.Info(util_log.Logger).Log("msg", "checkpoint done", "time", elapsed.String())
		c.metrics.checkpointDuration.Observe(elapsed.Seconds())
	}()

	iter := c.iter.Iter()
	for iter.Next() {
		if err := c.writer.Write(iter.Stream()); err != nil {
			return err
		}

		if !immediate {
			if time.Since(start) > c.dur {
				// This indicates the checkpoint is taking too long; stop waiting
				// and flush the remaining series as fast as possible.
				immediate = true
				continue
			}
		}

		select {
		case <-c.quit:
			return c.writer.Close(true)
		case <-ticker.C:
		}

	}

	if iter.Error() != nil {
		return iter.Error()
	}

	return c.writer.Close(false)
}

func (c *Checkpointer) Run() {
	ticker := time.NewTicker(c.dur)
	defer ticker.Stop()
	defer c.iter.Stop()

	for {
		select {
		case <-ticker.C:
			level.Info(util_log.Logger).Log("msg", "starting checkpoint")
			if err := c.PerformCheckpoint(); err != nil {
				level.Error(util_log.Logger).Log("msg", "error checkpointing series", "err", err)
				continue
			}
		case <-c.quit:
			return
		}
	}
}

func unflushedChunks(descs []chunkDesc) []chunkDesc {
	filtered := make([]chunkDesc, 0, len(descs))

	for _, d := range descs {
		if d.flushed.IsZero() {
			filtered = append(filtered, d)
		}
	}

	return filtered
}
