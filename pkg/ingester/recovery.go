package ingester

import (
	"context"
	io "io"
	"runtime"
	"sync"

	"github.com/cortexproject/cortex/pkg/ingester/client"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/go-kit/kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/tsdb/record"
	"github.com/prometheus/prometheus/tsdb/wal"

	"github.com/grafana/loki/pkg/logproto"
)

type WALReader interface {
	Next() bool
	Err() error
	// Record should not be used across multiple calls to Next()
	Record() []byte
}

type NoopWALReader struct{}

func (NoopWALReader) Next() bool     { return false }
func (NoopWALReader) Err() error     { return nil }
func (NoopWALReader) Record() []byte { return nil }
func (NoopWALReader) Close() error   { return nil }

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

func newCheckpointReader(dir string) (WALReader, io.Closer, error) {
	lastCheckpointDir, idx, err := lastCheckpoint(dir)
	if err != nil {
		return nil, nil, err
	}
	if idx < 0 {
		level.Info(util.Logger).Log("msg", "no checkpoint found, treating as no-op")
		var reader NoopWALReader
		return reader, reader, nil
	}

	r, err := wal.NewSegmentsReader(lastCheckpointDir)
	if err != nil {
		return nil, nil, err
	}
	return wal.NewReader(r), r, nil
}

type Recoverer interface {
	NumWorkers() int
	Series(series *Series) error
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

func (r *ingesterRecoverer) Series(series *Series) error {
	inst := r.ing.getOrCreateInstance(series.UserID)

	// TODO(owen-d): create another fn to avoid unnecessary label type conversions.
	stream, err := inst.getOrCreateStream(logproto.Stream{
		Labels: client.FromLabelAdaptersToLabels(series.Labels).String(),
	}, true, nil)

	if err != nil {
		return err
	}

	added, err := stream.setChunks(series.Chunks)
	if err != nil {
		return err
	}
	r.ing.metrics.recoveredChunksTotal.Add(float64(len(series.Chunks)))
	r.ing.metrics.recoveredEntriesTotal.Add(float64(added))

	// now store the stream in the recovery map under the fingerprint originally recorded
	// as it's possible the newly mapped fingerprint is different. This is because the WAL records
	// will use this original reference.
	got, _ := r.users.LoadOrStore(series.UserID, &sync.Map{})
	streamsMap := got.(*sync.Map)
	streamsMap.Store(series.Fingerprint, stream)
	return nil
}

// SetStream is responsible for setting the key path for userIDs -> fingerprints -> streams.
// Internally, this uses nested sync.Maps due to their performance benefits for sets that only grow.
// Using these also allows us to bypass the ingester -> instance -> stream hierarchy internally, which
// may yield some performance gains, but is essential for the following:
// Due to the use of the instance's fingerprint mapper, stream fingerprints are NOT necessarily
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
		true,
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

	// ignore out of order errors here (it's possible for a checkpoint to already have data from the wal segments)
	_ = s.(*stream).Push(context.Background(), entries.Entries, nil)
	return nil
}

func (r *ingesterRecoverer) Close() {
	close(r.done)
}

func (r *ingesterRecoverer) Done() <-chan struct{} {
	return r.done
}

func RecoverWAL(reader WALReader, recoverer Recoverer) error {
	dispatch := func(recoverer Recoverer, b []byte, inputs []chan recoveryInput) error {
		rec := recordPool.GetRecord()
		if err := decodeWALRecord(b, rec); err != nil {
			return err
		}

		// First process all series to ensure we don't write entries to nonexistant series.
		var firstErr error
		for _, s := range rec.Series {
			if err := recoverer.SetStream(rec.UserID, s); err != nil {
				if firstErr == nil {
					firstErr = err
				}
			}

		}

		for _, entries := range rec.RefEntries {
			worker := int(entries.Ref % uint64(len(inputs)))
			inputs[worker] <- recoveryInput{
				userID: rec.UserID,
				data:   entries,
			}
		}

		return firstErr
	}

	process := func(recoverer Recoverer, input <-chan recoveryInput, errCh chan<- error) {
		for {
			select {
			case <-recoverer.Done():

			case next, ok := <-input:
				if !ok {
					return
				}
				entries, ok := next.data.(RefEntries)
				var err error
				if !ok {
					err = errors.Errorf("unexpected type (%T) when recovering WAL, expecting (%T)", next.data, entries)
				}
				if err == nil {
					err = recoverer.Push(next.userID, entries)
				}

				// Pass the error back, but respect the quit signal.
				if err != nil {
					errCh <- err
				}
			}
		}
	}

	return recoverGeneric(
		reader,
		recoverer,
		dispatch,
		process,
	)

}

func RecoverCheckpoint(reader WALReader, recoverer Recoverer) error {
	dispatch := func(recoverer Recoverer, b []byte, inputs []chan recoveryInput) error {
		s := &Series{}
		if err := decodeCheckpointRecord(b, s); err != nil {
			return err
		}

		worker := int(s.Fingerprint % uint64(len(inputs)))
		inputs[worker] <- recoveryInput{
			userID: s.UserID,
			data:   s,
		}
		return nil
	}

	process := func(recoverer Recoverer, input <-chan recoveryInput, errCh chan<- error) {
		for {
			select {
			case <-recoverer.Done():

			case next, ok := <-input:
				if !ok {
					return
				}
				series, ok := next.data.(*Series)
				var err error
				if !ok {
					err = errors.Errorf("unexpected type (%T) when recovering WAL, expecting (%T)", next.data, series)
				}
				if err == nil {
					err = recoverer.Series(series)
				}

				if err != nil {
					errCh <- err
				}
			}
		}
	}

	return recoverGeneric(
		reader,
		recoverer,
		dispatch,
		process,
	)
}

type recoveryInput struct {
	userID string
	data   interface{}
}

// recoverGeneric enables reusing the ability to recover from WALs of different types
// by exposing the dispatch and process functions.
// Note: it explicitly does not call the Recoverer.Close function as it's possible to layer
// multiple recoveries on top of each other, as in the case of recovering from Checkpoints
// then the WAL.
func recoverGeneric(
	reader WALReader,
	recoverer Recoverer,
	dispatch func(Recoverer, []byte, []chan recoveryInput) error,
	process func(Recoverer, <-chan recoveryInput, chan<- error),
) error {
	var wg sync.WaitGroup
	var firstErr error
	nWorkers := recoverer.NumWorkers()

	if nWorkers < 1 {
		return errors.New("cannot recover with no workers")
	}

	errCh := make(chan error)
	inputs := make([]chan recoveryInput, 0, nWorkers)
	wg.Add(nWorkers)
	for i := 0; i < nWorkers; i++ {
		inputs = append(inputs, make(chan recoveryInput))

		go func(input <-chan recoveryInput) {
			defer wg.Done()
			process(recoverer, input, errCh)
		}(inputs[i])

	}

	go func() {
		for reader.Next() {
			b := reader.Record()
			if err := reader.Err(); err != nil {
				errCh <- err
				continue
			}

			if err := dispatch(recoverer, b, inputs); err != nil {
				errCh <- err
				continue
			}
		}

		for _, w := range inputs {
			close(w)
		}
	}()

	finished := make(chan struct{})
	go func(finished chan<- struct{}) {
		wg.Wait()
		finished <- struct{}{}
	}(finished)

	for {
		select {
		case <-finished:
			return firstErr
		case err := <-errCh:
			if firstErr == nil {
				firstErr = err
			}
		}
	}
}
