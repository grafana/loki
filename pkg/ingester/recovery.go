package ingester

import (
	io "io"
	"runtime"
	"sync"

	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/tsdb/record"
	"github.com/prometheus/prometheus/tsdb/wal"
)

type WALReader interface {
	Next() bool
	Err() error
	Record() []byte
}

type MemoryWALReader struct {
	xs [][]byte
}

func NewMemoryWALReader(xs [][]byte) *MemoryWALReader {
	return &MemoryWALReader{
		xs: xs,
	}
}

func (m *MemoryWALReader) Next() bool {
	if len(m.xs) < 1 {
		return false
	}

	m.xs = m.xs[1:]
	return true
}

func (m *MemoryWALReader) Err() error { return nil }

func (m *MemoryWALReader) Record() []byte { return m.xs[0] }

// If startSegment is <0, it means all the segments.
func newWalReader(dir string, startSegment int) (*wal.Reader, io.Closer, error) {
	var (
		segmentReader io.ReadCloser
		err           error
	)
	if startSegment < 0 {
		segmentReader, err = wal.NewSegmentsReader(dir)
		if err != nil {
			return nil, nil, err
		}
	} else {
		first, last, err := wal.Segments(dir)
		if err != nil {
			return nil, nil, err
		}
		if startSegment > last {
			return nil, nil, errors.New("start segment is beyond the last WAL segment")
		}
		if first > startSegment {
			startSegment = first
		}
		segmentReader, err = wal.NewSegmentsRangeReader(wal.SegmentRange{
			Dir:   dir,
			First: startSegment,
			Last:  -1, // Till the end.
		})
		if err != nil {
			return nil, nil, err
		}
	}
	return wal.NewReader(segmentReader), segmentReader, nil
}

type Recoverer interface {
	NumWorkers() int
	GetOrCreateStream(userID string, series record.RefSeries) error
	Push(userID string, entries RefEntries) error
	Close()
	Done() <-chan struct{}
}

type ingesterRecoverer struct {
	m    sync.Map
	ing  *Ingester
	done chan struct{}
}

// Use all available cores
func (r *ingesterRecoverer) NumWorkers() int { return runtime.GOMAXPROCS(0) }

func (r *ingesterRecoverer) GetOrCreateStream(userID string, series record.RefSeries) error {}

func (r *ingesterRecoverer) Push(userID string, entries RefEntries) error {}

func (r *ingesterRecoverer) Close() {
	close(r.done)
}

func (r *ingesterRecoverer) Done() <-chan struct{} {
	return r.done
}

/*
Recovery strategy:
- loop over all records
  - create instance (user) if not exist
  - create all streams (no samples) if not exist

  - loop over all []RefEntries in the WALRecord
    - mod fp into worker pool
*/
func RecoverWAL(reader WALReader, recoverer Recoverer) error {
	defer recoverer.Close()

	var lastErr error
	nWorkers := recoverer.NumWorkers()

	if nWorkers < 1 {
		return errors.New("cannot recover WAL with no workers")
	}

	errCh := make(chan error)
	inputs := make([]chan recoveryInput, 0, nWorkers)
	for i := 0; i < nWorkers; i++ {
		inputs = append(inputs, make(chan recoveryInput))

		go processEntries(recoverer, inputs[i], errCh)
	}

outer:
	for reader.Next() {
		rec := recordPool.GetRecord()
		b := reader.Record()
		if lastErr = reader.Err(); lastErr != nil {
			break outer
		}

		if lastErr = decodeWALRecord(b, rec); lastErr != nil {
			break outer
		}

		// First process all series to ensure we don't write entries to nonexistant series.
		for _, s := range rec.Series {
			select {
			case <-recoverer.Done():
				break outer
			default:
			}

			if lastErr = recoverer.GetOrCreateStream(rec.UserID, s); lastErr != nil {
				break outer
			}
		}

		for _, entries := range rec.RefEntries {
			worker := int(entries.Ref % uint64(nWorkers))
			select {
			case lastErr = <-errCh:
				break outer
			case <-recoverer.Done():
				break outer
			case inputs[worker] <- recoveryInput{
				userID:  rec.UserID,
				entries: entries,
			}:
			}
		}
	}

	return lastErr
}

type recoveryInput struct {
	userID  string
	entries RefEntries
}

func processEntries(recoverer Recoverer, input <-chan recoveryInput, errCh chan<- error) {
	for {
		select {
		case <-recoverer.Done():

		case next := <-input:
			// Pass the error back, but respect the quit signal.
			if err := recoverer.Push(next.userID, next.entries); err != nil {
				select {
				case errCh <- err:
				case <-recoverer.Done():
				}
				return
			}
		}
	}
}
