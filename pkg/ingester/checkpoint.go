package ingester

import (
	fmt "fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"time"

	"github.com/cortexproject/cortex/pkg/ingester/client"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/gogo/protobuf/proto"
	"github.com/pkg/errors"
	tsdb_errors "github.com/prometheus/prometheus/tsdb/errors"
	"github.com/prometheus/prometheus/tsdb/fileutil"
	"github.com/prometheus/prometheus/tsdb/wal"

	"github.com/grafana/loki/pkg/chunkenc"
)

// The passed wireChunks slice is for re-use.
func toWireChunks(descs []chunkDesc, wireChunks []Chunk) ([]Chunk, error) {
	if cap(wireChunks) < len(descs) {
		wireChunks = make([]Chunk, len(descs))
	} else {
		wireChunks = wireChunks[:len(descs)]
	}
	for i, d := range descs {
		from, to := d.chunk.Bounds()
		wireChunk := Chunk{
			From:        from,
			To:          to,
			Closed:      d.closed,
			FlushedAt:   d.flushed,
			LastUpdated: d.lastUpdated,
			Synced:      d.synced,
		}

		slice := wireChunks[i].Data[:0] // try to re-use the memory from last time
		if cap(slice) < d.chunk.CompressedSize() {
			slice = make([]byte, 0, d.chunk.CompressedSize())
		}

		chk, head, err := d.chunk.SerializeForCheckpoint(slice)
		if err != nil {
			return nil, err
		}

		wireChunk.Data = chk
		wireChunk.Head = head
		wireChunks[i] = wireChunk
	}
	return wireChunks, nil
}

func fromWireChunks(conf *Config, wireChunks []Chunk) ([]chunkDesc, error) {
	descs := make([]chunkDesc, 0, len(wireChunks))
	for _, c := range wireChunks {
		desc := chunkDesc{
			closed:      c.Closed,
			synced:      c.Synced,
			flushed:     c.FlushedAt,
			lastUpdated: c.LastUpdated,
		}

		mc, err := chunkenc.MemchunkFromCheckpoint(c.Data, c.Head, conf.BlockSize, conf.TargetChunkSize)
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
	//TODO(owen-d): reduce allocs
	// The proto unmarshaling code will retain references to the underlying []byte it's passed
	// in order to reduce allocs. This is harmful to us because when reading from a WAL, the []byte
	// is only guaranteed to be valid between calls to Next().
	// Therefore, we copy it to avoid this problem.
	cpy := make([]byte, len(rec))
	copy(cpy, rec)

	switch RecordType(cpy[0]) {
	case CheckpointRecord:
		return proto.Unmarshal(cpy[1:], s)
	default:
		return errors.Errorf("unexpected record type: %d", rec[0])
	}
}

func encodeWithTypeHeader(m proto.Message, typ RecordType) ([]byte, error) {
	buf, err := proto.Marshal(m)
	if err != nil {
		return nil, err
	}

	b := make([]byte, 0, len(buf)+1)
	b = append(b, byte(typ))
	b = append(b, buf...)
	return b, nil
}

type SeriesWithErr struct {
	Err    error
	Series *Series
}

type SeriesIter interface {
	Count() int
	Iter() <-chan *SeriesWithErr
	Stop()
}

type ingesterSeriesIter struct {
	ing *Ingester

	done chan struct{}
}

func newIngesterSeriesIter(ing *Ingester) *ingesterSeriesIter {
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

func (i *ingesterSeriesIter) Iter() <-chan *SeriesWithErr {
	ch := make(chan *SeriesWithErr)
	go func() {
		for _, inst := range i.ing.getInstances() {
			inst.streamsMtx.RLock()
			// Need to buffer streams internally so the read lock isn't held trying to write to a blocked channel.
			streams := make([]*stream, 0, len(inst.streams))
			inst.streamsMtx.RUnlock()
			_ = inst.forAllStreams(func(stream *stream) error {
				streams = append(streams, stream)
				return nil
			})

			for _, stream := range streams {
				stream.chunkMtx.RLock()
				if len(stream.chunks) < 1 {
					stream.chunkMtx.RUnlock()
					// it's possible the stream has been flushed to storage
					// in between starting the checkpointing process and
					// checkpointing this stream.
					continue
				}

				// TODO(owen-d): use a pool
				chunks, err := toWireChunks(stream.chunks, nil)
				stream.chunkMtx.RUnlock()

				var s *Series
				if err == nil {
					s = &Series{
						UserID:      inst.instanceID,
						Fingerprint: uint64(stream.fp),
						Labels:      client.FromLabelsToLabelAdapters(stream.labels),
						Chunks:      chunks,
					}
				}
				select {
				case ch <- &SeriesWithErr{
					Err:    err,
					Series: s,
				}:
				case <-i.done:
					return
				}
			}
		}
		close(ch)
	}()
	return ch
}

type CheckpointWriter interface {
	// Advances current checkpoint, can also signal a no-op.
	Advance() (noop bool, err error)
	Write(*Series) error
	// Closes current checkpoint.
	Close(abort bool) error
}

type WALCheckpointWriter struct {
	metrics    *ingesterMetrics
	segmentWAL *wal.WAL

	checkpointWAL *wal.WAL
	lastSegment   int    // name of the last segment guaranteed to be covered by the checkpoint
	final         string // filename to atomically rotate upon completion
	bufSize       int
	recs          [][]byte
}

func (w *WALCheckpointWriter) Advance() (bool, error) {
	_, lastSegment, err := wal.Segments(w.segmentWAL.Dir())
	if err != nil {
		return false, err
	}

	if lastSegment < 0 {
		// There are no WAL segments. No need of checkpoint yet.
		return true, nil
	}

	// First we advance the wal segment internally to ensure we don't overlap a previous checkpoint in
	// low throughput scenarios and to minimize segment replays on top of checkpoints.
	if err := w.segmentWAL.NextSegment(); err != nil {
		return false, err
	}

	// Checkpoint is named after the last WAL segment present so that when replaying the WAL
	// we can start from that particular WAL segment.
	checkpointDir := filepath.Join(w.segmentWAL.Dir(), fmt.Sprintf(checkpointPrefix+"%06d", lastSegment))
	level.Info(util.Logger).Log("msg", "attempting checkpoint for", "dir", checkpointDir)
	checkpointDirTemp := checkpointDir + ".tmp"

	// cleanup any old partial checkpoints
	if _, err := os.Stat(checkpointDirTemp); err == nil {
		if err := os.RemoveAll(checkpointDirTemp); err != nil {
			level.Error(util.Logger).Log("msg", "unable to cleanup old tmp checkpoint", "dir", checkpointDirTemp)
			return false, err
		}
	}

	if err := os.MkdirAll(checkpointDirTemp, 0777); err != nil {
		return false, errors.Wrap(err, "create checkpoint dir")
	}

	checkpoint, err := wal.NewSize(log.With(util.Logger, "component", "checkpoint_wal"), nil, checkpointDirTemp, walSegmentSize, false)
	if err != nil {
		return false, errors.Wrap(err, "open checkpoint")
	}

	w.checkpointWAL = checkpoint
	w.lastSegment = lastSegment
	w.final = checkpointDir

	return false, nil
}

func (w *WALCheckpointWriter) Write(s *Series) error {
	b, err := encodeWithTypeHeader(s, CheckpointRecord)
	if err != nil {
		return err
	}

	w.recs = append(w.recs, b)
	w.bufSize += len(b)

	// 1MB
	if w.bufSize > 1>>20 {
		if err := w.flush(); err != nil {
			return err
		}

	}
	return nil
}

func (w *WALCheckpointWriter) flush() error {
	if err := w.checkpointWAL.Log(w.recs...); err != nil {
		return err
	}
	w.metrics.checkpointLoggedBytesTotal.Add(float64(w.bufSize))
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
	dirs, err := ioutil.ReadDir(dir)
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

	files, err := ioutil.ReadDir(w.segmentWAL.Dir())
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
		return errors.Wrap(err, "rename checkpoint directory")
	}
	level.Info(util.Logger).Log("msg", "atomic checkpoint finished", "old", w.checkpointWAL.Dir(), "new", w.final)
	// We delete the WAL segments which are before the previous checkpoint and not before the
	// current checkpoint created. This is because if the latest checkpoint is corrupted for any reason, we
	// should be able to recover from the older checkpoint which would need the older WAL segments.
	if err := w.segmentWAL.Truncate(w.lastSegment + 1); err != nil {
		// It is fine to have old WAL segments hanging around if deletion failed.
		// We can try again next time.
		level.Error(util.Logger).Log("msg", "error deleting old WAL segments", "err", err, "lastSegment", w.lastSegment)
	}

	if w.lastSegment >= 0 {
		if err := w.deleteCheckpoints(w.lastSegment); err != nil {
			// It is fine to have old checkpoints hanging around if deletion failed.
			// We can try again next time.
			level.Error(util.Logger).Log("msg", "error deleting old checkpoint", "err", err)
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
		level.Info(util.Logger).Log("msg", "checkpoint done", "time", elapsed.String())
		c.metrics.checkpointDuration.Observe(elapsed.Seconds())
	}()
	for s := range c.iter.Iter() {
		if s.Err != nil {
			return s.Err
		}
		if err := c.writer.Write(s.Series); err != nil {
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

	return c.writer.Close(false)
}

func (c *Checkpointer) Run() {
	ticker := time.NewTicker(c.dur)
	defer ticker.Stop()
	defer c.iter.Stop()

	for {
		select {
		case <-ticker.C:
			level.Info(util.Logger).Log("msg", "starting checkpoint")
			if err := c.PerformCheckpoint(); err != nil {
				level.Error(util.Logger).Log("msg", "error checkpointing series", "err", err)
				continue
			}
		case <-c.quit:
			return
		}
	}
}
