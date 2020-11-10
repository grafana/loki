package ingester

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"time"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/go-kit/kit/log/level"
	"github.com/gogo/protobuf/proto"
	"github.com/pkg/errors"
	tsdb_errors "github.com/prometheus/prometheus/tsdb/errors"
	"github.com/prometheus/prometheus/tsdb/fileutil"
	"github.com/prometheus/prometheus/tsdb/wal"

	"github.com/grafana/loki/pkg/chunkenc"
)

// The passed wireChunks slice is for re-use.
func toWireChunks(descs []*chunkDesc, wireChunks []Chunk) ([]Chunk, error) {
	if cap(wireChunks) < len(descs) {
		wireChunks = make([]Chunk, len(descs))
	} else {
		wireChunks = wireChunks[:len(descs)]
	}
	for i, d := range descs {
		from, to := d.chunk.Bounds()
		wireChunk := Chunk{
			From:      from,
			To:        to,
			Closed:    d.closed,
			FlushedAt: d.flushed,
		}

		slice := wireChunks[i].Data[:0] // try to re-use the memory from last time
		if cap(slice) < d.chunk.CompressedSize() {
			slice = make([]byte, 0, d.chunk.CompressedSize())
		}

		out, err := d.chunk.BytesWith(slice)
		if err != nil {
			return nil, err
		}

		wireChunk.Data = out
		wireChunks[i] = wireChunk
	}
	return wireChunks, nil
}

func fromWireChunks(conf *Config, wireChunks []Chunk) ([]*chunkDesc, error) {
	descs := make([]*chunkDesc, 0, len(wireChunks))
	for _, c := range wireChunks {
		desc := &chunkDesc{
			closed:      c.Closed,
			flushed:     c.FlushedAt,
			lastUpdated: time.Now(),
		}

		mc, err := chunkenc.NewByteChunk(c.Data, conf.BlockSize, conf.TargetChunkSize)
		if err != nil {
			return nil, err
		}
		desc.chunk = mc

		descs = append(descs, desc)
	}
	return descs, nil
}

func decodeCheckpointRecord(rec []byte, s *Series) error {
	switch RecordType(rec[0]) {
	case CheckpointRecord:
		return proto.Unmarshal(rec[1:], s)
	default:
		return errors.Errorf("unexpected record type: %d", rec[0])
	}
}

func encodeWithTypeHeader(m proto.Message, typ RecordType, b []byte) ([]byte, error) {
	buf, err := proto.Marshal(m)
	if err != nil {
		return b, err
	}

	b = append(b[:0], byte(typ))
	b = append(b, buf...)
	return b, nil
}

type SeriesIter interface {
	Num() int
	Iter() <-chan *Series
	Stop()
}

type CheckpointWriter interface {
	Write(*Series) error
	Close() error
}

type WALCheckpointWriter struct {
	cfg           WALConfig
	metrics       *ingesterMetrics
	checkpointWAL *wal.WAL
	segmentWAL    *wal.WAL

	lastSegment int    // name of the last segment guaranteed to be covered by the checkpoint
	final       string // filename to atomically rotate upon completion
	start       time.Time
	bufSize     int
	recs        [][]byte
}

func (w *WALCheckpointWriter) Write(s *Series) error {
	b, err := encodeWithTypeHeader(s, CheckpointRecord, recordPool.GetBytes()[:0])
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

// deleteCheckpoints deletes all checkpoints in a directory which is < maxIndex.
func (w *WALCheckpointWriter) deleteCheckpoints(maxIndex int) (err error) {
	w.metrics.checkpointDeleteTotal.Inc()
	defer func() {
		if err != nil {
			w.metrics.checkpointDeleteFail.Inc()
		}
	}()

	var errs tsdb_errors.MultiError

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

func (w *WALCheckpointWriter) Close() error {
	defer func() {
		elapsed := time.Since(w.start)
		level.Info(util.Logger).Log("msg", "checkpoint done", "time", elapsed.String())
		w.metrics.checkpointDuration.Observe(elapsed.Seconds())
	}()
	if len(w.recs) > 0 {
		if err := w.flush(); err != nil {
			return err
		}
	}
	if err := w.checkpointWAL.Close(); err != nil {
		return err
	}

	if err := fileutil.Replace(w.checkpointWAL.Dir(), w.final); err != nil {
		return errors.Wrap(err, "rename checkpoint directory")
	}
	// We delete the WAL segments which are before the previous checkpoint and not before the
	// current checkpoint created. This is because if the latest checkpoint is corrupted for any reason, we
	// should be able to recover from the older checkpoint which would need the older WAL segments.
	if err := w.segmentWAL.Truncate(w.lastSegment); err != nil {
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

	quit chan struct{}
}

func NewCheckpointer(dur time.Duration, iter SeriesIter, writer CheckpointWriter, metrics *ingesterMetrics) *Checkpointer {
	return &Checkpointer{
		dur:    dur,
		iter:   iter,
		writer: writer,
		quit:   make(chan struct{}),
	}
}

func (c *Checkpointer) PerformCheckpoint() (err error) {
	c.metrics.checkpointCreationTotal.Inc()
	defer func() {
		if err != nil {
			c.metrics.checkpointCreationFail.Inc()
		}
	}()
	// signal whether checkpoint writes should be amortized or burst
	var immediate bool
	n := c.iter.Num()
	if n < 1 {
		return nil
	}

	perSeriesDuration := (95 * c.dur) / (100 * time.Duration(n))

	ticker := time.NewTicker(perSeriesDuration)
	defer ticker.Stop()
	start := time.Now()
	for s := range c.iter.Iter() {
		if err := c.writer.Write(s); err != nil {
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
		case <-ticker.C:
		}

	}

	return c.writer.Close()
}
