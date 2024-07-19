package ingester

import (
	"errors"
	"fmt"
	"io"
	"runtime"
	"sync"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/prometheus/tsdb/chunks"
	"github.com/prometheus/prometheus/tsdb/record"
	"github.com/prometheus/prometheus/tsdb/wlog"
	"golang.org/x/net/context"

	"github.com/grafana/loki/v3/pkg/ingester/wal"
	"github.com/grafana/loki/v3/pkg/logproto"
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

func newCheckpointReader(dir string, logger log.Logger) (WALReader, io.Closer, error) {
	lastCheckpointDir, idx, err := lastCheckpoint(dir)
	if err != nil {
		return nil, nil, err
	}
	if idx < 0 {
		level.Info(logger).Log("msg", "no checkpoint found, treating as no-op")
		var reader NoopWALReader
		return reader, reader, nil
	}

	r, err := wlog.NewSegmentsReader(lastCheckpointDir)
	if err != nil {
		return nil, nil, err
	}
	return wlog.NewReader(r), r, nil
}

type Recoverer interface {
	NumWorkers() int
	Series(series *Series) error
	SetStream(ctx context.Context, userID string, series record.RefSeries) error
	Push(userID string, entries wal.RefEntries) error
	Done() <-chan struct{}
}

type ingesterRecoverer struct {
	// basically map[userID]map[fingerprint]*stream
	users  sync.Map
	ing    *Ingester
	logger log.Logger
	done   chan struct{}
}

func newIngesterRecoverer(i *Ingester) *ingesterRecoverer {

	return &ingesterRecoverer{
		ing:    i,
		done:   make(chan struct{}),
		logger: i.logger,
	}
}

// Use all available cores
func (r *ingesterRecoverer) NumWorkers() int { return runtime.GOMAXPROCS(0) }

func (r *ingesterRecoverer) Series(series *Series) error {
	return r.ing.replayController.WithBackPressure(func() error {

		inst, err := r.ing.GetOrCreateInstance(series.UserID)
		if err != nil {
			return err
		}

		// TODO(owen-d): create another fn to avoid unnecessary label type conversions.
		stream, err := inst.getOrCreateStream(context.Background(), logproto.Stream{
			Labels: logproto.FromLabelAdaptersToLabels(series.Labels).String(),
		}, nil)

		if err != nil {
			return err
		}

		bytesAdded, entriesAdded, err := stream.setChunks(series.Chunks)
		stream.lastLine.ts = series.To
		stream.lastLine.content = series.LastLine
		stream.entryCt = series.EntryCt
		stream.highestTs = series.HighestTs

		if err != nil {
			return err
		}
		r.ing.metrics.memoryChunks.Add(float64(len(series.Chunks)))
		r.ing.metrics.recoveredChunksTotal.Add(float64(len(series.Chunks)))
		r.ing.metrics.recoveredEntriesTotal.Add(float64(entriesAdded))
		r.ing.replayController.Add(int64(bytesAdded))

		// now store the stream in the recovery map under the fingerprint originally recorded
		// as it's possible the newly mapped fingerprint is different. This is because the WAL records
		// will use this original reference.
		got, _ := r.users.LoadOrStore(series.UserID, &sync.Map{})
		streamsMap := got.(*sync.Map)
		streamsMap.Store(chunks.HeadSeriesRef(series.Fingerprint), stream)

		return nil
	})
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
func (r *ingesterRecoverer) SetStream(ctx context.Context, userID string, series record.RefSeries) error {
	inst, err := r.ing.GetOrCreateInstance(userID)
	if err != nil {
		return err
	}

	stream, err := inst.getOrCreateStream(
		ctx,
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

func (r *ingesterRecoverer) Push(userID string, entries wal.RefEntries) error {
	return r.ing.replayController.WithBackPressure(func() error {
		out, ok := r.users.Load(userID)
		if !ok {
			return fmt.Errorf("user (%s) not set during WAL replay", userID)
		}

		s, ok := out.(*sync.Map).Load(entries.Ref)
		if !ok {
			return fmt.Errorf("stream (%d) not set during WAL replay for user (%s)", entries.Ref, userID)
		}

		// ignore out of order errors here (it's possible for a checkpoint to already have data from the wal segments)
		bytesAdded, err := s.(*stream).Push(context.Background(), entries.Entries, nil, entries.Counter, true, false, r.ing.customStreamsTracker)
		r.ing.replayController.Add(int64(bytesAdded))
		if err != nil && err == ErrEntriesExist {
			r.ing.metrics.duplicateEntriesTotal.Add(float64(len(entries.Entries)))
		}
		return nil
	})
}

func (r *ingesterRecoverer) Close() {
	// Ensure this is only run once.
	select {
	case <-r.done:
		return
	default:
	}

	close(r.done)

	// Enable the limiter here to accurately reflect tenant limits after recovery.
	r.ing.limiter.Enable()

	for _, inst := range r.ing.getInstances() {
		inst.forAllStreams(context.Background(), func(s *stream) error {
			s.chunkMtx.Lock()
			defer s.chunkMtx.Unlock()

			// reset all the incrementing stream counters after a successful WAL replay.
			s.resetCounter()

			if len(s.chunks) == 0 {
				inst.removeStream(s)
				return nil
			}

			// If we've replayed a WAL with unordered writes, but the new
			// configuration disables them, convert all streams/head blocks
			// to ensure unordered writes are disabled after the replay,
			// but without dropping any previously accepted data.
			isAllowed := r.ing.limiter.UnorderedWrites(s.tenant)
			old := s.unorderedWrites
			s.unorderedWrites = isAllowed

			if !isAllowed && old {
				err := s.chunks[len(s.chunks)-1].chunk.ConvertHead(headBlockType(s.chunkFormat, isAllowed))
				if err != nil {
					level.Warn(r.logger).Log(
						"msg", "error converting headblock",
						"err", err.Error(),
						"stream", s.labels.String(),
						"component", "ingesterRecoverer",
					)
				}
			}

			return nil
		})
	}
}

func (r *ingesterRecoverer) Done() <-chan struct{} {
	return r.done
}

func RecoverWAL(ctx context.Context, reader WALReader, recoverer Recoverer) error {
	dispatch := func(recoverer Recoverer, b []byte, inputs []chan recoveryInput) error {
		rec := recordPool.GetRecord()
		if err := wal.DecodeRecord(b, rec); err != nil {
			return err
		}

		// First process all series to ensure we don't write entries to nonexistant series.
		var firstErr error
		for _, s := range rec.Series {
			if err := recoverer.SetStream(ctx, rec.UserID, s); err != nil {
				if firstErr == nil {
					firstErr = err
				}
			}

		}

		for _, entries := range rec.RefEntries {
			worker := int(uint64(entries.Ref) % uint64(len(inputs)))
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
				entries, ok := next.data.(wal.RefEntries)
				var err error
				if !ok {
					err = fmt.Errorf("unexpected type (%T) when recovering WAL, expecting (%T)", next.data, entries)
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
	dispatch := func(_ Recoverer, b []byte, inputs []chan recoveryInput) error {
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
					err = fmt.Errorf("unexpected type (%T) when recovering WAL, expecting (%T)", next.data, series)
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
