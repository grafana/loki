package ingester

import (
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-kit/kit/log/level"
	"github.com/golang/protobuf/proto"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/common/model"
	tsdb_errors "github.com/prometheus/prometheus/tsdb/errors"
	"github.com/prometheus/prometheus/tsdb/fileutil"
	"github.com/prometheus/prometheus/tsdb/wal"

	"github.com/cortexproject/cortex/pkg/ingester/client"
	"github.com/cortexproject/cortex/pkg/util"
)

// WALConfig is config for the Write Ahead Log.
type WALConfig struct {
	WALEnabled         bool          `yaml:"wal_enabled"`
	CheckpointEnabled  bool          `yaml:"checkpoint_enabled"`
	Recover            bool          `yaml:"recover_from_wal"`
	Dir                string        `yaml:"wal_dir"`
	CheckpointDuration time.Duration `yaml:"checkpoint_duration"`
}

// RegisterFlags adds the flags required to config this to the given FlagSet
func (cfg *WALConfig) RegisterFlags(f *flag.FlagSet) {
	f.StringVar(&cfg.Dir, "ingester.wal-dir", "wal", "Directory to store the WAL and/or recover from WAL.")
	f.BoolVar(&cfg.Recover, "ingester.recover-from-wal", false, "Recover data from existing WAL irrespective of WAL enabled/disabled.")
	f.BoolVar(&cfg.WALEnabled, "ingester.wal-enabled", false, "Enable writing of ingested data into WAL.")
	f.BoolVar(&cfg.CheckpointEnabled, "ingester.checkpoint-enabled", true, "Enable checkpointing of in-memory chunks. It should always be true when using normally. Set it to false iff you are doing some small tests as there is no mechanism to delete the old WAL yet if checkpoint is disabled.")
	f.DurationVar(&cfg.CheckpointDuration, "ingester.checkpoint-duration", 30*time.Minute, "Interval at which checkpoints should be created.")
}

// WAL interface allows us to have a no-op WAL when the WAL is disabled.
type WAL interface {
	// Log marshalls the records and writes it into the WAL.
	Log(*Record) error
	// Stop stops all the WAL operations.
	Stop()
}

type noopWAL struct{}

func (noopWAL) Log(*Record) error { return nil }
func (noopWAL) Stop()             {}

type walWrapper struct {
	cfg  WALConfig
	quit chan struct{}
	wait sync.WaitGroup

	wal           *wal.WAL
	getUserStates func() map[string]*userState
	checkpointMtx sync.Mutex

	// Checkpoint metrics.
	checkpointDeleteFail    prometheus.Counter
	checkpointDeleteTotal   prometheus.Counter
	checkpointCreationFail  prometheus.Counter
	checkpointCreationTotal prometheus.Counter
	checkpointDuration      prometheus.Summary
}

// newWAL creates a WAL object. If the WAL is disabled, then the returned WAL is a no-op WAL.
func newWAL(cfg WALConfig, userStatesFunc func() map[string]*userState, registerer prometheus.Registerer) (WAL, error) {
	if !cfg.WALEnabled {
		return &noopWAL{}, nil
	}

	util.WarnExperimentalUse("Chunks WAL")

	var walRegistry prometheus.Registerer
	if registerer != nil {
		walRegistry = prometheus.WrapRegistererWith(prometheus.Labels{"kind": "wal"}, registerer)
	}
	tsdbWAL, err := wal.NewSize(util.Logger, walRegistry, cfg.Dir, wal.DefaultSegmentSize/4, true)
	if err != nil {
		return nil, err
	}

	w := &walWrapper{
		cfg:           cfg,
		quit:          make(chan struct{}),
		wal:           tsdbWAL,
		getUserStates: userStatesFunc,
	}

	w.checkpointDeleteFail = promauto.With(registerer).NewCounter(prometheus.CounterOpts{
		Name: "cortex_ingester_checkpoint_deletions_failed_total",
		Help: "Total number of checkpoint deletions that failed.",
	})
	w.checkpointDeleteTotal = promauto.With(registerer).NewCounter(prometheus.CounterOpts{
		Name: "cortex_ingester_checkpoint_deletions_total",
		Help: "Total number of checkpoint deletions attempted.",
	})
	w.checkpointCreationFail = promauto.With(registerer).NewCounter(prometheus.CounterOpts{
		Name: "cortex_ingester_checkpoint_creations_failed_total",
		Help: "Total number of checkpoint creations that failed.",
	})
	w.checkpointCreationTotal = promauto.With(registerer).NewCounter(prometheus.CounterOpts{
		Name: "cortex_ingester_checkpoint_creations_total",
		Help: "Total number of checkpoint creations attempted.",
	})
	w.checkpointDuration = promauto.With(registerer).NewSummary(prometheus.SummaryOpts{
		Name:       "cortex_ingester_checkpoint_duration_seconds",
		Help:       "Time taken to create a checkpoint.",
		Objectives: map[float64]float64{0.5: 0.05, 0.9: 0.01, 0.99: 0.001},
	})

	w.wait.Add(1)
	go w.run()
	return w, nil
}

func (w *walWrapper) Stop() {
	close(w.quit)
	w.wait.Wait()
	w.wal.Close()
}

func (w *walWrapper) Log(record *Record) error {
	select {
	case <-w.quit:
		return nil
	default:
		if record == nil {
			return nil
		}
		buf, err := proto.Marshal(record)
		if err != nil {
			return err
		}
		return w.wal.Log(buf)
	}
}

func (w *walWrapper) run() {
	defer w.wait.Done()

	if !w.cfg.CheckpointEnabled {
		return
	}

	ticker := time.NewTicker(w.cfg.CheckpointDuration)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			start := time.Now()
			level.Info(util.Logger).Log("msg", "starting checkpoint")
			if err := w.performCheckpoint(false); err != nil {
				level.Error(util.Logger).Log("msg", "error checkpointing series", "err", err)
				continue
			}
			elapsed := time.Since(start)
			level.Info(util.Logger).Log("msg", "checkpoint done", "time", elapsed.String())
			w.checkpointDuration.Observe(elapsed.Seconds())
		case <-w.quit:
			level.Info(util.Logger).Log("msg", "creating checkpoint before shutdown")
			if err := w.performCheckpoint(true); err != nil {
				level.Error(util.Logger).Log("msg", "error checkpointing series during shutdown", "err", err)
			}
			return
		}
	}
}

const checkpointPrefix = "checkpoint."

func (w *walWrapper) performCheckpoint(immediate bool) (err error) {
	if !w.cfg.CheckpointEnabled {
		return nil
	}

	// This method is called during shutdown which can interfere with ongoing checkpointing.
	// Hence to avoid any race between file creation and WAL truncation, we hold this lock here.
	w.checkpointMtx.Lock()
	defer w.checkpointMtx.Unlock()

	w.checkpointCreationTotal.Inc()
	defer func() {
		if err != nil {
			w.checkpointCreationFail.Inc()
		}
	}()

	if w.getUserStates == nil {
		return errors.New("function to get user states not initialised")
	}

	_, lastSegment, err := w.wal.Segments()
	if err != nil {
		return err
	}
	if lastSegment < 0 {
		// There are no WAL segments. No need of checkpoint yet.
		return nil
	}

	_, lastCh, err := lastCheckpoint(w.wal.Dir())
	if err != nil {
		return err
	}

	if lastCh == lastSegment {
		// As the checkpoint name is taken from last WAL segment, we need to ensure
		// a new segment for every checkpoint so that the old checkpoint is not overwritten.
		if err := w.wal.NextSegment(); err != nil {
			return err
		}

		_, lastSegment, err = w.wal.Segments()
		if err != nil {
			return err
		}
	}

	// Checkpoint is named after the last WAL segment present so that when replaying the WAL
	// we can start from that particular WAL segment.
	checkpointDir := filepath.Join(w.wal.Dir(), fmt.Sprintf(checkpointPrefix+"%06d", lastSegment))
	level.Info(util.Logger).Log("msg", "attempting checkpoint for", "dir", checkpointDir)
	checkpointDirTemp := checkpointDir + ".tmp"

	if err := os.MkdirAll(checkpointDirTemp, 0777); err != nil {
		return errors.Wrap(err, "create checkpoint dir")
	}
	checkpoint, err := wal.New(nil, nil, checkpointDirTemp, true)
	if err != nil {
		return errors.Wrap(err, "open checkpoint")
	}
	defer func() {
		checkpoint.Close()
		os.RemoveAll(checkpointDirTemp)
	}()

	// Count number of series - we'll use this to rate limit checkpoints.
	numSeries := 0
	us := w.getUserStates()
	for _, state := range us {
		numSeries += state.fpToSeries.length()
	}
	if numSeries == 0 {
		return nil
	}

	var ticker *time.Ticker
	if !immediate {
		perSeriesDuration := w.cfg.CheckpointDuration / time.Duration(numSeries)
		ticker = time.NewTicker(perSeriesDuration)
		defer ticker.Stop()
	}

	var wireChunkBuf []client.Chunk
	for userID, state := range us {
		for pair := range state.fpToSeries.iter() {
			state.fpLocker.Lock(pair.fp)
			wireChunkBuf, err = w.checkpointSeries(checkpoint, userID, pair.fp, pair.series, wireChunkBuf)
			state.fpLocker.Unlock(pair.fp)
			if err != nil {
				return err
			}

			if !immediate {
				select {
				case <-ticker.C:
				case <-w.quit: // When we're trying to shutdown, finish the checkpoint as fast as possible.
				}
			}
		}
	}

	if err := checkpoint.Close(); err != nil {
		return errors.Wrap(err, "close checkpoint")
	}
	if err := fileutil.Replace(checkpointDirTemp, checkpointDir); err != nil {
		return errors.Wrap(err, "rename checkpoint directory")
	}

	// We delete the WAL segments which are before the previous checkpoint and not before the
	// current checkpoint created. This is because if the latest checkpoint is corrupted for any reason, we
	// should be able to recover from the older checkpoint which would need the older WAL segments.
	if err := w.wal.Truncate(lastCh); err != nil {
		// It is fine to have old WAL segments hanging around if deletion failed.
		// We can try again next time.
		level.Error(util.Logger).Log("msg", "error deleting old WAL segments", "err", err)
	}

	if lastCh >= 0 {
		if err := w.deleteCheckpoints(lastCh); err != nil {
			// It is fine to have old checkpoints hanging around if deletion failed.
			// We can try again next time.
			level.Error(util.Logger).Log("msg", "error deleting old checkpoint", "err", err)
		}
	}

	return nil
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

		if !strings.HasPrefix(di.Name(), checkpointPrefix) {
			continue
		}
		if !di.IsDir() {
			return "", -1, fmt.Errorf("checkpoint %s is not a directory", di.Name())
		}
		idx, err := strconv.Atoi(di.Name()[len(checkpointPrefix):])
		if err != nil {
			continue
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
func (w *walWrapper) deleteCheckpoints(maxIndex int) (err error) {
	w.checkpointDeleteTotal.Inc()
	defer func() {
		if err != nil {
			w.checkpointDeleteFail.Inc()
		}
	}()

	var errs tsdb_errors.MultiError

	files, err := ioutil.ReadDir(w.wal.Dir())
	if err != nil {
		return err
	}
	for _, fi := range files {
		if !strings.HasPrefix(fi.Name(), checkpointPrefix) {
			continue
		}
		index, err := strconv.Atoi(fi.Name()[len(checkpointPrefix):])
		if err != nil || index >= maxIndex {
			continue
		}
		if err := os.RemoveAll(filepath.Join(w.wal.Dir(), fi.Name())); err != nil {
			errs.Add(err)
		}
	}
	return errs.Err()
}

// checkpointSeries write the chunks of the series to the checkpoint.
func (w *walWrapper) checkpointSeries(cp *wal.WAL, userID string, fp model.Fingerprint, series *memorySeries, wireChunks []client.Chunk) ([]client.Chunk, error) {
	var err error
	wireChunks, err = toWireChunks(series.chunkDescs, wireChunks[:0])
	if err != nil {
		return wireChunks, err
	}

	buf, err := proto.Marshal(&Series{
		UserId:      userID,
		Fingerprint: uint64(fp),
		Labels:      client.FromLabelsToLabelAdapters(series.metric),
		Chunks:      wireChunks,
	})
	if err != nil {
		return wireChunks, err
	}

	return wireChunks, cp.Log(buf)
}

type walRecoveryParameters struct {
	walDir      string
	ingester    *Ingester
	numWorkers  int
	stateCache  []map[string]*userState
	seriesCache []map[string]map[uint64]*memorySeries
}

func recoverFromWAL(ingester *Ingester) error {
	params := walRecoveryParameters{
		walDir:     ingester.cfg.WALConfig.Dir,
		numWorkers: runtime.GOMAXPROCS(0),
		ingester:   ingester,
	}

	params.stateCache = make([]map[string]*userState, params.numWorkers)
	params.seriesCache = make([]map[string]map[uint64]*memorySeries, params.numWorkers)
	for i := 0; i < params.numWorkers; i++ {
		params.stateCache[i] = make(map[string]*userState)
		params.seriesCache[i] = make(map[string]map[uint64]*memorySeries)
	}

	level.Info(util.Logger).Log("msg", "recovering from checkpoint")
	start := time.Now()
	userStates, idx, err := processCheckpointWithRepair(params)
	if err != nil {
		return err
	}
	elapsed := time.Since(start)
	level.Info(util.Logger).Log("msg", "recovered from checkpoint", "time", elapsed.String())

	if segExists, err := segmentsExist(params.walDir); err == nil && !segExists {
		level.Info(util.Logger).Log("msg", "no segments found, skipping recover from segments")
		ingester.userStatesMtx.Lock()
		ingester.userStates = userStates
		ingester.userStatesMtx.Unlock()
		return nil
	} else if err != nil {
		return err
	}

	level.Info(util.Logger).Log("msg", "recovering from WAL", "dir", params.walDir, "start_segment", idx)
	start = time.Now()
	if err := processWALWithRepair(idx, userStates, params); err != nil {
		return err
	}
	elapsed = time.Since(start)
	level.Info(util.Logger).Log("msg", "recovered from WAL", "time", elapsed.String())

	ingester.userStatesMtx.Lock()
	ingester.userStates = userStates
	ingester.userStatesMtx.Unlock()
	return nil
}

func processCheckpointWithRepair(params walRecoveryParameters) (*userStates, int, error) {

	// Use a local userStates, so we don't need to worry about locking.
	userStates := newUserStates(params.ingester.limiter, params.ingester.cfg, params.ingester.metrics)

	lastCheckpointDir, idx, err := lastCheckpoint(params.walDir)
	if err != nil {
		return nil, -1, err
	}
	if idx < 0 {
		level.Info(util.Logger).Log("msg", "no checkpoint found")
		return userStates, -1, nil
	}

	level.Info(util.Logger).Log("msg", fmt.Sprintf("recovering from %s", lastCheckpointDir))

	err = processCheckpoint(lastCheckpointDir, userStates, params)
	if err == nil {
		return userStates, idx, nil
	}

	// We don't call repair on checkpoint as losing even a single record is like losing the entire data of a series.
	// We try recovering from the older checkpoint instead.
	params.ingester.metrics.walCorruptionsTotal.Inc()
	level.Error(util.Logger).Log("msg", "checkpoint recovery failed, deleting this checkpoint and trying to recover from old checkpoint", "err", err)

	// Deleting this checkpoint to try the previous checkpoint.
	if err := os.RemoveAll(lastCheckpointDir); err != nil {
		return nil, -1, errors.Wrapf(err, "unable to delete checkpoint directory %s", lastCheckpointDir)
	}

	// If we have reached this point, it means the last checkpoint was deleted.
	// Now the last checkpoint will be the one before the deleted checkpoint.
	lastCheckpointDir, idx, err = lastCheckpoint(params.walDir)
	if err != nil {
		return nil, -1, err
	}

	// Creating new userStates to discard the old chunks.
	userStates = newUserStates(params.ingester.limiter, params.ingester.cfg, params.ingester.metrics)
	if idx < 0 {
		// There was only 1 checkpoint. We don't error in this case
		// as for the first checkpoint entire WAL will/should be present.
		return userStates, -1, nil
	}

	level.Info(util.Logger).Log("msg", fmt.Sprintf("attempting recovery from %s", lastCheckpointDir))
	if err := processCheckpoint(lastCheckpointDir, userStates, params); err != nil {
		// We won't attempt the repair again even if its the old checkpoint.
		params.ingester.metrics.walCorruptionsTotal.Inc()
		return nil, -1, err
	}

	return userStates, idx, nil
}

// segmentsExist is a stripped down version of
// https://github.com/prometheus/prometheus/blob/4c648eddf47d7e07fbc74d0b18244402200dca9e/tsdb/wal/wal.go#L739-L760.
func segmentsExist(dir string) (bool, error) {
	files, err := fileutil.ReadDir(dir)
	if err != nil {
		return false, err
	}
	for _, fn := range files {
		if _, err := strconv.Atoi(fn); err == nil {
			// First filename which is a number.
			// This is how Prometheus stores and this
			// is how it checks too.
			return true, nil
		}
	}
	return false, nil
}

// processCheckpoint loads the chunks of the series present in the last checkpoint.
func processCheckpoint(name string, userStates *userStates, params walRecoveryParameters) error {

	reader, closer, err := newWalReader(name, -1)
	if err != nil {
		return err
	}
	defer closer.Close()

	var (
		inputs = make([]chan *Series, params.numWorkers)
		// errChan is to capture the errors from goroutine.
		// The channel size is nWorkers+1 to not block any worker if all of them error out.
		errChan    = make(chan error, params.numWorkers)
		wg         = sync.WaitGroup{}
		seriesPool = &sync.Pool{
			New: func() interface{} {
				return &Series{}
			},
		}
	)

	wg.Add(params.numWorkers)
	for i := 0; i < params.numWorkers; i++ {
		inputs[i] = make(chan *Series, 300)
		go func(input <-chan *Series, stateCache map[string]*userState, seriesCache map[string]map[uint64]*memorySeries) {
			processCheckpointRecord(userStates, seriesPool, stateCache, seriesCache, input, errChan, params.ingester.metrics.memoryChunks)
			wg.Done()
		}(inputs[i], params.stateCache[i], params.seriesCache[i])
	}

	var capturedErr error
Loop:
	for reader.Next() {
		s := seriesPool.Get().(*Series)
		if err := proto.Unmarshal(reader.Record(), s); err != nil {
			// We don't return here in order to close/drain all the channels and
			// make sure all goroutines exit.
			capturedErr = err
			break Loop
		}
		// The yoloString from the unmarshal of LabelAdapter gets corrupted
		// when travelling through the channel. Hence making a copy of that.
		// This extra alloc during the read path is fine as it's only 1 time
		// and saves extra allocs during write path by having LabelAdapter.
		s.Labels = copyLabelAdapters(s.Labels)

		select {
		case capturedErr = <-errChan:
			// Exit early on an error.
			// Only acts upon the first error received.
			break Loop
		default:
			mod := s.Fingerprint % uint64(params.numWorkers)
			inputs[mod] <- s
		}
	}

	for i := 0; i < params.numWorkers; i++ {
		close(inputs[i])
	}
	wg.Wait()
	// If any worker errored out, some input channels might not be empty.
	// Hence drain them.
	for i := 0; i < params.numWorkers; i++ {
		for range inputs[i] {
		}
	}

	if capturedErr != nil {
		return capturedErr
	}
	select {
	case capturedErr = <-errChan:
		return capturedErr
	default:
		return reader.Err()
	}
}

func copyLabelAdapters(las []client.LabelAdapter) []client.LabelAdapter {
	for i := range las {
		n, v := make([]byte, len(las[i].Name)), make([]byte, len(las[i].Value))
		copy(n, las[i].Name)
		copy(v, las[i].Value)
		las[i].Name = string(n)
		las[i].Value = string(v)
	}
	return las
}

func processCheckpointRecord(
	userStates *userStates,
	seriesPool *sync.Pool,
	stateCache map[string]*userState,
	seriesCache map[string]map[uint64]*memorySeries,
	seriesChan <-chan *Series,
	errChan chan error,
	memoryChunks prometheus.Counter,
) {
	var la []client.LabelAdapter
	for s := range seriesChan {
		state, ok := stateCache[s.UserId]
		if !ok {
			state = userStates.getOrCreate(s.UserId)
			stateCache[s.UserId] = state
			seriesCache[s.UserId] = make(map[uint64]*memorySeries)
		}

		la = la[:0]
		for _, l := range s.Labels {
			la = append(la, client.LabelAdapter{
				Name:  string(l.Name),
				Value: string(l.Value),
			})
		}
		series, err := state.createSeriesWithFingerprint(model.Fingerprint(s.Fingerprint), la, nil, true)
		if err != nil {
			errChan <- err
			return
		}

		descs, err := fromWireChunks(s.Chunks)
		if err != nil {
			errChan <- err
			return
		}

		if err := series.setChunks(descs); err != nil {
			errChan <- err
			return
		}
		memoryChunks.Add(float64(len(descs)))

		seriesCache[s.UserId][s.Fingerprint] = series
		seriesPool.Put(s)
	}
}

type samplesWithUserID struct {
	samples []Sample
	userID  string
}

func processWALWithRepair(startSegment int, userStates *userStates, params walRecoveryParameters) error {

	corruptErr := processWAL(startSegment, userStates, params)
	if corruptErr == nil {
		return nil
	}

	params.ingester.metrics.walCorruptionsTotal.Inc()
	level.Error(util.Logger).Log("msg", "error in replaying from WAL", "err", corruptErr)

	// Attempt repair.
	level.Info(util.Logger).Log("msg", "attempting repair of the WAL")
	w, err := wal.New(util.Logger, nil, params.walDir, true)
	if err != nil {
		return err
	}

	err = w.Repair(corruptErr)
	if err != nil {
		level.Error(util.Logger).Log("msg", "error in repairing WAL", "err", err)
	}
	var multiErr tsdb_errors.MultiError
	multiErr.Add(err)
	multiErr.Add(w.Close())

	return multiErr.Err()
}

// processWAL processes the records in the WAL concurrently.
func processWAL(startSegment int, userStates *userStates, params walRecoveryParameters) error {

	reader, closer, err := newWalReader(params.walDir, startSegment)
	if err != nil {
		return err
	}
	defer closer.Close()

	var (
		wg      sync.WaitGroup
		inputs  = make([]chan *samplesWithUserID, params.numWorkers)
		outputs = make([]chan *samplesWithUserID, params.numWorkers)
		// errChan is to capture the errors from goroutine.
		// The channel size is nWorkers to not block any worker if all of them error out.
		errChan = make(chan error, params.numWorkers)
		shards  = make([]*samplesWithUserID, params.numWorkers)
	)

	wg.Add(params.numWorkers)
	for i := 0; i < params.numWorkers; i++ {
		outputs[i] = make(chan *samplesWithUserID, 300)
		inputs[i] = make(chan *samplesWithUserID, 300)
		shards[i] = &samplesWithUserID{}

		go func(input <-chan *samplesWithUserID, output chan<- *samplesWithUserID,
			stateCache map[string]*userState, seriesCache map[string]map[uint64]*memorySeries) {
			processWALSamples(userStates, stateCache, seriesCache, input, output, errChan)
			wg.Done()
		}(inputs[i], outputs[i], params.stateCache[i], params.seriesCache[i])
	}

	var (
		capturedErr error
		record      = &Record{}
	)
Loop:
	for reader.Next() {
		select {
		case capturedErr = <-errChan:
			// Exit early on an error.
			// Only acts upon the first error received.
			break Loop
		default:
		}
		if err := proto.Unmarshal(reader.Record(), record); err != nil {
			// We don't return here in order to close/drain all the channels and
			// make sure all goroutines exit.
			capturedErr = err
			break Loop
		}

		if len(record.Labels) > 0 {
			state := userStates.getOrCreate(record.UserId)
			// Create the series from labels which do not exist.
			for _, labels := range record.Labels {
				_, ok := state.fpToSeries.get(model.Fingerprint(labels.Fingerprint))
				if ok {
					continue
				}
				_, err := state.createSeriesWithFingerprint(model.Fingerprint(labels.Fingerprint), labels.Labels, nil, true)
				if err != nil {
					// We don't return here in order to close/drain all the channels and
					// make sure all goroutines exit.
					capturedErr = err
					break Loop
				}
			}
		}

		// We split up the samples into chunks of 5000 samples or less.
		// With O(300 * #cores) in-flight sample batches, large scrapes could otherwise
		// cause thousands of very large in flight buffers occupying large amounts
		// of unused memory.
		for len(record.Samples) > 0 {
			m := 5000
			if len(record.Samples) < m {
				m = len(record.Samples)
			}
			for i := 0; i < params.numWorkers; i++ {
				if len(shards[i].samples) == 0 {
					// It is possible that the previous iteration did not put
					// anything in this shard. In that case no need to get a new buffer.
					shards[i].userID = record.UserId
					continue
				}
				select {
				case buf := <-outputs[i]:
					buf.samples = buf.samples[:0]
					buf.userID = record.UserId
					shards[i] = buf
				default:
					shards[i] = &samplesWithUserID{
						userID: record.UserId,
					}
				}
			}
			for _, sam := range record.Samples[:m] {
				mod := sam.Fingerprint % uint64(params.numWorkers)
				shards[mod].samples = append(shards[mod].samples, sam)
			}
			for i := 0; i < params.numWorkers; i++ {
				if len(shards[i].samples) > 0 {
					inputs[i] <- shards[i]
				}
			}
			record.Samples = record.Samples[m:]
		}
	}

	for i := 0; i < params.numWorkers; i++ {
		close(inputs[i])
		for range outputs[i] {
		}
	}
	wg.Wait()
	// If any worker errored out, some input channels might not be empty.
	// Hence drain them.
	for i := 0; i < params.numWorkers; i++ {
		for range inputs[i] {
		}
	}

	if capturedErr != nil {
		return capturedErr
	}
	select {
	case capturedErr = <-errChan:
		return capturedErr
	default:
		return reader.Err()
	}
}

func processWALSamples(userStates *userStates, stateCache map[string]*userState, seriesCache map[string]map[uint64]*memorySeries,
	input <-chan *samplesWithUserID, output chan<- *samplesWithUserID, errChan chan error) {
	defer close(output)

	sp := model.SamplePair{}
	for samples := range input {
		state, ok := stateCache[samples.userID]
		if !ok {
			state = userStates.getOrCreate(samples.userID)
			stateCache[samples.userID] = state
			seriesCache[samples.userID] = make(map[uint64]*memorySeries)
		}
		sc := seriesCache[samples.userID]
		for i := range samples.samples {
			series, ok := sc[samples.samples[i].Fingerprint]
			if !ok {
				series, ok = state.fpToSeries.get(model.Fingerprint(samples.samples[i].Fingerprint))
				if !ok {
					// This should ideally not happen.
					// If the series was not created in recovering checkpoint or
					// from the labels of any records previous to this, there
					// is no way to get the labels for this fingerprint.
					level.Warn(util.Logger).Log("msg", "series not found for sample during wal recovery", "userid", samples.userID, "fingerprint", model.Fingerprint(samples.samples[i].Fingerprint).String())
					continue
				}
			}

			sp.Timestamp = model.Time(samples.samples[i].Timestamp)
			sp.Value = model.SampleValue(samples.samples[i].Value)
			// There can be many out of order samples because of checkpoint and WAL overlap.
			// Checking this beforehand avoids the allocation of lots of error messages.
			if sp.Timestamp.After(series.lastTime) {
				if err := series.add(sp); err != nil {
					errChan <- err
					return
				}
			}
		}
		output <- samples
	}
}

// If startSegment is <0, it means all the segments.
func newWalReader(name string, startSegment int) (*wal.Reader, io.Closer, error) {
	var (
		segmentReader io.ReadCloser
		err           error
	)
	if startSegment < 0 {
		segmentReader, err = wal.NewSegmentsReader(name)
		if err != nil {
			return nil, nil, err
		}
	} else {
		first, last, err := SegmentRange(name)
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
			Dir:   name,
			First: startSegment,
			Last:  -1, // Till the end.
		})
		if err != nil {
			return nil, nil, err
		}
	}
	return wal.NewReader(segmentReader), segmentReader, nil
}

// SegmentRange returns the first and last segment index of the WAL in the dir.
// If https://github.com/prometheus/prometheus/pull/6477 is merged, get rid of this
// method and use from Prometheus directly.
func SegmentRange(dir string) (int, int, error) {
	files, err := fileutil.ReadDir(dir)
	if err != nil {
		return 0, 0, err
	}
	first, last := math.MaxInt32, math.MinInt32
	for _, fn := range files {
		k, err := strconv.Atoi(fn)
		if err != nil {
			continue
		}
		if k < first {
			first = k
		}
		if k > last {
			last = k
		}
	}
	if first == math.MaxInt32 || last == math.MinInt32 {
		return -1, -1, nil
	}
	return first, last, nil
}
