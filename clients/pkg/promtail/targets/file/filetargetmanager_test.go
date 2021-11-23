package file

import (
	"os"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/discovery"
	"github.com/prometheus/prometheus/discovery/targetgroup"

	"github.com/grafana/loki/clients/pkg/promtail/api"
	"github.com/grafana/loki/clients/pkg/promtail/client/fake"
	"github.com/grafana/loki/clients/pkg/promtail/positions"
	"github.com/grafana/loki/clients/pkg/promtail/scrapeconfig"
	"github.com/grafana/loki/clients/pkg/promtail/targets/testutils"
)

func newTestLogDirectories() (string, func(), error) {
	testutils.InitRandom()
	dirName := "/tmp/" + testutils.RandName()
	logFileDir := dirName + "/logdir"

	err := os.MkdirAll(dirName, 0750)
	if err != nil {
		return "", nil, err
	}
	err = os.MkdirAll(logFileDir, 0750)
	if err != nil {
		return "", nil, err
	}

	return logFileDir, func() {
		_ = os.RemoveAll(dirName)
	}, nil
}

func newTestPositions(logger log.Logger, filePath string) (positions.Positions, error) {
	// Set the sync period to a really long value, to guarantee the sync timer never runs, this way we know
	// everything saved was done through channel notifications when target.stop() was called.
	pos, err := positions.New(logger, positions.Config{
		SyncPeriod:    10 * time.Second,
		PositionsFile: filePath,
	})
	if err != nil {
		return nil, err
	}
	return pos, nil
}

func newTestFileTargetManager(logger log.Logger, client api.EntryHandler, positions positions.Positions, observePath string) (*FileTargetManager, error) {
	targetGroup := targetgroup.Group{
		Targets: []model.LabelSet{{
			"localhost": "",
		}},
		Labels: model.LabelSet{
			"job":      "varlogs",
			"match":    "true",
			"__path__": model.LabelValue(observePath),
		},
		Source: "",
	}
	sc := scrapeconfig.Config{
		JobName:        "",
		PipelineStages: nil,
		RelabelConfigs: nil,
		ServiceDiscoveryConfig: scrapeconfig.ServiceDiscoveryConfig{
			StaticConfigs: discovery.StaticConfig{
				&targetGroup,
			},
		},
	}
	tc := &Config{
		SyncPeriod: 10 * time.Second,
	}

	metrics := NewMetrics(nil)
	ftm, err := NewFileTargetManager(metrics, logger, positions, client, []scrapeconfig.Config{sc}, tc)
	if err != nil {
		return nil, err
	}
	return ftm, nil
}

func TestLongPositionsSyncDelayStillSavesCorrectPosition(t *testing.T) {
	w := log.NewSyncWriter(os.Stderr)
	logger := log.NewLogfmtLogger(w)
	logDirName, cleanup, err := newTestLogDirectories()
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	logFile := logDirName + "test.log"
	positionsFileName := logDirName + "/positions.yml"
	ps, err := newTestPositions(logger, positionsFileName)
	if err != nil {
		t.Fatal(err)
	}

	client := fake.New(func() {})
	defer client.Stop()

	ftm, err := newTestFileTargetManager(logger, client, ps, logDirName+"*")
	if err != nil {
		t.Fatal(err)
	}

	f, err := os.Create(logFile)
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
	for len(client.Received()) != 10 && countdown > 0 {
		time.Sleep(1 * time.Millisecond)
		countdown--
	}

	ftm.Stop()
	ps.Stop()

	// Assert the position value is in the correct spot.
	val, err := ps.Get(logFile)
	if err != nil {
		t.Errorf("Positions file did not contain any data for our test log file, err: %s", err.Error())
	}
	if val != 50 {
		t.Error("Incorrect position found, expected 50, found", val)
	}

	// Assert the number of messages the handler received is correct.
	if len(client.Received()) != 10 {
		t.Error("Handler did not receive the correct number of messages, expected 10 received", len(client.Received()))
	}

	// Spot check one of the messages.
	if client.Received()[0].Line != "test" {
		t.Error("Expected first log message to be 'test' but was", client.Received()[0])
	}
}

func TestWatchEntireDirectory(t *testing.T) {
	w := log.NewSyncWriter(os.Stderr)
	logger := log.NewLogfmtLogger(w)
	logDirName, cleanup, err := newTestLogDirectories()
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	logFile := logDirName + "test.log"
	positionsFileName := logDirName + "/positions.yml"
	ps, err := newTestPositions(logger, positionsFileName)
	if err != nil {
		t.Fatal(err)
	}

	client := fake.New(func() {})
	defer client.Stop()

	ftm, err := newTestFileTargetManager(logger, client, ps, logDirName+"*")
	if err != nil {
		t.Fatal(err)
	}

	f, err := os.Create(logFile)
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
	for len(client.Received()) != 10 && countdown > 0 {
		time.Sleep(1 * time.Millisecond)
		countdown--
	}

	ftm.Stop()
	ps.Stop()

	// Assert the position value is in the correct spot.
	val, err := ps.Get(logFile)
	if err != nil {
		t.Errorf("Positions file did not contain any data for our test log file, err: %s", err.Error())
	}
	if val != 50 {
		t.Error("Incorrect position found, expected 50, found", val)
	}

	// Assert the number of messages the handler received is correct.
	if len(client.Received()) != 10 {
		t.Error("Handler did not receive the correct number of messages, expected 10 received", len(client.Received()))
	}

	// Spot check one of the messages.
	if client.Received()[0].Line != "test" {
		t.Error("Expected first log message to be 'test' but was", client.Received()[0].Line)
	}
}

func TestFileRolls(t *testing.T) {
	w := log.NewSyncWriter(os.Stderr)
	logger := log.NewLogfmtLogger(w)
	logDirName, cleanup, err := newTestLogDirectories()
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	logFile := logDirName + "/test.log"
	positionsFileName := logDirName + "/positions.yml"
	ps, err := newTestPositions(logger, positionsFileName)
	if err != nil {
		t.Fatal(err)
	}

	client := fake.New(func() {})
	defer client.Stop()

	ftm, err := newTestFileTargetManager(logger, client, ps, logDirName+"/*.log")
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(logFile)
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
	for len(client.Received()) != 10 && countdown > 0 {
		time.Sleep(1 * time.Millisecond)
		countdown--
	}

	// Rename the log file to something not in the pattern, then create a new file with the same name.
	err = os.Rename(logFile, logDirName+"/test.log.1")
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
	for len(client.Received()) != 20 && countdown > 0 {
		time.Sleep(1 * time.Millisecond)
		countdown--
	}

	ftm.Stop()
	ps.Stop()

	if len(client.Received()) != 20 {
		t.Error("Handler did not receive the correct number of messages, expected 20 received", len(client.Received()))
	}

	// Spot check one of the messages.
	if client.Received()[0].Line != "test1" {
		t.Error("Expected first log message to be 'test1' but was", client.Received()[0].Line)
	}

	// Spot check the first message from the second file.
	if client.Received()[10].Line != "test2" {
		t.Error("Expected first log message to be 'test2' but was", client.Received()[10].Line)
	}
}

func TestResumesWhereLeftOff(t *testing.T) {
	w := log.NewSyncWriter(os.Stderr)
	logger := log.NewLogfmtLogger(w)
	logDirName, cleanup, err := newTestLogDirectories()
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	logFile := logDirName + "/test.log"
	positionsFileName := logDirName + "/positions.yml"
	ps, err := newTestPositions(logger, positionsFileName)
	if err != nil {
		t.Fatal(err)
	}

	client := fake.New(func() {})
	defer client.Stop()

	ftm, err := newTestFileTargetManager(logger, client, ps, logDirName+"/*.log")
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(logFile)
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
	for len(client.Received()) != 10 && countdown > 0 {
		time.Sleep(1 * time.Millisecond)
		countdown--
	}

	ftm.Stop()
	ps.Stop()

	// Create another positions (so that it loads from the previously saved positions file).
	ps2, err := newTestPositions(logger, positionsFileName)
	if err != nil {
		t.Fatal(err)
	}

	// Create a new target manager, keep the same client so we can track what was sent through the handler.
	ftm2, err := newTestFileTargetManager(logger, client, ps2, logDirName+"/*.log")
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
	for len(client.Received()) != 20 && countdown > 0 {
		time.Sleep(1 * time.Millisecond)
		countdown--
	}

	ftm2.Stop()
	ps2.Stop()

	if len(client.Received()) != 20 {
		t.Error("Handler did not receive the correct number of messages, expected 20 received", len(client.Received()))
	}

	// Spot check one of the messages.
	if client.Received()[0].Line != "test1" {
		t.Error("Expected first log message to be 'test1' but was", client.Received()[0])
	}

	// Spot check the first message from the second file.
	if client.Received()[10].Line != "test2" {
		t.Error("Expected first log message to be 'test2' but was", client.Received()[10])
	}
}

func TestGlobWithMultipleFiles(t *testing.T) {
	w := log.NewSyncWriter(os.Stderr)
	logger := log.NewLogfmtLogger(w)
	logDirName, cleanup, err := newTestLogDirectories()
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	logFile1 := logDirName + "/test.log"
	logFile2 := logDirName + "/dirt.log"
	positionsFileName := logDirName + "/positions.yml"
	ps, err := newTestPositions(logger, positionsFileName)
	if err != nil {
		t.Fatal(err)
	}

	client := fake.New(func() {})
	defer client.Stop()

	ftm, err := newTestFileTargetManager(logger, client, ps, logDirName+"/*.log")
	if err != nil {
		t.Fatal(err)
	}

	f1, err := os.Create(logFile1)
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
	for len(client.Received()) != 20 && countdown > 0 {
		time.Sleep(1 * time.Millisecond)
		countdown--
	}

	ftm.Stop()
	ps.Stop()

	// Assert the position value is in the correct spot.
	val, err := ps.Get(logFile1)
	if err != nil {
		t.Errorf("Positions file did not contain any data for our test log file, err: %s", err.Error())
	}
	if val != 60 {
		t.Error("Incorrect position found for file 1, expected 60, found", val)
	}
	val, err = ps.Get(logFile2)
	if err != nil {
		t.Errorf("Positions file did not contain any data for our test log file, err: %s", err.Error())
	}
	if val != 60 {
		t.Error("Incorrect position found for file 2, expected 60, found", val)
	}

	// Assert the number of messages the handler received is correct.
	if len(client.Received()) != 20 {
		t.Error("Handler did not receive the correct number of messages, expected 20 received", len(client.Received()))
	}
}
