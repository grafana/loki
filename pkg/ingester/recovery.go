package ingester

import (
	"context"
	io "io"
	"runtime"
	"sync"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/tsdb/record"
	"github.com/prometheus/prometheus/tsdb/wal"
)

type WALReader interface {
	Next() bool
	Err() error
	Record() []byte
}

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
	SetStream(userID string, series record.RefSeries) error
	Push(userID string, entries RefEntries) error
	Close()
	Done() <-chan struct{}
}

type ingesterRecoverer struct {
	// basically map[userID]map[fingerprint]*stream
	users sync.Map
	ing   *Ingester
	done  chan struct{}
}

func newIngesterRecoverer(i *Ingester) *ingesterRecoverer {
	return &ingesterRecoverer{
		ing:  i,
		done: make(chan struct{}),
	}
}

// Use all available cores
func (r *ingesterRecoverer) NumWorkers() int { return runtime.GOMAXPROCS(0) }

// SetStream is responsible for setting the key path for userIDs -> fingerprints -> streams.
// Internally, this uses nested sync.Maps due to their performance benefits for sets that only grow.
// Using these also allows us to bypass the ingester -> instance -> stream hierarchy internally, which
// may yield some performance gains, but is essential for the following:
// Due to the use of the instance's fingerprint mapper, stream fingerprints ARE NOT necessarily
// deterministic. The WAL uses the post-mapped fingerprint on the ingester that originally
// created the stream and we ensure that said fingerprint maps correctly to the newly
// created stream during WAL replay, even if the new in memory stream was assigned a different
// fingerprint from the mapper. This is paramount because subsequent WAL records will use
// the fingerprint reported in the WAL record, not the potentially differing one assigned during
// stream creation.
func (r *ingesterRecoverer) SetStream(userID string, series record.RefSeries) error {
	inst := r.ing.getOrCreateInstance(userID)

	stream, err := inst.getOrCreateStream(
		logproto.Stream{
			Labels: series.Labels.String(),
		},
		nil,
	)
	if err != nil {
		return err
	}

	// Now that we have the stream, ensure that the userID -> fingerprint -> stream
	// path is set properly.
	got, _ := r.users.LoadOrStore(userID, &sync.Map{})
	streamsMap := got.(*sync.Map)
	streamsMap.Store(series.Ref, stream)
	return nil
}

func (r *ingesterRecoverer) Push(userID string, entries RefEntries) error {
	out, ok := r.users.Load(userID)
	if !ok {
		return errors.Errorf("user (%s) not set during WAL replay", userID)
	}

	s, ok := out.(*sync.Map).Load(entries.Ref)
	if !ok {
		return errors.Errorf("stream (%d) not set during WAL replay for user (%s)", entries.Ref, userID)
	}

	return s.(*stream).Push(context.Background(), entries.Entries, nil)
}

func (r *ingesterRecoverer) Close() {
	close(r.done)
}

func (r *ingesterRecoverer) Done() <-chan struct{} {
	return r.done
}

/*
RecoverWAL recovers from WAL segments (not checkpoints).
Recovery strategy:
- loop over all records
  - create instance (user) if not exist
  - create all streams (no samples) if not exist

  - loop over all []RefEntries in the WALRecord
    - mod fp into worker pool
*/
func RecoverWAL(reader WALReader, recoverer Recoverer) error {
	defer recoverer.Close()

	var wg sync.WaitGroup
	var lastErr error
	nWorkers := recoverer.NumWorkers()

	if nWorkers < 1 {
		return errors.New("cannot recover WAL with no workers")
	}

	errCh := make(chan error)
	inputs := make([]chan recoveryInput, 0, nWorkers)
	wg.Add(nWorkers)
	for i := 0; i < nWorkers; i++ {
		inputs = append(inputs, make(chan recoveryInput))

		go processEntries(recoverer, inputs[i], errCh, &wg)
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
			if lastErr = recoverer.SetStream(rec.UserID, s); lastErr != nil {
				break outer
			}
		}

		for _, entries := range rec.RefEntries {
			worker := int(entries.Ref % uint64(nWorkers))
			select {
			case lastErr = <-errCh:
				break outer

			case inputs[worker] <- recoveryInput{
				userID:  rec.UserID,
				entries: entries,
			}:
			}
		}
	}

	for _, w := range inputs {
		close(w)
	}

	// may have broken loop early
	if lastErr != nil {
		return lastErr
	}

	finished := make(chan struct{})
	go func(finished chan<- struct{}) {
		wg.Wait()
		finished <- struct{}{}
	}(finished)

	select {
	case <-finished:
	case lastErr = <-errCh:
	}

	return lastErr
}

type recoveryInput struct {
	userID  string
	entries RefEntries
}

func processEntries(recoverer Recoverer, input <-chan recoveryInput, errCh chan<- error, wg *sync.WaitGroup) {
	defer wg.Done()
	for {
		select {
		case <-recoverer.Done():

		case next, ok := <-input:
			if !ok {
				return
			}
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
