package file

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-kit/log"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/discovery"
	"github.com/prometheus/prometheus/discovery/targetgroup"

	"github.com/grafana/loki/v3/clients/pkg/promtail/api"
	"github.com/grafana/loki/v3/clients/pkg/promtail/client/fake"
	"github.com/grafana/loki/v3/clients/pkg/promtail/positions"
	"github.com/grafana/loki/v3/clients/pkg/promtail/scrapeconfig"
)

func newTestLogDirectories(t *testing.T) string {
	tmpDir := t.TempDir()
	logFileDir := filepath.Join(tmpDir, "logs")
	err := os.MkdirAll(logFileDir, 0750)
	assert.NoError(t, err)
	return logFileDir
}

func newTestPositions(logger log.Logger, filePath string) (positions.Positions, error) {
	// Set the sync period to a really long value, to guarantee the sync timer never runs, this way we know
	// everything saved was done through channel notifications when target.stop() was called.
	pos, err := positions.New(logger, positions.Config{
		SyncPeriod:    60 * time.Second,
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
		SyncPeriod: 1 * time.Second,
	}

	metrics := NewMetrics(nil)
	ftm, err := NewFileTargetManager(metrics, logger, positions, client, []scrapeconfig.Config{sc}, tc, DefaultWatchConig)
	if err != nil {
		return nil, err
	}
	return ftm, nil
}

func TestLongPositionsSyncDelayStillSavesCorrectPosition(t *testing.T) {
	w := log.NewSyncWriter(os.Stderr)
	logger := log.NewLogfmtLogger(w)
	logDirName := newTestLogDirectories(t)

	logFile := filepath.Join(logDirName, "test.log")
	positionsFileName := filepath.Join(logDirName, "positions.yml")
	ps, err := newTestPositions(logger, positionsFileName)
	if err != nil {
		t.Fatal(err)
	}

	client := fake.New(func() {})
	defer client.Stop()

	ftm, err := newTestFileTargetManager(logger, client, ps, logDirName+"/*")
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

	assert.Eventually(t, func() bool {
		return len(client.Received()) == 10
	}, time.Second*10, time.Millisecond*1)

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
	logDirName := newTestLogDirectories(t)

	logFile := filepath.Join(logDirName, "test.log")
	positionsFileName := filepath.Join(logDirName, "positions.yml")
	ps, err := newTestPositions(logger, positionsFileName)
	if err != nil {
		t.Fatal(err)
	}

	client := fake.New(func() {})
	defer client.Stop()

	ftm, err := newTestFileTargetManager(logger, client, ps, logDirName+"/*")
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

	assert.Eventually(t, func() bool {
		return len(client.Received()) == 10
	}, time.Second*10, time.Millisecond*1)

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
	logDirName := newTestLogDirectories(t)

	logFile := filepath.Join(logDirName, "test.log")
	positionsFileName := filepath.Join(logDirName, "positions.yml")
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
	assert.Eventually(t, func() bool {
		return len(client.Received()) == 10
	}, time.Second*10, time.Millisecond*1)

	// Rename the log file to something not in the pattern, then create a new file with the same name.
	err = os.Rename(logFile, filepath.Join(logDirName, "test.log.1"))
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
	assert.Eventually(t, func() bool {
		return len(client.Received()) == 20
	}, time.Second*10, time.Millisecond*1)

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
	logDirName := newTestLogDirectories(t)

	logFile := filepath.Join(logDirName, "test.log")
	positionsFileName := filepath.Join(logDirName, "positions.yml")
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
	assert.Eventually(t, func() bool {
		return len(client.Received()) == 10
	}, time.Second*10, time.Millisecond*1)

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
	assert.Eventually(t, func() bool {
		return len(client.Received()) == 20
	}, time.Second*10, time.Millisecond*1)

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
	logDirName := newTestLogDirectories(t)

	logFile1 := filepath.Join(logDirName, "test.log")
	logFile2 := filepath.Join(logDirName, "dirt.log")
	positionsFileName := filepath.Join(logDirName, "positions.yml")
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
	assert.Eventually(t, func() bool {
		return len(client.Received()) == 20
	}, time.Second*10, time.Millisecond*1)

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

func TestDeadlockTargetManager(t *testing.T) {
	client := fake.New(func() {})
	defer client.Stop()

	targetEventHandler := make(chan fileTargetEvent)
	defer func() {
		close(targetEventHandler)
	}()

	syncer := &targetSyncer{
		metrics:           NewMetrics(nil),
		log:               log.NewLogfmtLogger(log.NewSyncWriter(os.Stderr)),
		positions:         nil,
		entryHandler:      client,
		hostname:          "localhost",
		fileEventWatchers: make(map[string]chan fsnotify.Event),
		targets:           make(map[string]*FileTarget),
		targetConfig: &Config{
			SyncPeriod: time.Hour,
		},
	}

	syncer.sync([]*targetgroup.Group{
		{
			Targets: []model.LabelSet{
				{
					hostLabel: "localhost",
					pathLabel: "baz",
					"job":     "bar",
				},
			},
		},
	}, targetEventHandler)

	require.Equal(t, len(syncer.targets), 1)
	require.Equal(t, len(syncer.fileEventWatchers), 1)

	syncer.sync([]*targetgroup.Group{
		{
			Targets: []model.LabelSet{
				{},
			},
		},
	}, targetEventHandler)

	syncer.sendFileCreateEvent(fsnotify.Event{Name: "baz"})

	require.Equal(t, len(syncer.targets), 0)
	require.Equal(t, len(syncer.fileEventWatchers), 0)
}

func TestDeadlockStartWatchingDuringSync(t *testing.T) {
	w := log.NewSyncWriter(os.Stderr)
	logger := log.NewLogfmtLogger(w)
	oldLogDir := newTestLogDirectories(t)
	newLogDir := newTestLogDirectories(t)

	positionsFileName := filepath.Join(oldLogDir, "positions.yml")
	ps, err := newTestPositions(logger, positionsFileName)
	assert.NoError(t, err)

	client := fake.New(func() {})
	defer client.Stop()

	ftm, err := newTestFileTargetManager(logger, client, ps, oldLogDir+"/*")
	assert.NoError(t, err)

	done := make(chan struct{})
	go func() {
		for i := 0; i < 10; i++ {
			dir := filepath.Join(newLogDir, fmt.Sprintf("%d", i))
			err := os.MkdirAll(dir, 0750)
			assert.NoError(t, err)
			time.Sleep(1 * time.Millisecond)
			for j := 0; j < 100; j++ {
				logFile := filepath.Join(dir, fmt.Sprintf("test%d.log", j))
				f, err := os.Create(logFile)
				assert.NoError(t, err)
				_, err = f.WriteString("just a log line\n")
				assert.NoError(t, err)
				err = f.Close()
				assert.NoError(t, err)
				time.Sleep(1 * time.Millisecond)
			}
		}
		close(done)
	}()

	tg := targetgroup.Group{
		Targets: []model.LabelSet{{
			"localhost": "",
		}},
		Labels: model.LabelSet{
			"job":      "varlogs",
			"match":    "true",
			"__path__": model.LabelValue(newLogDir + "/**/*.log"),
		},
		Source: "",
	}
	for i := 0; i < 10; i++ {
		ftm.syncers[""].sync([]*targetgroup.Group{&tg}, ftm.targetEventHandler)
	}
	<-done

	ftm.Stop()
	ps.Stop()
}

func TestLabelSetUpdate(t *testing.T) {
	client := fake.New(func() {})
	defer client.Stop()

	targetEventHandler := make(chan fileTargetEvent)
	defer func() {
		close(targetEventHandler)
	}()

	syncer := &targetSyncer{
		metrics:           NewMetrics(nil),
		log:               log.NewLogfmtLogger(log.NewSyncWriter(os.Stderr)),
		positions:         nil,
		entryHandler:      client,
		hostname:          "localhost",
		fileEventWatchers: make(map[string]chan fsnotify.Event),
		targets:           make(map[string]*FileTarget),
		targetConfig: &Config{
			SyncPeriod: time.Hour,
		},
	}

	var target = model.LabelSet{
		hostLabel: "localhost",
		pathLabel: "baz",
		"job":     "foo",
	}

	var target2 = model.LabelSet{
		hostLabel: "localhost",
		pathLabel: "baz",
		"job":     "foo2",
	}

	// two separate targets with same path
	syncer.sync([]*targetgroup.Group{
		{
			Targets: []model.LabelSet{target, target2},
		},
	}, targetEventHandler)
	syncer.sendFileCreateEvent(fsnotify.Event{Name: "baz"})

	require.Equal(t, 2, len(syncer.targets))
	require.Equal(t, 1, len(syncer.fileEventWatchers))

	// remove second target and modify the first
	target["job"] = "bar"
	syncer.sync([]*targetgroup.Group{
		{
			Targets: []model.LabelSet{target},
		},
	}, targetEventHandler)

	require.Equal(t, 1, len(syncer.targets))
	require.Equal(t, 1, len(syncer.fileEventWatchers))

	// cleanup all targets
	syncer.sync([]*targetgroup.Group{
		{
			Targets: []model.LabelSet{},
		},
	}, targetEventHandler)
	require.Equal(t, 0, len(syncer.targets))
	require.Equal(t, 0, len(syncer.fileEventWatchers))

}
