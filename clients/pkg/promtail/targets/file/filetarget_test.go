package file

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.uber.org/atomic"
	"gopkg.in/fsnotify.v1"

	"github.com/go-kit/log"

	"github.com/grafana/loki/clients/pkg/promtail/client/fake"
	"github.com/grafana/loki/clients/pkg/promtail/positions"
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
	}, nil, fakeHandler)
	assert.NoError(t, err)

	// Start with nothing watched.
	if len(target.watches) != 0 {
		t.Fatal("Expected watches to be 0 at this point in the test...")
	}
	if len(target.tails) != 0 {
		t.Fatal("Expected tails to be 0 at this point in the test...")
	}

	// Create the base dir, still nothing watched.
	err = os.MkdirAll(logDir1, 0750)
	assert.NoError(t, err)

	err = target.sync()
	assert.NoError(t, err)

	if len(target.watches) != 0 {
		t.Fatal("Expected watches to be 0 at this point in the test...")
	}
	if len(target.tails) != 0 {
		t.Fatal("Expected tails to be 0 at this point in the test...")
	}

	// Add a file, which should create a watcher and a tailer.
	_, err = os.Create(logDir1File1)
	assert.NoError(t, err)

	// Delay sync() call to make sure the filesystem watch event does not fire during sync()
	time.Sleep(10 * time.Millisecond)
	err = target.sync()
	assert.NoError(t, err)

	assert.Equal(t, 1, len(target.watches),
		"Expected watches to be 1 at this point in the test...",
	)
	assert.Equal(t, 1, len(target.tails),
		"Expected tails to be 1 at this point in the test...",
	)
	require.Eventually(t, func() bool {
		return receivedStartWatch.Load() == 1
	}, time.Second*10, time.Millisecond*1, "Expected received starting watch event to be 1 at this point in the test...")

	// Add another file, should get another tailer.
	_, err = os.Create(logDir1File2)
	assert.NoError(t, err)

	err = target.sync()
	assert.NoError(t, err)

	assert.Equal(t, 1, len(target.watches),
		"Expected watches to be 1 at this point in the test...",
	)
	assert.Equal(t, 2, len(target.tails),
		"Expected tails to be 2 at this point in the test...",
	)

	// Remove one of the files, tailer should stop.
	err = os.Remove(logDir1File1)
	assert.NoError(t, err)

	err = target.sync()
	assert.NoError(t, err)

	assert.Equal(t, 1, len(target.watches),
		"Expected watches to be 1 at this point in the test...",
	)
	assert.Equal(t, 1, len(target.tails),
		"Expected tails to be 1 at this point in the test...",
	)

	// Remove the entire directory, other tailer should stop and watcher should go away.
	err = os.RemoveAll(logDir1)
	assert.NoError(t, err)

	err = target.sync()
	assert.NoError(t, err)

	assert.Equal(t, 0, len(target.watches),
		"Expected watches to be 0 at this point in the test...",
	)
	assert.Equal(t, 0, len(target.tails),
		"Expected tails to be 0 at this point in the test...",
	)
	require.Eventually(t, func() bool {
		return receivedStartWatch.Load() == 1
	}, time.Second*10, time.Millisecond*1, "Expected received starting watch event to be 1 at this point in the test...")
	require.Eventually(t, func() bool {
		return receivedStartWatch.Load() == 1
	}, time.Second*10, time.Millisecond*1, "Expected received stopping watch event to be 1 at this point in the test...")

	target.Stop()
	ps.Stop()
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
	}, nil, fakeHandler)
	assert.NoError(t, err)

	// Start with nothing watched.
	if len(target.watches) != 0 {
		t.Fatal("Expected watches to be 0 at this point in the test...")
	}
	if len(target.tails) != 0 {
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

	if len(target.watches) != 0 {
		t.Fatal("Expected watches to be 0 at this point in the test...")
	}
	if len(target.tails) != 0 {
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

	assert.Equal(t, 2, len(target.watches),
		"Expected watches to be 2 at this point in the test...",
	)
	assert.Equal(t, 3, len(target.tails),
		"Expected tails to be 3 at this point in the test...",
	)
	require.Eventually(t, func() bool {
		return receivedStartWatch.Load() == 2
	}, time.Second*10, time.Millisecond*1, "Expected received starting watch event to be 2 at this point in the test...")
	require.Eventually(t, func() bool {
		return receivedStopWatch.Load() == 0
	}, time.Second*10, time.Millisecond*1, "Expected received stopping watch event to be 0 at this point in the test...")

	// Remove the first directory, other tailer should stop and its watchers should go away.
	// Only the non-excluded `logDir2` should be watched.
	err = os.RemoveAll(logDir1)
	assert.NoError(t, err)

	err = target.sync()
	assert.NoError(t, err)

	assert.Equal(t, 1, len(target.watches),
		"Expected watches to be 1 at this point in the test...",
	)
	assert.Equal(t, 1, len(target.tails),
		"Expected tails to be 1 at this point in the test...",
	)
	require.Eventually(t, func() bool {
		return receivedStartWatch.Load() == 2
	}, time.Second*10, time.Millisecond*1, "Expected received starting watch event to still be 2 at this point in the test...")
	require.Eventually(t, func() bool {
		return receivedStopWatch.Load() == 1
	}, time.Second*10, time.Millisecond*1, "Expected received stopping watch event to be 1 at this point in the test...")

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

	target, err := NewFileTarget(metrics, logger, client, ps, path, "", nil, nil, &Config{
		// To handle file creation event from channel, set enough long time as sync period
		SyncPeriod: 10 * time.Minute,
	}, fakeFileHandler, fakeTargetHandler)
	if err != nil {
		t.Fatal(err)
	}

	_, err = os.Create(logFile)
	if err != nil {
		t.Fatal(err)
	}
	fakeFileHandler <- fsnotify.Event{
		Name: logFile,
		Op:   fsnotify.Create,
	}
	require.Eventually(t, func() bool {
		return len(target.tails) == 1
	}, time.Second*10, time.Millisecond*1, "Expected tails to be 1 at this point in the test...")
}

func TestToStopTailing(t *testing.T) {
	nt := []string{"file1", "file2", "file3", "file4", "file5", "file6", "file7", "file11", "file12", "file15"}
	et := make(map[string]*tailer, 15)
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
	et := make(map[string]*tailer, 15)
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
