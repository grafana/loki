package file

import (
	"bytes"
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"

	"github.com/grafana/loki/v3/clients/pkg/promtail/client/fake"
	"github.com/grafana/loki/v3/clients/pkg/promtail/positions"
)

func TestFileTargetSync(t *testing.T) {
	w := log.NewSyncWriter(os.Stderr)
	logger := log.NewLogfmtLogger(w)

	dirName := newTestLogDirectories(t)
	positionsFileName := filepath.Join(dirName, "positions.yml")
	logDir1 := filepath.Join(dirName, "log1")
	logDir1File1 := filepath.Join(logDir1, "test1.log")
	logDir1File2 := filepath.Join(logDir1, "test2.log")

	// Set the sync period to a really long value, to guarantee the sync timer never runs, this way we know
	// everything saved was done through channel notifications when target.stop() was called.
	ps, err := positions.New(logger, positions.Config{
		SyncPeriod:    10 * time.Minute,
		PositionsFile: positionsFileName,
	})
	if err != nil {
		t.Fatal(err)
	}

	client := fake.New(func() {})
	defer client.Stop()

	metrics := NewMetrics(nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fakeHandler := make(chan fileTargetEvent)
	receivedStartWatch := atomic.NewInt32(0)
	receivedStopWatch := atomic.NewInt32(0)
	go func() {
		for {
			select {
			case event := <-fakeHandler:
				switch event.eventType {
				case fileTargetEventWatchStart:
					receivedStartWatch.Add(1)
				case fileTargetEventWatchStop:
					receivedStopWatch.Add(1)
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	path := logDir1 + "/*.log"
	target, err := NewFileTarget(metrics, logger, client, ps, path, "", nil, nil, &Config{
		SyncPeriod: 1 * time.Minute, // assure the sync is not called by the ticker
	}, DefaultWatchConig, nil, fakeHandler, "", nil)
	assert.NoError(t, err)

	// Start with nothing watched.
	if target.getWatchesLen() != 0 {
		t.Fatal("Expected watches to be 0 at this point in the test...")
	}
	if target.getReadersLen() != 0 {
		t.Fatal("Expected tails to be 0 at this point in the test...")
	}

	// Create the base dir, still nothing watched.
	err = os.MkdirAll(logDir1, 0750)
	assert.NoError(t, err)

	err = target.sync()
	assert.NoError(t, err)

	if target.getWatchesLen() != 0 {
		t.Fatal("Expected watches to be 0 at this point in the test...")
	}
	if target.getReadersLen() != 0 {
		t.Fatal("Expected tails to be 0 at this point in the test...")
	}

	// Add a file, which should create a watcher and a tailer.
	_, err = os.Create(logDir1File1)
	assert.NoError(t, err)

	// Delay sync() call to make sure the filesystem watch event does not fire during sync()
	time.Sleep(10 * time.Millisecond)
	err = target.sync()
	assert.NoError(t, err)

	assert.Equal(t, 1, target.getWatchesLen(),
		"Expected watches to be 1 at this point in the test...",
	)
	assert.Equal(t, 1, target.getReadersLen(),
		"Expected tails to be 1 at this point in the test...",
	)

	requireEventually(t, func() bool {
		return receivedStartWatch.Load() == 1
	}, "Expected received starting watch event to be 1 at this point in the test...")

	// Add another file, should get another tailer.
	_, err = os.Create(logDir1File2)
	assert.NoError(t, err)

	err = target.sync()
	assert.NoError(t, err)

	assert.Equal(t, 1, target.getWatchesLen(),
		"Expected watches to be 1 at this point in the test...",
	)
	assert.Equal(t, 2, target.getReadersLen(),
		"Expected tails to be 2 at this point in the test...",
	)

	// Remove one of the files, tailer should stop.
	err = os.Remove(logDir1File1)
	assert.NoError(t, err)

	err = target.sync()
	assert.NoError(t, err)

	assert.Equal(t, 1, target.getWatchesLen(),
		"Expected watches to be 1 at this point in the test...",
	)
	assert.Equal(t, 1, target.getReadersLen(),
		"Expected tails to be 1 at this point in the test...",
	)

	// Remove the entire directory, other tailer should stop and watcher should go away.
	err = os.RemoveAll(logDir1)
	assert.NoError(t, err)

	err = target.sync()
	assert.NoError(t, err)

	assert.Equal(t, 0, target.getWatchesLen(),
		"Expected watches to be 0 at this point in the test...",
	)
	assert.Equal(t, 0, target.getReadersLen(),
		"Expected tails to be 0 at this point in the test...",
	)
	requireEventually(t, func() bool {
		return receivedStartWatch.Load() == 1
	}, "Expected received starting watch event to be 1 at this point in the test...")
	requireEventually(t, func() bool {
		return receivedStartWatch.Load() == 1
	}, "Expected received stopping watch event to be 1 at this point in the test...")

	target.Stop()
	ps.Stop()
}

func TestFileTarget_StopsTailersCleanly(t *testing.T) {
	w := log.NewSyncWriter(os.Stderr)
	logger := log.NewLogfmtLogger(w)

	tempDir := t.TempDir()
	positionsFileName := filepath.Join(tempDir, "positions.yml")
	logFile := filepath.Join(tempDir, "test1.log")

	ps, err := positions.New(logger, positions.Config{
		SyncPeriod:    10 * time.Millisecond,
		PositionsFile: positionsFileName,
	})
	require.NoError(t, err)

	client := fake.New(func() {})
	defer client.Stop()

	fakeHandler := make(chan fileTargetEvent, 10)
	pathToWatch := filepath.Join(tempDir, "*.log")
	registry := prometheus.NewRegistry()
	target, err := NewFileTarget(NewMetrics(registry), logger, client, ps, pathToWatch, "", nil, nil, &Config{
		SyncPeriod: 10 * time.Millisecond,
	}, DefaultWatchConig, nil, fakeHandler, "", nil)
	assert.NoError(t, err)

	_, err = os.Create(logFile)
	assert.NoError(t, err)

	requireEventually(t, func() bool {
		return target.getReadersLen() == 1
	}, "expected 1 tailer to be created")

	require.NoError(t, testutil.GatherAndCompare(registry, bytes.NewBufferString(`
		# HELP promtail_files_active_total Number of active files.
		# TYPE promtail_files_active_total gauge
		promtail_files_active_total 1
	`), "promtail_files_active_total"))

	// Inject an error to tailer

	initialReader, _ := target.getReader(logFile)
	initailTailer := initialReader.(*tailer)
	_ = initailTailer.tail.Tomb.Killf("test: network file systems can be unreliable")

	// Tailer will be replaced by a new one
	requireEventually(t, func() bool {
		currentReader, _ := target.getReader(logFile)
		var currentTailer *tailer
		if currentReader != nil {
			currentTailer = currentReader.(*tailer)
		}
		return target.getReadersLen() == 1 && currentTailer != initailTailer
	}, "expected dead tailer to be replaced by a new one")

	// The old tailer should be stopped:
	select {
	case <-initailTailer.done:
	case <-time.After(time.Second * 10):
		t.Fatal("expected position timer to be stopped cleanly")
	}

	// The old tailer's position timer should be stopped
	select {
	case <-initailTailer.posdone:
	case <-time.After(time.Second * 10):
		t.Fatal("expected position timer to be stopped cleanly")
	}

	target.Stop()
	ps.Stop()

	require.NoError(t, testutil.GatherAndCompare(registry, bytes.NewBufferString(`
		# HELP promtail_files_active_total Number of active files.
		# TYPE promtail_files_active_total gauge
		promtail_files_active_total 0
	`), "promtail_files_active_total"))
}

func TestFileTarget_StopsTailersCleanly_Parallel(t *testing.T) {
	w := log.NewSyncWriter(os.Stderr)
	logger := log.NewLogfmtLogger(w)

	tempDir := t.TempDir()
	positionsFileName := filepath.Join(tempDir, "positions.yml")

	ps, err := positions.New(logger, positions.Config{
		SyncPeriod:    10 * time.Millisecond,
		PositionsFile: positionsFileName,
	})
	require.NoError(t, err)

	client := fake.New(func() {})
	defer client.Stop()

	pathToWatch := filepath.Join(tempDir, "*.log")
	registry := prometheus.NewRegistry()
	metrics := NewMetrics(registry)

	// Increase this to several thousand to make the test more likely to fail when debugging a race condition
	iterations := 500
	fakeHandler := make(chan fileTargetEvent, 10*iterations)
	for i := 0; i < iterations; i++ {
		logFile := filepath.Join(tempDir, fmt.Sprintf("test_%d.log", i))

		target, err := NewFileTarget(metrics, logger, client, ps, pathToWatch, "", nil, nil, &Config{
			SyncPeriod: 10 * time.Millisecond,
		}, DefaultWatchConig, nil, fakeHandler, "", nil)
		assert.NoError(t, err)

		file, err := os.Create(logFile)
		assert.NoError(t, err)

		// Write some data to the file
		for j := 0; j < 5; j++ {
			_, _ = file.WriteString(fmt.Sprintf("test %d\n", j))
		}
		require.NoError(t, file.Close())

		requireEventually(t, func() bool {
			return testutil.CollectAndCount(registry, "promtail_read_lines_total") == 1
		}, "expected 1 read_lines_total metric")

		requireEventually(t, func() bool {
			return testutil.CollectAndCount(registry, "promtail_read_bytes_total") == 1
		}, "expected 1 read_bytes_total metric")

		requireEventually(t, func() bool {
			return testutil.ToFloat64(metrics.readLines) == 5
		}, "expected 5 read_lines_total")

		requireEventually(t, func() bool {
			return testutil.ToFloat64(metrics.totalBytes) == 35
		}, "expected 35 total_bytes")

		requireEventually(t, func() bool {
			return testutil.ToFloat64(metrics.readBytes) == 35
		}, "expected 35 read_bytes")

		// Concurrently stop the target and remove the file
		wg := sync.WaitGroup{}
		wg.Add(2)
		go func() {
			sleepRandomDuration(time.Millisecond * 10)
			target.Stop()
			wg.Done()

		}()
		go func() {
			sleepRandomDuration(time.Millisecond * 10)
			_ = os.Remove(logFile)
			wg.Done()
		}()

		wg.Wait()

		requireEventually(t, func() bool {
			return testutil.CollectAndCount(registry, "promtail_read_bytes_total") == 0
		}, "expected read_bytes_total metric to be cleaned up")

		requireEventually(t, func() bool {
			return testutil.CollectAndCount(registry, "promtail_file_bytes_total") == 0
		}, "expected file_bytes_total metric to be cleaned up")
	}

	ps.Stop()
}

// Make sure that Stop() doesn't hang if FileTarget is waiting on a channel send.
func TestFileTarget_StopAbruptly(t *testing.T) {
	w := log.NewSyncWriter(os.Stderr)
	logger := log.NewLogfmtLogger(w)

	dirName := newTestLogDirectories(t)
	positionsFileName := filepath.Join(dirName, "positions.yml")
	logDir1 := filepath.Join(dirName, "log1")
	logDir2 := filepath.Join(dirName, "log2")
	logDir3 := filepath.Join(dirName, "log3")

	logfile1 := filepath.Join(logDir1, "test1.log")
	logfile2 := filepath.Join(logDir2, "test1.log")
	logfile3 := filepath.Join(logDir3, "test1.log")

	ps, err := positions.New(logger, positions.Config{
		SyncPeriod:    10 * time.Millisecond,
		PositionsFile: positionsFileName,
	})
	require.NoError(t, err)

	client := fake.New(func() {})
	defer client.Stop()

	// fakeHandler has to be a buffered channel so that we can call the len() function on it.
	// We need to call len() to check if the channel is full.
	fakeHandler := make(chan fileTargetEvent, 1)
	pathToWatch := filepath.Join(dirName, "**", "*.log")
	registry := prometheus.NewRegistry()
	target, err := NewFileTarget(NewMetrics(registry), logger, client, ps, pathToWatch, "", nil, nil, &Config{
		SyncPeriod: 10 * time.Millisecond,
	}, DefaultWatchConig, nil, fakeHandler, "", nil)
	assert.NoError(t, err)

	// Create a directory, still nothing is watched.
	err = os.MkdirAll(logDir1, 0750)
	assert.NoError(t, err)
	_, err = os.Create(logfile1)
	assert.NoError(t, err)

	// There should be only one WatchStart event in the channel so far.
	ftEvent := <-fakeHandler
	require.Equal(t, fileTargetEventWatchStart, ftEvent.eventType)

	requireEventually(t, func() bool {
		return target.getReadersLen() == 1
	}, "expected 1 tailer to be created")

	require.NoError(t, testutil.GatherAndCompare(registry, bytes.NewBufferString(`
		# HELP promtail_files_active_total Number of active files.
		# TYPE promtail_files_active_total gauge
		promtail_files_active_total 1
	`), "promtail_files_active_total"))

	// Create two directories - one more than the buffer of fakeHandler,
	// so that the file target hands until we call Stop().
	err = os.MkdirAll(logDir2, 0750)
	assert.NoError(t, err)
	_, err = os.Create(logfile2)
	assert.NoError(t, err)

	err = os.MkdirAll(logDir3, 0750)
	assert.NoError(t, err)
	_, err = os.Create(logfile3)
	assert.NoError(t, err)

	// Wait until the file target is waiting on a channel send due to a full channel buffer.
	requireEventually(t, func() bool {
		return len(fakeHandler) == 1
	}, "expected an event in the fakeHandler channel")

	// If FileHandler works well, then it will stop waiting for
	// the blocked fakeHandler and stop cleanly.
	// This is why this time we don't drain fakeHandler.
	requireEventually(t, func() bool {
		target.Stop()
		ps.Stop()
		return true
	}, "expected FileTarget not to hang")

	require.NoError(t, testutil.GatherAndCompare(registry, bytes.NewBufferString(`
		# HELP promtail_files_active_total Number of active files.
		# TYPE promtail_files_active_total gauge
		promtail_files_active_total 0
	`), "promtail_files_active_total"))
}

func TestFileTargetPathExclusion(t *testing.T) {
	w := log.NewSyncWriter(os.Stderr)
	logger := log.NewLogfmtLogger(w)

	dirName := newTestLogDirectories(t)
	positionsFileName := filepath.Join(dirName, "positions.yml")
	logDir1 := filepath.Join(dirName, "log1")
	logDir2 := filepath.Join(dirName, "log2")
	logDir3 := filepath.Join(dirName, "log3")
	logFiles := []string{
		filepath.Join(logDir1, "test1.log"),
		filepath.Join(logDir1, "test2.log"),
		filepath.Join(logDir2, "test1.log"),
		filepath.Join(logDir3, "test1.log"),
		filepath.Join(logDir3, "test2.log"),
	}

	// Set the sync period to a really long value, to guarantee the sync timer never runs, this way we know
	// everything saved was done through channel notifications when target.stop() was called.
	ps, err := positions.New(logger, positions.Config{
		SyncPeriod:    10 * time.Minute,
		PositionsFile: positionsFileName,
	})
	if err != nil {
		t.Fatal(err)
	}

	client := fake.New(func() {})
	defer client.Stop()

	metrics := NewMetrics(nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fakeHandler := make(chan fileTargetEvent)
	receivedStartWatch := atomic.NewInt32(0)
	receivedStopWatch := atomic.NewInt32(0)
	go func() {
		for {
			select {
			case event := <-fakeHandler:
				switch event.eventType {
				case fileTargetEventWatchStart:
					receivedStartWatch.Add(1)
				case fileTargetEventWatchStop:
					receivedStopWatch.Add(1)
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	path := filepath.Join(dirName, "**", "*.log")
	pathExclude := filepath.Join(dirName, "log3", "*.log")
	target, err := NewFileTarget(metrics, logger, client, ps, path, pathExclude, nil, nil, &Config{
		SyncPeriod: 1 * time.Minute, // assure the sync is not called by the ticker
	}, DefaultWatchConig, nil, fakeHandler, "", nil)
	assert.NoError(t, err)

	// Start with nothing watched.
	if target.getWatchesLen() != 0 {
		t.Fatal("Expected watches to be 0 at this point in the test...")
	}
	if target.getReadersLen() != 0 {
		t.Fatal("Expected tails to be 0 at this point in the test...")
	}

	// Create the base directories, still nothing watched.
	err = os.MkdirAll(logDir1, 0750)
	assert.NoError(t, err)
	err = os.MkdirAll(logDir2, 0750)
	assert.NoError(t, err)
	err = os.MkdirAll(logDir3, 0750)
	assert.NoError(t, err)

	err = target.sync()
	assert.NoError(t, err)

	if target.getWatchesLen() != 0 {
		t.Fatal("Expected watches to be 0 at this point in the test...")
	}
	if target.getReadersLen() != 0 {
		t.Fatal("Expected tails to be 0 at this point in the test...")
	}

	// Create all the files, which should create two directory watchers and three file tailers.
	for _, f := range logFiles {
		_, err = os.Create(f)
		assert.NoError(t, err)
	}

	// Delay sync() call to make sure the filesystem watch event does not fire during sync()
	time.Sleep(10 * time.Millisecond)
	err = target.sync()
	assert.NoError(t, err)

	assert.Equal(t, 2, target.getWatchesLen(),
		"Expected watches to be 2 at this point in the test...",
	)
	assert.Equal(t, 3, target.getReadersLen(),
		"Expected tails to be 3 at this point in the test...",
	)
	requireEventually(t, func() bool {
		return receivedStartWatch.Load() == 2
	}, "Expected received starting watch event to be 2 at this point in the test...")
	requireEventually(t, func() bool {
		return receivedStopWatch.Load() == 0
	}, "Expected received stopping watch event to be 0 at this point in the test...")

	// Remove the first directory, other tailer should stop and its watchers should go away.
	// Only the non-excluded `logDir2` should be watched.
	err = os.RemoveAll(logDir1)
	assert.NoError(t, err)

	err = target.sync()
	assert.NoError(t, err)

	assert.Equal(t, 1, target.getWatchesLen(),
		"Expected watches to be 1 at this point in the test...",
	)
	assert.Equal(t, 1, target.getReadersLen(),
		"Expected tails to be 1 at this point in the test...",
	)
	requireEventually(t, func() bool {
		return receivedStartWatch.Load() == 2
	}, "Expected received starting watch event to still be 2 at this point in the test...")
	requireEventually(t, func() bool {
		return receivedStopWatch.Load() == 1
	}, "Expected received stopping watch event to be 1 at this point in the test...")

	require.NoError(t, os.RemoveAll(logDir2))
	require.NoError(t, os.RemoveAll(logDir3))
	require.NoError(t, target.sync())

	target.Stop()
	ps.Stop()
}

func TestHandleFileCreationEvent(t *testing.T) {
	w := log.NewSyncWriter(os.Stderr)
	logger := log.NewLogfmtLogger(w)

	dirName := newTestLogDirectories(t)
	positionsFileName := filepath.Join(dirName, "positions.yml")
	logDir := filepath.Join(dirName, "log")
	logFile := filepath.Join(logDir, "test1.log")
	logFileIgnored := filepath.Join(logDir, "test.donot.log")

	if err := os.MkdirAll(logDir, 0750); err != nil {
		t.Fatal(err)
	}

	// Set the sync period to a really long value, to guarantee the sync timer never runs, this way we know
	// everything saved was done through channel notifications when target.stop() was called.
	ps, err := positions.New(logger, positions.Config{
		SyncPeriod:    10 * time.Minute,
		PositionsFile: positionsFileName,
	})
	if err != nil {
		t.Fatal(err)
	}

	client := fake.New(func() {})
	defer client.Stop()

	metrics := NewMetrics(nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fakeFileHandler := make(chan fsnotify.Event)
	fakeTargetHandler := make(chan fileTargetEvent)
	path := logDir + "/*.log"
	go func() {
		for {
			select {
			case <-fakeTargetHandler:
				continue
			case <-ctx.Done():
				return
			}
		}
	}()

	pathExclude := "**/*.donot.log"
	target, err := NewFileTarget(metrics, logger, client, ps, path, pathExclude, nil, nil, &Config{
		// To handle file creation event from channel, set enough long time as sync period
		SyncPeriod: 10 * time.Minute,
	}, DefaultWatchConig, fakeFileHandler, fakeTargetHandler, "", nil)
	if err != nil {
		t.Fatal(err)
	}

	_, err = os.Create(logFile)
	if err != nil {
		t.Fatal(err)
	}
	_, err = os.Create(logFileIgnored)
	if err != nil {
		t.Fatal(err)
	}
	fakeFileHandler <- fsnotify.Event{
		Name: logFile,
		Op:   fsnotify.Create,
	}
	fakeFileHandler <- fsnotify.Event{
		Name: logFileIgnored,
		Op:   fsnotify.Create,
	}
	requireEventually(t, func() bool {
		return target.getReadersLen() == 1
	}, "Expected tails to be 1 at this point in the test...")
}

func TestToStopTailing(t *testing.T) {
	nt := []string{"file1", "file2", "file3", "file4", "file5", "file6", "file7", "file11", "file12", "file15"}
	et := make(map[string]Reader, 15)
	for i := 1; i <= 15; i++ {
		et[fmt.Sprintf("file%d", i)] = nil
	}
	st := toStopTailing(nt, et)
	sort.Strings(st)
	expected := []string{"file10", "file13", "file14", "file8", "file9"}
	if len(st) != len(expected) {
		t.Error("Expected 5 tailers to be stopped, got", len(st))
	}
	for i := range expected {
		if st[i] != expected[i] {
			t.Error("Results mismatch, expected", expected[i], "got", st[i])
		}
	}

}

func BenchmarkToStopTailing(b *testing.B) {
	nt := []string{"file1", "file2", "file3", "file4", "file5", "file6", "file7", "file11", "file12", "file15"}
	et := make(map[string]Reader, 15)
	for i := 1; i <= 15; i++ {
		et[fmt.Sprintf("file%d", i)] = nil
	}
	for n := 0; n < b.N; n++ {
		toStopTailing(nt, et)
	}
}

func TestMissing(t *testing.T) {
	a := map[string]struct{}{}
	b := map[string]struct{}{}

	c := missing(a, b)
	if len(c) != 0 {
		t.Error("Expected no results with empty sets")
	}

	a["str1"] = struct{}{}
	a["str2"] = struct{}{}
	a["str3"] = struct{}{}
	c = missing(a, b)
	if len(c) != 0 {
		t.Error("Expected no results with empty b set")
	}
	c = missing(b, a)
	if len(c) != 3 {
		t.Error("Expected three results")
	}
	if _, ok := c["str1"]; !ok {
		t.Error("Expected the set to contain str1 but it did not")
	}
	if _, ok := c["str2"]; !ok {
		t.Error("Expected the set to contain str2 but it did not")
	}
	if _, ok := c["str3"]; !ok {
		t.Error("Expected the set to contain str3 but it did not")
	}

	b["str1"] = struct{}{}
	b["str4"] = struct{}{}
	c = missing(a, b)
	if len(c) != 1 {
		t.Error("Expected one result")
	}
	if _, ok := c["str4"]; !ok {
		t.Error("Expected the set to contain str4 but it did not")
	}

	c = missing(b, a)
	if len(c) != 2 {
		t.Error("Expected two results")
	}
	if _, ok := c["str2"]; !ok {
		t.Error("Expected the set to contain str2 but it did not")
	}
	if _, ok := c["str3"]; !ok {
		t.Error("Expected the set to contain str3 but it did not")
	}

}

func requireEventually(t *testing.T, f func() bool, msg string) {
	t.Helper()
	require.Eventually(t, f, time.Second*10, time.Millisecond, msg)
}

func sleepRandomDuration(maxDuration time.Duration) {
	time.Sleep(time.Duration(rand.Int63n(int64(maxDuration))))
}
