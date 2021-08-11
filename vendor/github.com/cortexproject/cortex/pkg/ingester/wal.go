package ingester

import (
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"sync"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/gogo/protobuf/proto"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/tsdb/encoding"
	tsdb_errors "github.com/prometheus/prometheus/tsdb/errors"
	"github.com/prometheus/prometheus/tsdb/fileutil"
	tsdb_record "github.com/prometheus/prometheus/tsdb/record"
	"github.com/prometheus/prometheus/tsdb/wal"

	"github.com/cortexproject/cortex/pkg/cortexpb"
	"github.com/cortexproject/cortex/pkg/ingester/client"
)

// WALConfig is config for the Write Ahead Log.
type WALConfig struct {
	WALEnabled         bool          `yaml:"wal_enabled"`
	CheckpointEnabled  bool          `yaml:"checkpoint_enabled"`
	Recover            bool          `yaml:"recover_from_wal"`
	Dir                string        `yaml:"wal_dir"`
	CheckpointDuration time.Duration `yaml:"checkpoint_duration"`
	FlushOnShutdown    bool          `yaml:"flush_on_shutdown_with_wal_enabled"`
	// We always checkpoint during shutdown. This option exists for the tests.
	checkpointDuringShutdown bool
}

// RegisterFlags adds the flags required to config this to the given FlagSet
func (cfg *WALConfig) RegisterFlags(f *flag.FlagSet) {
	f.StringVar(&cfg.Dir, "ingester.wal-dir", "wal", "Directory to store the WAL and/or recover from WAL.")
	f.BoolVar(&cfg.Recover, "ingester.recover-from-wal", false, "Recover data from existing WAL irrespective of WAL enabled/disabled.")
	f.BoolVar(&cfg.WALEnabled, "ingester.wal-enabled", false, "Enable writing of ingested data into WAL.")
	f.BoolVar(&cfg.CheckpointEnabled, "ingester.checkpoint-enabled", true, "Enable checkpointing of in-memory chunks. It should always be true when using normally. Set it to false iff you are doing some small tests as there is no mechanism to delete the old WAL yet if checkpoint is disabled.")
	f.DurationVar(&cfg.CheckpointDuration, "ingester.checkpoint-duration", 30*time.Minute, "Interval at which checkpoints should be created.")
	f.BoolVar(&cfg.FlushOnShutdown, "ingester.flush-on-shutdown-with-wal-enabled", false, "When WAL is enabled, should chunks be flushed to long-term storage on shutdown. Useful eg. for migration to blocks engine.")
	cfg.checkpointDuringShutdown = true
}

// WAL interface allows us to have a no-op WAL when the WAL is disabled.
type WAL interface {
	// Log marshalls the records and writes it into the WAL.
	Log(*WALRecord) error
	// Stop stops all the WAL operations.
	Stop()
}

// RecordType represents the type of the WAL/Checkpoint record.
type RecordType byte

const (
	// WALRecordSeries is the type for the WAL record on Prometheus TSDB record for series.
	WALRecordSeries RecordType = 1
	// WALRecordSamples is the type for the WAL record based on Prometheus TSDB record for samples.
	WALRecordSamples RecordType = 2

	// CheckpointRecord is the type for the Checkpoint record based on protos.
	CheckpointRecord RecordType = 3
)

type noopWAL struct{}

func (noopWAL) Log(*WALRecord) error { return nil }
func (noopWAL) Stop()                {}

type walWrapper struct {
	cfg  WALConfig
	quit chan struct{}
	wait sync.WaitGroup

	wal           *wal.WAL
	getUserStates func() map[string]*userState
	checkpointMtx sync.Mutex
	bytesPool     sync.Pool

	logger log.Logger

	// Metrics.
	checkpointDeleteFail       prometheus.Counter
	checkpointDeleteTotal      prometheus.Counter
	checkpointCreationFail     prometheus.Counter
	checkpointCreationTotal    prometheus.Counter
	checkpointDuration         prometheus.Summary
	checkpointLoggedBytesTotal prometheus.Counter
	walLoggedBytesTotal        prometheus.Counter
	walRecordsLogged           prometheus.Counter
}

// newWAL creates a WAL object. If the WAL is disabled, then the returned WAL is a no-op WAL.
func newWAL(cfg WALConfig, userStatesFunc func() map[string]*userState, registerer prometheus.Registerer, logger log.Logger) (WAL, error) {
	if !cfg.WALEnabled {
		return &noopWAL{}, nil
	}

	var walRegistry prometheus.Registerer
	if registerer != nil {
		walRegistry = prometheus.WrapRegistererWith(prometheus.Labels{"kind": "wal"}, registerer)
	}
	tsdbWAL, err := wal.NewSize(logger, walRegistry, cfg.Dir, wal.DefaultSegmentSize/4, false)
	if err != nil {
		return nil, err
	}

	w := &walWrapper{
		cfg:           cfg,
		quit:          make(chan struct{}),
		wal:           tsdbWAL,
		getUserStates: userStatesFunc,
		bytesPool: sync.Pool{
			New: func() interface{} {
				return make([]byte, 0, 512)
			},
		},
		logger: logger,
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
	w.walRecordsLogged = promauto.With(registerer).NewCounter(prometheus.CounterOpts{
		Name: "cortex_ingester_wal_records_logged_total",
		Help: "Total number of WAL records logged.",
	})
	w.checkpointLoggedBytesTotal = promauto.With(registerer).NewCounter(prometheus.CounterOpts{
		Name: "cortex_ingester_checkpoint_logged_bytes_total",
		Help: "Total number of bytes written to disk for checkpointing.",
	})
	w.walLoggedBytesTotal = promauto.With(registerer).NewCounter(prometheus.CounterOpts{
		Name: "cortex_ingester_wal_logged_bytes_total",
		Help: "Total number of bytes written to disk for WAL records.",
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

func (w *walWrapper) Log(record *WALRecord) error {
	if record == nil || (len(record.Series) == 0 && len(record.Samples) == 0) {
		return nil
	}
	select {
	case <-w.quit:
		return nil
	default:
		buf := w.bytesPool.Get().([]byte)[:0]
		defer func() {
			w.bytesPool.Put(buf) // nolint:staticcheck
		}()

		if len(record.Series) > 0 {
			buf = record.encodeSeries(buf)
			if err := w.wal.Log(buf); err != nil {
				return err
			}
			w.walRecordsLogged.Inc()
			w.walLoggedBytesTotal.Add(float64(len(buf)))
			buf = buf[:0]
		}
		if len(record.Samples) > 0 {
			buf = record.encodeSamples(buf)
			if err := w.wal.Log(buf); err != nil {
				return err
			}
			w.walRecordsLogged.Inc()
			w.walLoggedBytesTotal.Add(float64(len(buf)))
		}
		return nil
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
			level.Info(w.logger).Log("msg", "starting checkpoint")
			if err := w.performCheckpoint(false); err != nil {
				level.Error(w.logger).Log("msg", "error checkpointing series", "err", err)
				continue
			}
			elapsed := time.Since(start)
			level.Info(w.logger).Log("msg", "checkpoint done", "time", elapsed.String())
			w.checkpointDuration.Observe(elapsed.Seconds())
		case <-w.quit:
			if w.cfg.checkpointDuringShutdown {
				level.Info(w.logger).Log("msg", "creating checkpoint before shutdown")
				if err := w.performCheckpoint(true); err != nil {
					level.Error(w.logger).Log("msg", "error checkpointing series during shutdown", "err", err)
				}
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

	_, lastSegment, err := wal.Segments(w.wal.Dir())
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

		_, lastSegment, err = wal.Segments(w.wal.Dir())
		if err != nil {
			return err
		}
	}

	// Checkpoint is named after the last WAL segment present so that when replaying the WAL
	// we can start from that particular WAL segment.
	checkpointDir := filepath.Join(w.wal.Dir(), fmt.Sprintf(checkpointPrefix+"%06d", lastSegment))
	level.Info(w.logger).Log("msg", "attempting checkpoint for", "dir", checkpointDir)
	checkpointDirTemp := checkpointDir + ".tmp"

	if err := os.MkdirAll(checkpointDirTemp, 0777); err != nil {
		return errors.Wrap(err, "create checkpoint dir")
	}
	checkpoint, err := wal.New(nil, nil, checkpointDirTemp, false)
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

	perSeriesDuration := (95 * w.cfg.CheckpointDuration) / (100 * time.Duration(numSeries))

	var wireChunkBuf []client.Chunk
	var b []byte
	bytePool := sync.Pool{
		New: func() interface{} {
			return make([]byte, 0, 1024)
		},
	}
	records := [][]byte{}
	totalSize := 0
	ticker := time.NewTicker(perSeriesDuration)
	defer ticker.Stop()
	start := time.Now()
	for userID, state := range us {
		for pair := range state.fpToSeries.iter() {
			state.fpLocker.Lock(pair.fp)
			wireChunkBuf, b, err = w.checkpointSeries(userID, pair.fp, pair.series, wireChunkBuf, bytePool.Get().([]byte))
			state.fpLocker.Unlock(pair.fp)
			if err != nil {
				return err
			}

			records = append(records, b)
			totalSize += len(b)
			if totalSize >= 1*1024*1024 { // 1 MiB.
				if err := checkpoint.Log(records...); err != nil {
					return err
				}
				w.checkpointLoggedBytesTotal.Add(float64(totalSize))
				totalSize = 0
				for i := range records {
					bytePool.Put(records[i]) // nolint:staticcheck
				}
				records = records[:0]
			}

			if !immediate {
				if time.Since(start) > 2*w.cfg.CheckpointDuration {
					// This could indicate a surge in number of series and continuing with
					// the old estimation of ticker can make checkpointing run indefinitely in worst case
					// and disk running out of space. Re-adjust the ticker might not solve the problem
					// as there can be another surge again. Hence let's checkpoint this one immediately.
					immediate = true
					continue
				}

				select {
				case <-ticker.C:
				case <-w.quit: // When we're trying to shutdown, finish the checkpoint as fast as possible.
				}
			}
		}
	}

	if err := checkpoint.Log(records...); err != nil {
		return err
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
		level.Error(w.logger).Log("msg", "error deleting old WAL segments", "err", err)
	}

	if lastCh >= 0 {
		if err := w.deleteCheckpoints(lastCh); err != nil {
			// It is fine to have old checkpoints hanging around if deletion failed.
			// We can try again next time.
			level.Error(w.logger).Log("msg", "error deleting old checkpoint", "err", err)
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
func (w *walWrapper) deleteCheckpoints(maxIndex int) (err error) {
	w.checkpointDeleteTotal.Inc()
	defer func() {
		if err != nil {
			w.checkpointDeleteFail.Inc()
		}
	}()

	errs := tsdb_errors.NewMulti()

	files, err := ioutil.ReadDir(w.wal.Dir())
	if err != nil {
		return err
	}
	for _, fi := range files {
		index, err := checkpointIndex(fi.Name(), true)
		if err != nil || index >= maxIndex {
			continue
		}
		if err := os.RemoveAll(filepath.Join(w.wal.Dir(), fi.Name())); err != nil {
			errs.Add(err)
		}
	}
	return errs.Err()
}

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

// checkpointSeries write the chunks of the series to the checkpoint.
func (w *walWrapper) checkpointSeries(userID string, fp model.Fingerprint, series *memorySeries, wireChunks []client.Chunk, b []byte) ([]client.Chunk, []byte, error) {
	var err error
	wireChunks, err = toWireChunks(series.chunkDescs, wireChunks)
	if err != nil {
		return wireChunks, b, err
	}

	b, err = encodeWithTypeHeader(&Series{
		UserId:      userID,
		Fingerprint: uint64(fp),
		Labels:      cortexpb.FromLabelsToLabelAdapters(series.metric),
		Chunks:      wireChunks,
	}, CheckpointRecord, b)

	return wireChunks, b, err
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

	level.Info(ingester.logger).Log("msg", "recovering from checkpoint")
	start := time.Now()
	userStates, idx, err := processCheckpointWithRepair(params)
	if err != nil {
		return err
	}
	elapsed := time.Since(start)
	level.Info(ingester.logger).Log("msg", "recovered from checkpoint", "time", elapsed.String())

	if segExists, err := segmentsExist(params.walDir); err == nil && !segExists {
		level.Info(ingester.logger).Log("msg", "no segments found, skipping recover from segments")
		ingester.userStatesMtx.Lock()
		ingester.userStates = userStates
		ingester.userStatesMtx.Unlock()
		return nil
	} else if err != nil {
		return err
	}

	level.Info(ingester.logger).Log("msg", "recovering from WAL", "dir", params.walDir, "start_segment", idx)
	start = time.Now()
	if err := processWALWithRepair(idx, userStates, params); err != nil {
		return err
	}
	elapsed = time.Since(start)
	level.Info(ingester.logger).Log("msg", "recovered from WAL", "time", elapsed.String())

	ingester.userStatesMtx.Lock()
	ingester.userStates = userStates
	ingester.userStatesMtx.Unlock()
	return nil
}

func processCheckpointWithRepair(params walRecoveryParameters) (*userStates, int, error) {
	logger := params.ingester.logger

	// Use a local userStates, so we don't need to worry about locking.
	userStates := newUserStates(params.ingester.limiter, params.ingester.cfg, params.ingester.metrics, params.ingester.logger)

	lastCheckpointDir, idx, err := lastCheckpoint(params.walDir)
	if err != nil {
		return nil, -1, err
	}
	if idx < 0 {
		level.Info(logger).Log("msg", "no checkpoint found")
		return userStates, -1, nil
	}

	level.Info(logger).Log("msg", fmt.Sprintf("recovering from %s", lastCheckpointDir))

	err = processCheckpoint(lastCheckpointDir, userStates, params)
	if err == nil {
		return userStates, idx, nil
	}

	// We don't call repair on checkpoint as losing even a single record is like losing the entire data of a series.
	// We try recovering from the older checkpoint instead.
	params.ingester.metrics.walCorruptionsTotal.Inc()
	level.Error(logger).Log("msg", "checkpoint recovery failed, deleting this checkpoint and trying to recover from old checkpoint", "err", err)

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
	userStates = newUserStates(params.ingester.limiter, params.ingester.cfg, params.ingester.metrics, params.ingester.logger)
	if idx < 0 {
		// There was only 1 checkpoint. We don't error in this case
		// as for the first checkpoint entire WAL will/should be present.
		return userStates, -1, nil
	}

	level.Info(logger).Log("msg", fmt.Sprintf("attempting recovery from %s", lastCheckpointDir))
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
	files, err := ioutil.ReadDir(dir)
	if err != nil {
		return false, err
	}
	for _, f := range files {
		if _, err := strconv.Atoi(f.Name()); err == nil {
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
		m, err := decodeCheckpointRecord(reader.Record(), s)
		if err != nil {
			// We don't return here in order to close/drain all the channels and
			// make sure all goroutines exit.
			capturedErr = err
			break Loop
		}
		s = m.(*Series)

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

func copyLabelAdapters(las []cortexpb.LabelAdapter) []cortexpb.LabelAdapter {
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
	var la []cortexpb.LabelAdapter
	for s := range seriesChan {
		state, ok := stateCache[s.UserId]
		if !ok {
			state = userStates.getOrCreate(s.UserId)
			stateCache[s.UserId] = state
			seriesCache[s.UserId] = make(map[uint64]*memorySeries)
		}

		la = la[:0]
		for _, l := range s.Labels {
			la = append(la, cortexpb.LabelAdapter{
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
	samples []tsdb_record.RefSample
	userID  string
}

func processWALWithRepair(startSegment int, userStates *userStates, params walRecoveryParameters) error {
	logger := params.ingester.logger

	corruptErr := processWAL(startSegment, userStates, params)
	if corruptErr == nil {
		return nil
	}

	params.ingester.metrics.walCorruptionsTotal.Inc()
	level.Error(logger).Log("msg", "error in replaying from WAL", "err", corruptErr)

	// Attempt repair.
	level.Info(logger).Log("msg", "attempting repair of the WAL")
	w, err := wal.New(logger, nil, params.walDir, true)
	if err != nil {
		return err
	}

	err = w.Repair(corruptErr)
	if err != nil {
		level.Error(logger).Log("msg", "error in repairing WAL", "err", err)
	}

	return tsdb_errors.NewMulti(err, w.Close()).Err()
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
			processWALSamples(userStates, stateCache, seriesCache, input, output, errChan, params.ingester.logger)
			wg.Done()
		}(inputs[i], outputs[i], params.stateCache[i], params.seriesCache[i])
	}

	var (
		capturedErr error
		walRecord   = &WALRecord{}
		lp          labelPairs
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

		if err := decodeWALRecord(reader.Record(), walRecord); err != nil {
			// We don't return here in order to close/drain all the channels and
			// make sure all goroutines exit.
			capturedErr = err
			break Loop
		}

		if len(walRecord.Series) > 0 {
			userID := walRecord.UserID

			state := userStates.getOrCreate(userID)

			for _, s := range walRecord.Series {
				fp := model.Fingerprint(s.Ref)
				_, ok := state.fpToSeries.get(fp)
				if ok {
					continue
				}

				lp = lp[:0]
				for _, l := range s.Labels {
					lp = append(lp, cortexpb.LabelAdapter(l))
				}
				if _, err := state.createSeriesWithFingerprint(fp, lp, nil, true); err != nil {
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
		walRecordSamples := walRecord.Samples
		for len(walRecordSamples) > 0 {
			m := 5000
			userID := walRecord.UserID
			if len(walRecordSamples) < m {
				m = len(walRecordSamples)
			}

			for i := 0; i < params.numWorkers; i++ {
				if len(shards[i].samples) == 0 {
					// It is possible that the previous iteration did not put
					// anything in this shard. In that case no need to get a new buffer.
					shards[i].userID = userID
					continue
				}
				select {
				case buf := <-outputs[i]:
					buf.samples = buf.samples[:0]
					buf.userID = userID
					shards[i] = buf
				default:
					shards[i] = &samplesWithUserID{
						userID: userID,
					}
				}
			}

			for _, sam := range walRecordSamples[:m] {
				mod := sam.Ref % uint64(params.numWorkers)
				shards[mod].samples = append(shards[mod].samples, sam)
			}

			for i := 0; i < params.numWorkers; i++ {
				if len(shards[i].samples) > 0 {
					inputs[i] <- shards[i]
				}
			}

			walRecordSamples = walRecordSamples[m:]
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
	input <-chan *samplesWithUserID, output chan<- *samplesWithUserID, errChan chan error, logger log.Logger) {
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
			series, ok := sc[samples.samples[i].Ref]
			if !ok {
				series, ok = state.fpToSeries.get(model.Fingerprint(samples.samples[i].Ref))
				if !ok {
					// This should ideally not happen.
					// If the series was not created in recovering checkpoint or
					// from the labels of any records previous to this, there
					// is no way to get the labels for this fingerprint.
					level.Warn(logger).Log("msg", "series not found for sample during wal recovery", "userid", samples.userID, "fingerprint", model.Fingerprint(samples.samples[i].Ref).String())
					continue
				}
			}
			sp.Timestamp = model.Time(samples.samples[i].T)
			sp.Value = model.SampleValue(samples.samples[i].V)
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
		first, last, err := wal.Segments(name)
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

func decodeCheckpointRecord(rec []byte, m proto.Message) (_ proto.Message, err error) {
	switch RecordType(rec[0]) {
	case CheckpointRecord:
		if err := proto.Unmarshal(rec[1:], m); err != nil {
			return m, err
		}
	default:
		// The legacy proto record will have it's first byte >7.
		// Hence it does not match any of the existing record types.
		err := proto.Unmarshal(rec, m)
		if err != nil {
			return m, err
		}
	}

	return m, err
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

// WALRecord is a struct combining the series and samples record.
type WALRecord struct {
	UserID  string
	Series  []tsdb_record.RefSeries
	Samples []tsdb_record.RefSample
}

func (record *WALRecord) encodeSeries(b []byte) []byte {
	buf := encoding.Encbuf{B: b}
	buf.PutByte(byte(WALRecordSeries))
	buf.PutUvarintStr(record.UserID)

	var enc tsdb_record.Encoder
	// The 'encoded' already has the type header and userID here, hence re-using
	// the remaining part of the slice (i.e. encoded[len(encoded):])) to encode the series.
	encoded := buf.Get()
	encoded = append(encoded, enc.Series(record.Series, encoded[len(encoded):])...)

	return encoded
}

func (record *WALRecord) encodeSamples(b []byte) []byte {
	buf := encoding.Encbuf{B: b}
	buf.PutByte(byte(WALRecordSamples))
	buf.PutUvarintStr(record.UserID)

	var enc tsdb_record.Encoder
	// The 'encoded' already has the type header and userID here, hence re-using
	// the remaining part of the slice (i.e. encoded[len(encoded):]))to encode the samples.
	encoded := buf.Get()
	encoded = append(encoded, enc.Samples(record.Samples, encoded[len(encoded):])...)

	return encoded
}

func decodeWALRecord(b []byte, walRec *WALRecord) (err error) {
	var (
		userID   string
		dec      tsdb_record.Decoder
		rseries  []tsdb_record.RefSeries
		rsamples []tsdb_record.RefSample

		decbuf = encoding.Decbuf{B: b}
		t      = RecordType(decbuf.Byte())
	)

	walRec.Series = walRec.Series[:0]
	walRec.Samples = walRec.Samples[:0]
	switch t {
	case WALRecordSamples:
		userID = decbuf.UvarintStr()
		rsamples, err = dec.Samples(decbuf.B, walRec.Samples)
	case WALRecordSeries:
		userID = decbuf.UvarintStr()
		rseries, err = dec.Series(decbuf.B, walRec.Series)
	default:
		return errors.New("unknown record type")
	}

	// We reach here only if its a record with type header.
	if decbuf.Err() != nil {
		return decbuf.Err()
	}

	if err != nil {
		return err
	}

	walRec.UserID = userID
	walRec.Samples = rsamples
	walRec.Series = rseries

	return nil
}
