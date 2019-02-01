package targets

import (
	"io/ioutil"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-kit/kit/log/level"
	"github.com/grafana/loki/pkg/promtail/api"
	"github.com/grafana/loki/pkg/promtail/positions"

	"github.com/go-kit/kit/log"
	"github.com/prometheus/common/model"
	yaml "gopkg.in/yaml.v2"
)

func TestLongSyncDelayStillSavesCorrectPosition(t *testing.T) {
	w := log.NewSyncWriter(os.Stderr)
	logger := log.NewLogfmtLogger(w)

	initRandom()
	dirName := "/tmp/" + randName()
	positionsFileName := dirName + "/positions.yml"
	logFile := dirName + "/test.log"

	err := os.MkdirAll(dirName, 0750)
	if err != nil {
		t.Error(err)
		return
	}
	defer os.RemoveAll(dirName)

	// Set the sync period to a really long value, to guarantee the sync timer never runs, this way we know
	// everything saved was done through channel notifications when target.stop() was called.
	ps, err := positions.New(logger, positions.Config{
		SyncPeriod:    10 * time.Second,
		PositionsFile: positionsFileName,
	})
	if err != nil {
		t.Error(err)
		return
	}

	client := &TestClient{
		log:      logger,
		messages: make([]string, 0),
	}

	target, err := NewFileTarget(logger, client, ps, logFile, nil, &api.TargetConfig{
		SyncPeriod: 10 * time.Second,
	})
	if err != nil {
		t.Error(err)
		return
	}

	f, err := os.Create(logFile)
	if err != nil {
		t.Error(err)
		return
	}

	for i := 0; i < 10; i++ {
		_, err = f.WriteString("test\n")
		if err != nil {
			t.Error(err)
			return
		}
		time.Sleep(1 * time.Millisecond)
	}

	target.Stop()
	ps.Stop()

	buf, err := ioutil.ReadFile(filepath.Clean(positionsFileName))
	if err != nil {
		t.Error("Expected to find a positions file but did not", err)
		return
	}
	var p positions.File
	if err := yaml.UnmarshalStrict(buf, &p); err != nil {
		t.Error("Failed to parse positions file:", err)
		return
	}

	// Assert the position value is in the correct spot.
	if val, ok := p.Positions[logFile]; ok {
		if val != 50 {
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
		t.Error(err)
		return
	}
	err = os.MkdirAll(logFileDir, 0750)
	if err != nil {
		t.Error(err)
		return
	}
	defer os.RemoveAll(dirName)

	// Set the sync period to a really long value, to guarantee the sync timer never runs, this way we know
	// everything saved was done through channel notifications when target.stop() was called.
	ps, err := positions.New(logger, positions.Config{
		SyncPeriod:    10 * time.Second,
		PositionsFile: positionsFileName,
	})
	if err != nil {
		t.Error(err)
		return
	}

	client := &TestClient{
		log:      logger,
		messages: make([]string, 0),
	}

	target, err := NewFileTarget(logger, client, ps, logFileDir+"*", nil, &api.TargetConfig{
		SyncPeriod: 10 * time.Second,
	})
	if err != nil {
		t.Error(err)
		return
	}

	f, err := os.Create(logFileDir + "test.log")
	if err != nil {
		t.Error(err)
		return
	}

	for i := 0; i < 10; i++ {
		_, err = f.WriteString("test\n")
		if err != nil {
			t.Error(err)
			return
		}
		time.Sleep(1 * time.Millisecond)
	}

	target.Stop()
	ps.Stop()

	buf, err := ioutil.ReadFile(filepath.Clean(positionsFileName))
	if err != nil {
		t.Error("Expected to find a positions file but did not", err)
		return
	}
	var p positions.File
	if err := yaml.UnmarshalStrict(buf, &p); err != nil {
		t.Error("Failed to parse positions file:", err)
		return
	}

	// Assert the position value is in the correct spot.
	if val, ok := p.Positions[logFileDir+"test.log"]; ok {
		if val != 50 {
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
		t.Error(err)
		return
	}
	defer os.RemoveAll(dirName)

	// Set the sync period to a really long value, to guarantee the sync timer never runs, this way we know
	// everything saved was done through channel notifications when target.stop() was called.
	positions, err := positions.New(logger, positions.Config{
		SyncPeriod:    10 * time.Second,
		PositionsFile: positionsFile,
	})
	if err != nil {
		t.Error(err)
		return
	}

	client := &TestClient{
		log:      logger,
		messages: make([]string, 0),
	}

	target, err := NewFileTarget(logger, client, positions, dirName+"/*.log", nil, &api.TargetConfig{
		SyncPeriod: 10 * time.Second,
	})
	if err != nil {
		t.Error(err)
		return
	}

	f, err := os.Create(logFile)
	if err != nil {
		t.Error(err)
		return
	}

	for i := 0; i < 10; i++ {
		_, err = f.WriteString("test1\n")
		if err != nil {
			t.Error(err)
			return
		}
		time.Sleep(1 * time.Millisecond)
	}

	// Rename the log file to something not in the pattern, then create a new file with the same name.
	err = os.Rename(logFile, dirName+"/test.log.1")
	if err != nil {
		t.Error("Failed to rename log file for test", err)
		return
	}
	f, err = os.Create(logFile)
	if err != nil {
		t.Error(err)
		return
	}

	for i := 0; i < 10; i++ {
		_, err = f.WriteString("test2\n")
		if err != nil {
			t.Error(err)
			return
		}
		time.Sleep(1 * time.Millisecond)
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
		t.Error(err)
		return
	}
	defer os.RemoveAll(dirName)

	// Set the sync period to a really long value, to guarantee the sync timer never runs, this way we know
	// everything saved was done through channel notifications when target.stop() was called.
	ps, err := positions.New(logger, positions.Config{
		SyncPeriod:    10 * time.Second,
		PositionsFile: positionsFileName,
	})
	if err != nil {
		t.Error(err)
		return
	}

	client := &TestClient{
		log:      logger,
		messages: make([]string, 0),
	}

	target, err := NewFileTarget(logger, client, ps, dirName+"/*.log", nil, &api.TargetConfig{
		SyncPeriod: 10 * time.Second,
	})
	if err != nil {
		t.Error(err)
		return
	}

	f, err := os.Create(logFile)
	if err != nil {
		t.Error(err)
		return
	}

	for i := 0; i < 10; i++ {
		_, err = f.WriteString("test1\n")
		if err != nil {
			t.Error(err)
			return
		}
		time.Sleep(1 * time.Millisecond)
	}

	target.Stop()
	ps.Stop()

	// Create another positions (so that it loads from the previously saved positions file).
	ps2, err := positions.New(logger, positions.Config{
		SyncPeriod:    10 * time.Second,
		PositionsFile: positionsFileName,
	})
	if err != nil {
		t.Error(err)
		return
	}

	// Create a new target, keep the same client so we can track what was sent through the handler.
	target2, err := NewFileTarget(logger, client, ps2, dirName+"/*.log", nil, &api.TargetConfig{
		SyncPeriod: 10 * time.Second,
	})
	if err != nil {
		t.Error(err)
		return
	}

	for i := 0; i < 10; i++ {
		_, err = f.WriteString("test2\n")
		if err != nil {
			t.Error(err)
			return
		}
		time.Sleep(1 * time.Millisecond)
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
		t.Error(err)
		return
	}
	defer os.RemoveAll(dirName)

	// Set the sync period to a really long value, to guarantee the sync timer never runs, this way we know
	// everything saved was done through channel notifications when target.stop() was called.
	ps, err := positions.New(logger, positions.Config{
		SyncPeriod:    10 * time.Second,
		PositionsFile: positionsFileName,
	})
	if err != nil {
		t.Error(err)
		return
	}

	client := &TestClient{
		log:      logger,
		messages: make([]string, 0),
	}

	target, err := NewFileTarget(logger, client, ps, dirName+"/*.log", nil, &api.TargetConfig{
		SyncPeriod: 10 * time.Second,
	})
	if err != nil {
		t.Error(err)
		return
	}

	f1, err := os.Create(logFile1)
	if err != nil {
		t.Error(err)
		return
	}
	f2, err := os.Create(logFile2)
	if err != nil {
		t.Error(err)
		return
	}

	for i := 0; i < 10; i++ {
		_, err = f1.WriteString("test1\n")
		if err != nil {
			t.Error(err)
			return
		}
		time.Sleep(1 * time.Millisecond)
		_, err = f2.WriteString("dirt1\n")
		if err != nil {
			t.Error(err)
			return
		}
		time.Sleep(1 * time.Millisecond)
	}

	target.Stop()
	ps.Stop()

	buf, err := ioutil.ReadFile(filepath.Clean(positionsFileName))
	if err != nil {
		t.Error("Expected to find a positions file but did not", err)
		return
	}
	var p positions.File
	if err := yaml.UnmarshalStrict(buf, &p); err != nil {
		t.Error("Failed to parse positions file:", err)
		return
	}

	// Assert the position value is in the correct spot.
	if val, ok := p.Positions[logFile1]; ok {
		if val != 60 {
			t.Error("Incorrect position found for file 1, expected 60, found", val)
		}
	} else {
		t.Error("Positions file did not contain any data for our test log file")
	}
	if val, ok := p.Positions[logFile2]; ok {
		if val != 60 {
			t.Error("Incorrect position found for file 2, expected 60, found", val)
		}
	} else {
		t.Error("Positions file did not contain any data for our test log file")
	}

	// Assert the number of messages the handler received is correct.
	if len(client.messages) != 20 {
		t.Error("Handler did not receive the correct number of messages, expected 20 received", len(client.messages))
	}

	// Spot check one of the messages, the first message should be from the first file because we wrote that first.
	if client.messages[0] != "test1" {
		t.Error("Expected first log message to be 'test1' but was", client.messages[0])
	}

	// Spot check the second message, it should be from the second file.
	if client.messages[1] != "dirt1" {
		t.Error("Expected first log message to be 'test2' but was", client.messages[1])
	}
}

type TestClient struct {
	log      log.Logger
	messages []string
}

func (c *TestClient) Handle(ls model.LabelSet, t time.Time, s string) error {
	c.messages = append(c.messages, s)
	level.Debug(c.log).Log("msg", "received log", "log", s)
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
