package objectstore

import (
	"context"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/go-kit/kit/log"
	"github.com/klauspost/compress/gzip"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v2"

	"github.com/grafana/loki/pkg/promtail/positions"
	"github.com/grafana/loki/pkg/promtail/scrapeconfig"
	"github.com/grafana/loki/pkg/promtail/targets/testutils"
)

var (
	logLines = []string{
		`http 2020-06-05T07:27:53.050517Z app/test/71e8c05ca6c6aed2 172.31.46.45:55956 - -1 -1 -1 503 - 120 353 "GET http://internal-test-1309439803.us-east-1.elb.amazonaws.com:80/test HTTP/1.1" "curl/7.58.0" - - arn:aws:elasticloadbalancing:us-east-1:042533238291:targetgroup/test2/5d656dfb418fc377 "Root=1-5ed9f3f9-38a5d3c63d696c3c9a19a95e" "-" "-" 0 2020-06-05T07:27:53.050000Z "forward" "-" "-" "-" "-"`,
		`http 2020-06-05T07:27:59.128396Z app/test/71e8c05ca6c6aed2 172.31.46.45:55958 - -1 -1 -1 503 - 120 353 "GET http://internal-test-1309439803.us-east-1.elb.amazonaws.com:80/test HTTP/1.1" "curl/7.58.0" - - arn:aws:elasticloadbalancing:us-east-1:042533238291:targetgroup/test2/5d656dfb418fc377 "Root=1-5ed9f3ff-7963647fb1d9da35eda2c603" "-" "-" 0 2020-06-05T07:27:59.128000Z "forward" "-" "-" "-" "-"`,
	}
	testScrapeConfig = scrapeconfig.Config{
		S3Config: &scrapeconfig.S3Targetconfig{
			Labels:     model.LabelSet{"test": "value"},
			Prefix:     "",
			SyncPeriod: 10 * time.Millisecond,
		},
	}
)

func Test_ObjectReader(t *testing.T) {
	w := log.NewSyncWriter(os.Stderr)
	logger := log.NewLogfmtLogger(w)

	testutils.InitRandom()
	dirName := "/tmp/" + testutils.RandName()
	logDirName := dirName + "/logs/"
	logFile := logDirName + "test.log.gz"

	err := os.MkdirAll(logDirName, 0750)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(dirName) }()

	positionsFileName := dirName + "/positions.yml"

	// Set the sync period to a really long value, to guarantee the sync timer never runs, this way we know
	// everything saved was done through channel notifications when target.stop() was called.
	ps, err := positions.New(logger, positions.Config{
		SyncPeriod:    10 * time.Second,
		PositionsFile: positionsFileName,
	})
	require.NoError(t, err)

	// create log file
	err = createLogFile(logFile, logLines)
	require.NoError(t, err)

	object := chunk.StorageObject{
		Key:        logFile,
		ModifiedAt: time.Now(),
	}

	client := &testutils.TestClient{
		Log:      logger,
		Messages: make([]*testutils.Entry, 0),
	}

	objectClient := newMockS3ObjectClient(logDirName)

	objectReader, err := newObjectReader("s3", object, objectClient, logger, model.LabelSet{}, ps, client, 0)
	require.NoError(t, err)

	countdown := 100
	for len(client.Messages) != 2 && countdown > 0 {
		time.Sleep(1 * time.Millisecond)
		countdown--
	}

	objectReader.stop()
	ps.Stop()

	// Assert the number of messages the handler received is correct.
	require.Equal(t, 2, len(client.Messages))
}

func Test_S3ResumeFromKnownPosition(t *testing.T) {
	w := log.NewSyncWriter(os.Stderr)
	logger := log.NewLogfmtLogger(w)

	testutils.InitRandom()
	dirName := "/tmp/" + testutils.RandName()
	logDirName := dirName + "/logs/"
	logFile := logDirName + "test.log.gz"

	err := os.MkdirAll(logDirName, 0750)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(dirName) }()

	positionsFileName := dirName + "/positions.yml"

	// Set the sync period to a really long value, to guarantee the sync timer never runs, this way we know
	// everything saved was done through channel notifications when target.stop() was called.
	ps, err := positions.New(logger, positions.Config{
		SyncPeriod:    10 * time.Second,
		PositionsFile: positionsFileName,
	})
	require.NoError(t, err)

	// create logfile
	err = createLogFile(logFile, logLines)
	require.NoError(t, err)

	client := &testutils.TestClient{
		Log:      logger,
		Messages: make([]*testutils.Entry, 0),
	}

	objectClient := newMockS3ObjectClient(logDirName)

	target, err := NewObjectTarget(logger, client, ps, "test", objectClient, &testScrapeConfig)
	require.NoError(t, err)

	countdown := 100
	for len(client.Messages) != 2 && countdown > 0 {
		time.Sleep(1 * time.Millisecond)
		countdown--
	}

	target.Stop()
	ps.Stop()

	// Assert the number of messages the handler received is correct.
	require.Equal(t, 2, len(client.Messages))

	time.Sleep(1 * time.Second)

	ps2, err := positions.New(logger, positions.Config{
		SyncPeriod:    10 * time.Second,
		PositionsFile: positionsFileName,
	})
	require.NoError(t, err)

	// create new file with more lines
	createLogFile(logDirName+"test.log.gz", []string{
		logLines[0],
		logLines[1],
		"test log line",
	})

	target2, err := NewObjectTarget(logger, client, ps2, "test", objectClient, &testScrapeConfig)
	if err != nil {
		t.Fatal(err)
	}

	countdown = 100
	for len(client.Messages) != 3 && countdown > 0 {
		time.Sleep(1 * time.Millisecond)
		countdown--
	}

	target2.Stop()
	ps2.Stop()

	// Assert the number of messages the handler received is correct.
	require.Equal(t, 3, len(client.Messages))

	// Assert recieved message matches the expected messae
	require.Equal(t, "test log line", client.Messages[2].Log)
}

func Test_PositionFileSync(t *testing.T) {
	w := log.NewSyncWriter(os.Stderr)
	logger := log.NewLogfmtLogger(w)

	testutils.InitRandom()
	dirName := "/tmp/" + testutils.RandName()
	logDirName := dirName + "/logs/"
	logFile := logDirName + "test.log.gz"

	err := os.MkdirAll(logDirName, 0750)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(dirName) }()

	positionsFileName := dirName + "/positions.yml"

	// Set the sync period to a really long value, to guarantee the sync timer never runs, this way we know
	// everything saved was done through channel notifications when target.stop() was called.
	ps, err := positions.New(logger, positions.Config{
		SyncPeriod:    10 * time.Second,
		PositionsFile: positionsFileName,
	})
	require.NoError(t, err)

	// create logfile
	err = createLogFile(logFile, logLines)
	require.NoError(t, err)

	client := &testutils.TestClient{
		Log:      logger,
		Messages: make([]*testutils.Entry, 0),
	}

	objectClient := newMockS3ObjectClient(logDirName)

	target, err := NewObjectTarget(logger, client, ps, "test", objectClient, &testScrapeConfig)
	require.NoError(t, err)

	countdown := 100
	for len(client.Messages) != 2 && countdown > 0 {
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
	val, ok := p.Positions["s3object-"+logFile]
	require.Equal(t, true, ok)

	pos := strings.Split(val, ":")[1]
	require.Equal(t, "796", pos)

	// Assert the number of messages the handler received is correct.
	require.Equal(t, 2, len(client.Messages))
}

func Test_ReadMultipleObjects(t *testing.T) {
	w := log.NewSyncWriter(os.Stderr)
	logger := log.NewLogfmtLogger(w)

	testutils.InitRandom()
	dirName := "/tmp/" + testutils.RandName()
	logDirName := dirName + "/logs/"
	logFile1 := logDirName + "test1.log.gz"
	logFile2 := logDirName + "test2.log.gz"
	logFile3 := logDirName + "test3.log.gz"

	err := os.MkdirAll(logDirName, 0750)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(dirName) }()

	positionsFileName := dirName + "/positions.yml"

	// Set the sync period to a really long value, to guarantee the sync timer never runs, this way we know
	// everything saved was done through channel notifications when target.stop() was called.
	ps, err := positions.New(logger, positions.Config{
		SyncPeriod:    10 * time.Second,
		PositionsFile: positionsFileName,
	})
	require.NoError(t, err)

	// create logfiles
	for _, file := range []string{logFile1, logFile2, logFile3} {
		err = createLogFile(file, logLines)
		require.NoError(t, err)
	}

	client := &testutils.TestClient{
		Log:      logger,
		Messages: make([]*testutils.Entry, 0),
	}

	objectClient := newMockS3ObjectClient(logDirName)

	target, err := NewObjectTarget(logger, client, ps, "test", objectClient, &testScrapeConfig)
	require.NoError(t, err)

	countdown := 100
	for len(client.Messages) != 6 && countdown > 0 {
		time.Sleep(1 * time.Millisecond)
		countdown--
	}

	// Assert the number of messages the handler received is correct.
	require.Equal(t, 6, len(client.Messages))

	time.Sleep(1 * time.Second)
	// add line and update log file
	createLogFile(logFile1, []string{
		logLines[0],
		logLines[1],
		"test log line",
	})

	// remove lines and update log file
	createLogFile(logFile2, []string{
		"test log line 1",
		"test log line 2",
		"test log line 3",
	})

	countdown = 100
	for len(client.Messages) != 10 && countdown > 0 {
		time.Sleep(1 * time.Millisecond)
		countdown--
	}

	target.Stop()
	ps.Stop()

	// Assert the number of messages the handler received is correct.
	require.Equal(t, 10, len(client.Messages))
}

func createLogFile(file string, lines []string) error {
	f, err := os.Create(file)
	if err != nil {
		return err
	}
	writer, err := gzip.NewWriterLevel(f, gzip.DefaultCompression)
	if err != nil {
		return err
	}

	for _, line := range lines {
		writer.Write([]byte(line))
		writer.Write([]byte("\n"))
	}
	writer.Close()
	return nil
}

type mockObjectClient struct {
	logDirName string
}

func newMockS3ObjectClient(logDirName string) chunk.ObjectClient {
	return &mockObjectClient{logDirName: logDirName}
}

func (mockClient *mockObjectClient) GetObject(ctx context.Context, objectKey string) (io.ReadCloser, error) {
	object, err := os.Open(objectKey)
	if err != nil {
		return nil, err
	}
	return object, nil
}

func (mockClient *mockObjectClient) List(ctx context.Context, prefix string) ([]chunk.StorageObject, []chunk.StorageCommonPrefix, error) {
	var objects []chunk.StorageObject
	err := filepath.Walk(mockClient.logDirName, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			objects = append(objects, chunk.StorageObject{
				ModifiedAt: info.ModTime(),
				Key:        mockClient.logDirName + info.Name(),
			})
		}
		return nil
	})

	if err != nil {
		return nil, nil, err
	}

	return objects, nil, nil
}

func (mockClient *mockObjectClient) DeleteObject(ctx context.Context, objectKey string) error {
	return nil
}

func (mockClient *mockObjectClient) PutObject(ctx context.Context, objectKey string, object io.ReadSeeker) error {
	return nil
}

func (mockClient *mockObjectClient) Stop() {}
