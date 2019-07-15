package targets

import (
	"fmt"
	"io/ioutil"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/common/model"
	"gopkg.in/yaml.v2"

	"github.com/grafana/loki/pkg/promtail/positions"
)

func TestLongPositionsSyncDelayStillSavesCorrectPosition(t *testing.T) {
	w := log.NewSyncWriter(os.Stderr)
	logger := log.NewLogfmtLogger(w)

	initRandom()
	dirName := "/tmp/" + randName()
	positionsFileName := dirName + "/positions.yml"
	logFile := dirName + "/test.log"

	err := os.MkdirAll(dirName, 0750)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(dirName) }()

	// Set the sync period to a really long value, to guarantee the sync timer never runs, this way we know
	// everything saved was done through channel notifications when target.stop() was called.
	ps, err := positions.New(logger, positions.Config{
		SyncPeriod:    10 * time.Second,
		PositionsFile: positionsFileName,
	})
	if err != nil {
		t.Fatal(err)
	}

	client := &TestClient{
		log:      logger,
		messages: make([]string, 0),
	}

	f, err := os.Create(logFile)
	if err != nil {
		t.Fatal(err)
	}

	target, err := NewFileTarget(logger, client, ps, logFile, nil, nil, &Config{
		SyncPeriod: 10 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 10; i++ {
		_, err = f.WriteString("test\n")
		if err != nil {
			t.Fatal(err)
		}
		time.Sleep(1 * time.Millisecond)
	}

	countdown := 10000
	for len(client.messages) != 10 && countdown > 0 {
		time.Sleep(1 * time.Millisecond)
		countdown--
	}

	target.Stop()
	ps.Stop()

	buf, err := ioutil.ReadFile(filepath.Clean(positionsFileName))
	if err != nil {
		t.Fatal("Expected to find a positions file but did not", err)
	}
	var p positions.File
	if err := yaml.UnmarshalStrict(buf, &p); err != nil {
		t.Fatal("Failed to parse positions file:", err)
	}

	// Assert the position value is in the correct spot.
	if val, ok := p.Positions[logFile]; ok {
		if val != "50" {
			t.Error("Incorrect position found, expected 50, found", val)
		}
	} else {
		t.Error("Positions file did not contain any data for our test log file")
	}

	// Assert the number of messages the handler received is correct.
	if len(client.messages) != 10 {
		t.Error("Handler did not receive the correct number of messages, expected 10 received", len(client.messages))
	}

	// Spot check one of the messages.
	if client.messages[0] != "test" {
		t.Error("Expected first log message to be 'test' but was", client.messages[0])
	}

}

func TestWatchEntireDirectory(t *testing.T) {
	w := log.NewSyncWriter(os.Stderr)
	logger := log.NewLogfmtLogger(w)

	initRandom()
	dirName := "/tmp/" + randName()
	positionsFileName := dirName + "/positions.yml"
	logFileDir := dirName + "/logdir/"

	err := os.MkdirAll(dirName, 0750)
	if err != nil {
		t.Fatal(err)
	}
	err = os.MkdirAll(logFileDir, 0750)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(dirName) }()

	// Set the sync period to a really long value, to guarantee the sync timer never runs, this way we know
	// everything saved was done through channel notifications when target.stop() was called.
	ps, err := positions.New(logger, positions.Config{
		SyncPeriod:    10 * time.Second,
		PositionsFile: positionsFileName,
	})
	if err != nil {
		t.Fatal(err)
	}

	client := &TestClient{
		log:      logger,
		messages: make([]string, 0),
	}

	f, err := os.Create(logFileDir + "test.log")
	if err != nil {
		t.Fatal(err)
	}

	target, err := NewFileTarget(logger, client, ps, logFileDir+"*", nil, nil, &Config{
		SyncPeriod: 10 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 10; i++ {
		_, err = f.WriteString("test\n")
		if err != nil {
			t.Fatal(err)
		}
		time.Sleep(1 * time.Millisecond)
	}

	countdown := 10000
	for len(client.messages) != 10 && countdown > 0 {
		time.Sleep(1 * time.Millisecond)
		countdown--
	}

	target.Stop()
	ps.Stop()

	buf, err := ioutil.ReadFile(filepath.Clean(positionsFileName))
	if err != nil {
		t.Fatal("Expected to find a positions file but did not", err)
	}
	var p positions.File
	if err := yaml.UnmarshalStrict(buf, &p); err != nil {
		t.Fatal("Failed to parse positions file:", err)
	}

	// Assert the position value is in the correct spot.
	if val, ok := p.Positions[logFileDir+"test.log"]; ok {
		if val != "50" {
			t.Error("Incorrect position found, expected 50, found", val)
		}
	} else {
		t.Error("Positions file did not contain any data for our test log file")
	}

	// Assert the number of messages the handler received is correct.
	if len(client.messages) != 10 {
		t.Error("Handler did not receive the correct number of messages, expected 10 received", len(client.messages))
	}

	// Spot check one of the messages.
	if client.messages[0] != "test" {
		t.Error("Expected first log message to be 'test' but was", client.messages[0])
	}

}

func TestFileRolls(t *testing.T) {
	w := log.NewSyncWriter(os.Stderr)
	logger := log.NewLogfmtLogger(w)

	initRandom()
	dirName := "/tmp/" + randName()
	positionsFile := dirName + "/positions.yml"
	logFile := dirName + "/test.log"

	err := os.MkdirAll(dirName, 0750)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(dirName) }()

	// Set the sync period to a really long value, to guarantee the sync timer never runs, this way we know
	// everything saved was done through channel notifications when target.stop() was called.
	positions, err := positions.New(logger, positions.Config{
		SyncPeriod:    10 * time.Second,
		PositionsFile: positionsFile,
	})
	if err != nil {
		t.Fatal(err)
	}

	client := &TestClient{
		log:      logger,
		messages: make([]string, 0),
	}

	f, err := os.Create(logFile)
	if err != nil {
		t.Fatal(err)
	}

	target, err := NewFileTarget(logger, client, positions, dirName+"/*.log", nil, nil, &Config{
		SyncPeriod: 10 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 10; i++ {
		_, err = f.WriteString("test1\n")
		if err != nil {
			t.Fatal(err)
		}
		time.Sleep(1 * time.Millisecond)
	}

	countdown := 10000
	for len(client.messages) != 10 && countdown > 0 {
		time.Sleep(1 * time.Millisecond)
		countdown--
	}

	// Rename the log file to something not in the pattern, then create a new file with the same name.
	err = os.Rename(logFile, dirName+"/test.log.1")
	if err != nil {
		t.Fatal("Failed to rename log file for test", err)
	}
	f, err = os.Create(logFile)
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 10; i++ {
		_, err = f.WriteString("test2\n")
		if err != nil {
			t.Fatal(err)
		}
		time.Sleep(1 * time.Millisecond)
	}

	countdown = 10000
	for len(client.messages) != 20 && countdown > 0 {
		time.Sleep(1 * time.Millisecond)
		countdown--
	}

	target.Stop()
	positions.Stop()

	if len(client.messages) != 20 {
		t.Error("Handler did not receive the correct number of messages, expected 20 received", len(client.messages))
	}

	// Spot check one of the messages.
	if client.messages[0] != "test1" {
		t.Error("Expected first log message to be 'test1' but was", client.messages[0])
	}

	// Spot check the first message from the second file.
	if client.messages[10] != "test2" {
		t.Error("Expected first log message to be 'test2' but was", client.messages[10])
	}
}

func TestResumesWhereLeftOff(t *testing.T) {
	w := log.NewSyncWriter(os.Stderr)
	logger := log.NewLogfmtLogger(w)

	initRandom()
	dirName := "/tmp/" + randName()
	positionsFileName := dirName + "/positions.yml"
	logFile := dirName + "/test.log"

	err := os.MkdirAll(dirName, 0750)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(dirName) }()

	// Set the sync period to a really long value, to guarantee the sync timer never runs, this way we know
	// everything saved was done through channel notifications when target.stop() was called.
	ps, err := positions.New(logger, positions.Config{
		SyncPeriod:    10 * time.Second,
		PositionsFile: positionsFileName,
	})
	if err != nil {
		t.Fatal(err)
	}

	client := &TestClient{
		log:      logger,
		messages: make([]string, 0),
	}

	f, err := os.Create(logFile)
	if err != nil {
		t.Fatal(err)
	}

	target, err := NewFileTarget(logger, client, ps, dirName+"/*.log", nil, nil, &Config{
		SyncPeriod: 10 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 10; i++ {
		_, err = f.WriteString("test1\n")
		if err != nil {
			t.Fatal(err)
		}
		time.Sleep(1 * time.Millisecond)
	}

	countdown := 10000
	for len(client.messages) != 10 && countdown > 0 {
		time.Sleep(1 * time.Millisecond)
		countdown--
	}

	target.Stop()
	ps.Stop()

	// Create another positions (so that it loads from the previously saved positions file).
	ps2, err := positions.New(logger, positions.Config{
		SyncPeriod:    10 * time.Second,
		PositionsFile: positionsFileName,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Create a new target, keep the same client so we can track what was sent through the handler.
	target2, err := NewFileTarget(logger, client, ps2, dirName+"/*.log", nil, nil, &Config{
		SyncPeriod: 10 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 10; i++ {
		_, err = f.WriteString("test2\n")
		if err != nil {
			t.Fatal(err)
		}
		time.Sleep(1 * time.Millisecond)
	}

	countdown = 10000
	for len(client.messages) != 20 && countdown > 0 {
		time.Sleep(1 * time.Millisecond)
		countdown--
	}

	target2.Stop()
	ps2.Stop()

	if len(client.messages) != 20 {
		t.Error("Handler did not receive the correct number of messages, expected 20 received", len(client.messages))
	}

	// Spot check one of the messages.
	if client.messages[0] != "test1" {
		t.Error("Expected first log message to be 'test1' but was", client.messages[0])
	}

	// Spot check the first message from the second file.
	if client.messages[10] != "test2" {
		t.Error("Expected first log message to be 'test2' but was", client.messages[10])
	}
}

func TestGlobWithMultipleFiles(t *testing.T) {
	w := log.NewSyncWriter(os.Stderr)
	logger := log.NewLogfmtLogger(w)

	initRandom()
	dirName := "/tmp/" + randName()
	positionsFileName := dirName + "/positions.yml"
	logFile1 := dirName + "/test.log"
	logFile2 := dirName + "/dirt.log"

	err := os.MkdirAll(dirName, 0750)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(dirName) }()

	// Set the sync period to a really long value, to guarantee the sync timer never runs, this way we know
	// everything saved was done through channel notifications when target.stop() was called.
	ps, err := positions.New(logger, positions.Config{
		SyncPeriod:    10 * time.Second,
		PositionsFile: positionsFileName,
	})
	if err != nil {
		t.Fatal(err)
	}

	client := &TestClient{
		log:      logger,
		messages: make([]string, 0),
	}

	f1, err := os.Create(logFile1)
	if err != nil {
		t.Fatal(err)
	}

	target, err := NewFileTarget(logger, client, ps, dirName+"/*.log", nil, nil, &Config{
		SyncPeriod: 10 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}

	f2, err := os.Create(logFile2)
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 10; i++ {
		_, err = f1.WriteString("test1\n")
		if err != nil {
			t.Fatal(err)
		}
		time.Sleep(1 * time.Millisecond)
		_, err = f2.WriteString("dirt1\n")
		if err != nil {
			t.Fatal(err)
		}
		time.Sleep(1 * time.Millisecond)
	}

	countdown := 10000
	for len(client.messages) != 20 && countdown > 0 {
		time.Sleep(1 * time.Millisecond)
		countdown--
	}

	target.Stop()
	ps.Stop()

	buf, err := ioutil.ReadFile(filepath.Clean(positionsFileName))
	if err != nil {
		t.Fatal("Expected to find a positions file but did not", err)
	}
	var p positions.File
	if err := yaml.UnmarshalStrict(buf, &p); err != nil {
		t.Fatal("Failed to parse positions file:", err)
	}

	// Assert the position value is in the correct spot.
	if val, ok := p.Positions[logFile1]; ok {
		if val != "60" {
			t.Error("Incorrect position found for file 1, expected 60, found", val)
		}
	} else {
		t.Error("Positions file did not contain any data for our test log file")
	}
	if val, ok := p.Positions[logFile2]; ok {
		if val != "60" {
			t.Error("Incorrect position found for file 2, expected 60, found", val)
		}
	} else {
		t.Error("Positions file did not contain any data for our test log file")
	}

	// Assert the number of messages the handler received is correct.
	if len(client.messages) != 20 {
		t.Error("Handler did not receive the correct number of messages, expected 20 received", len(client.messages))
	}

}

func TestFileTargetSync(t *testing.T) {
	w := log.NewSyncWriter(os.Stderr)
	logger := log.NewLogfmtLogger(w)

	initRandom()
	dirName := "/tmp/" + randName()
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

	client := &TestClient{
		log:      logger,
		messages: make([]string, 0),
	}

	target, err := NewFileTarget(logger, client, ps, logDir1+"/*.log", nil, nil, &Config{
		SyncPeriod: 10 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Close the watcher so no fsnotify events occur.
	if err = target.watcher.Close(); err != nil {
		t.Fatal(err)
	}

	// Start with nothing watched.
	if len(target.watches) != 0 {
		t.Fatal("Expected watches to be 0 at this point in the test...")
	}
	if len(target.tails) != 0 {
		t.Fatal("Expected tails to be 0 at this point in the test...")
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

	target.Stop()
	ps.Stop()

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

type TestClient struct {
	log      log.Logger
	messages []string
	sync.Mutex
}

func (c *TestClient) Handle(ls model.LabelSet, t time.Time, s string) error {
	level.Debug(c.log).Log("msg", "received log", "log", s)

	c.Lock()
	defer c.Unlock()
	c.messages = append(c.messages, s)
	return nil
}

func initRandom() {
	rand.Seed(time.Now().UnixNano())
}

var letters = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")

func randName() string {
	b := make([]rune, 10)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}
