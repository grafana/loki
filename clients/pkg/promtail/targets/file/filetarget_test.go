package file

import (
	"context"
	"fmt"
	"os"
	"sort"
	"testing"
	"time"

	"go.uber.org/atomic"
	"gopkg.in/fsnotify.v1"

	"github.com/go-kit/log"

	"github.com/grafana/loki/clients/pkg/promtail/client/fake"
	"github.com/grafana/loki/clients/pkg/promtail/positions"
	"github.com/grafana/loki/clients/pkg/promtail/targets/testutils"
)

func TestFileTargetSync(t *testing.T) {
	w := log.NewSyncWriter(os.Stderr)
	logger := log.NewLogfmtLogger(w)

	testutils.InitRandom()
	dirName := "/tmp/" + testutils.RandName()
	positionsFileName := dirName + "/positions.yml"
	logDir1 := dirName + "/log1"
	logDir1File1 := logDir1 + "/test1.log"
	logDir1File2 := logDir1 + "/test2.log"

	err := os.MkdirAll(dirName, 0750)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(dirName) }()

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
	target, err := NewFileTarget(metrics, logger, client, ps, path, nil, nil, &Config{
		SyncPeriod: 10 * time.Second,
	}, nil, fakeHandler)
	if err != nil {
		t.Fatal(err)
	}

	// Start with nothing watched.
	if len(target.watches) != 0 {
		t.Fatal("Expected watches to be 0 at this point in the test...")
	}
	if len(target.tails) != 0 {
		t.Fatal("Expected tails to be 0 at this point in the test...")
	}
	if receivedStartWatch.Load() != 0 {
		t.Fatal("Expected received starting watch event to be 0 at this point in the test...")
	}
	if receivedStopWatch.Load() != 0 {
		t.Fatal("Expected received stopping watch event to be 0 at this point in the test...")
	}

	// Create the base dir, still nothing watched.
	if err = os.MkdirAll(logDir1, 0750); err != nil {
		t.Fatal(err)
	}
	if err = target.sync(); err != nil {
		t.Fatal(err)
	}
	if len(target.watches) != 0 {
		t.Fatal("Expected watches to be 0 at this point in the test...")
	}
	if len(target.tails) != 0 {
		t.Fatal("Expected tails to be 0 at this point in the test...")
	}
	if receivedStartWatch.Load() != 0 {
		t.Fatal("Expected received starting watch event to be 0 at this point in the test...")
	}
	if receivedStopWatch.Load() != 0 {
		t.Fatal("Expected received stopping watch event to be 0 at this point in the test...")
	}

	// Add a file, which should create a watcher and a tailer.
	_, err = os.Create(logDir1File1)
	if err != nil {
		t.Fatal(err)
	}
	if err = target.sync(); err != nil {
		t.Fatal(err)
	}
	if len(target.watches) != 1 {
		t.Fatal("Expected watches to be 1 at this point in the test...")
	}
	if len(target.tails) != 1 {
		t.Fatal("Expected tails to be 1 at this point in the test...")
	}
	if receivedStartWatch.Load() != 1 {
		t.Fatal("Expected received starting watch event to be 1 at this point in the test...")
	}
	if receivedStopWatch.Load() != 0 {
		t.Fatal("Expected received stopping watch event to be 0 at this point in the test...")
	}

	// Add another file, should get another tailer.
	_, err = os.Create(logDir1File2)
	if err != nil {
		t.Fatal(err)
	}
	if err = target.sync(); err != nil {
		t.Fatal(err)
	}
	if len(target.watches) != 1 {
		t.Fatal("Expected watches to be 1 at this point in the test...")
	}
	if len(target.tails) != 2 {
		t.Fatal("Expected tails to be 2 at this point in the test...")
	}
	if receivedStartWatch.Load() != 1 {
		t.Fatal("Expected received starting watch event to be 1 at this point in the test...")
	}
	if receivedStopWatch.Load() != 0 {
		t.Fatal("Expected received stopping watch event to be 0 at this point in the test...")
	}

	// Remove one of the files, tailer should stop.
	if err = os.Remove(logDir1File1); err != nil {
		t.Fatal(err)
	}
	if err = target.sync(); err != nil {
		t.Fatal(err)
	}
	if len(target.watches) != 1 {
		t.Fatal("Expected watches to be 1 at this point in the test...")
	}
	if len(target.tails) != 1 {
		t.Fatal("Expected tails to be 1 at this point in the test...")
	}
	if receivedStartWatch.Load() != 1 {
		t.Fatal("Expected received starting watch event to be 1 at this point in the test...")
	}
	if receivedStopWatch.Load() != 0 {
		t.Fatal("Expected received stopping watch event to be 0 at this point in the test...")
	}

	// Remove the entire directory, other tailer should stop and watcher should go away.
	if err = os.RemoveAll(logDir1); err != nil {
		t.Fatal(err)
	}
	if err = target.sync(); err != nil {
		t.Fatal(err)
	}
	if len(target.watches) != 0 {
		t.Fatal("Expected watches to be 0 at this point in the test...")
	}
	if len(target.tails) != 0 {
		t.Fatal("Expected tails to be 0 at this point in the test...")
	}
	if receivedStartWatch.Load() != 1 {
		t.Fatal("Expected received starting watch event to be 1 at this point in the test...")
	}
	if receivedStopWatch.Load() != 1 {
		t.Fatal("Expected received stopping watch event to be 1 at this point in the test...")
	}

	target.Stop()
	ps.Stop()

}

func TestHandleFileCreationEvent(t *testing.T) {
	w := log.NewSyncWriter(os.Stderr)
	logger := log.NewLogfmtLogger(w)

	testutils.InitRandom()
	dirName := "/tmp/" + testutils.RandName()
	positionsFileName := dirName + "/positions.yml"
	logDir := dirName + "/log"
	logFile := logDir + "/test1.log"

	err := os.MkdirAll(dirName, 0750)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(dirName) }()
	if err = os.MkdirAll(logDir, 0750); err != nil {
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

	target, err := NewFileTarget(metrics, logger, client, ps, path, nil, nil, &Config{
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
	countdown := 10000
	for len(target.tails) != 1 && countdown > 0 {
		time.Sleep(1 * time.Millisecond)
		countdown--
	}
	if len(target.tails) != 1 {
		t.Fatal("Expected tails to be 1 at this point in the test...")
	}
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
