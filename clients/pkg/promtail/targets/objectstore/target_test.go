package objectstore

import (
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"sort"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/sqs"
	"github.com/aws/aws-sdk-go/service/sqs/sqsiface"
	"github.com/go-kit/log"
	"github.com/grafana/loki/clients/pkg/promtail/client/fake"
	"github.com/grafana/loki/clients/pkg/promtail/positions"
	"github.com/grafana/loki/clients/pkg/promtail/scrapeconfig"
	"github.com/grafana/loki/clients/pkg/promtail/targets/testutils"
	"github.com/grafana/loki/pkg/storage/chunk/local"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
)

func TestObjectS3Target_Run(t *testing.T) {
	var (
		w       = log.NewSyncWriter(os.Stderr)
		metrics = NewMetrics(prometheus.NewRegistry())
		logger  = log.NewLogfmtLogger(w)
		client  = fake.New(func() {})
		cfg     = &scrapeconfig.S3TargetConfig{
			BucketName:      "foo",
			AccessKeyID:     "test",
			SecretAccessKey: "bar",
			SQSQueue:        "test",
			Timeout:         int64(20),
			Labels:          model.LabelSet{"job": "s3_object"},
			Region:          "test-region",
			ResetCursor:     false,
		}
		storename = "s3"
	)
	labels := cfg.Labels.Merge(model.LabelSet{
		model.LabelName("s3_bucket"): model.LabelValue(cfg.BucketName),
	})

	ps, err := positions.New(logger, positions.Config{
		SyncPeriod:    10 * time.Second,
		PositionsFile: t.TempDir() + "/positions.yml",
	})
	require.NoError(t, err)

	tempDir, err := ioutil.TempDir("", "temp-object-store")
	require.NoError(t, err)
	defer func() {
		require.NoError(t, os.RemoveAll(tempDir))
	}()

	objectClient, err := local.NewFSObjectClient(local.FSConfig{Directory: tempDir})
	require.NoError(t, err)

	testutils.InitRandom()
	dirName := "/tmp/promtail_test_" + testutils.RandName()
	folder := "folder1"
	file1 := "file1"
	file2 := "file2"

	defer func() { _ = os.RemoveAll(dirName) }()
	err = os.MkdirAll(dirName, 0750)
	if err != nil {
		t.Error(err)
		return
	}

	testDir := dirName + "/" + folder
	err = os.MkdirAll(testDir, 0750)
	if err != nil {
		t.Error(err)
	}

	linesFile1 := []string{
		"Test log 1",
		"Test log 2",
		"Test log 3",
	}
	err = createLogLines(path.Join(dirName, folder, file1), linesFile1)
	if err != nil {
		t.Error(err)
	}
	linesFile2 := []string{
		"Test log 4",
		"Test log 5",
		"Test log 6",
	}
	err = createLogLines(path.Join(dirName, folder, file2), linesFile2)
	if err != nil {
		t.Error(err)
	}

	files := []string{file1, file2}

	for _, filename := range files {
		f, err := os.ReadFile(path.Join(dirName, folder, filename))
		if err != nil {
			t.Error(err)
		}
		err = objectClient.PutObject(context.Background(), path.Join(folder, filename), bytes.NewReader(f))
		require.NoError(t, err)
	}

	record1 := `
	{
		"Records": [
			{
				"eventTime": "2021-12-27T10:19:05.521Z",
				"s3": {
					"object": {
						"key": "folder1/file1"
					}
				}
			}
		]
	}`

	record2 := `
	{
		"Records": [
			{
				"eventTime": "2021-12-27T10:19:06.521Z",
				"s3": {
					"object": {
						"key": "folder1/file2"
					}
				}
			}
		]
	}
	`
	mockURL := "mockURL"
	messages := map[string][]*sqs.Message{
		mockURL: {
			{Body: aws.String(record1)},
			{Body: aws.String(record2)},
		},
	}

	sqsClient := &s3Client{
		svc:      &mockSQS{messages: messages},
		queueURL: &mockURL,
	}

	ta, err := NewTarget(metrics, logger, client, ps, objectClient, sqsClient, labels, cfg.Timeout, cfg.ResetCursor, storename)
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		return len(client.Received()) == 6
	}, 5*time.Second, 100*time.Millisecond)

	received := client.Received()
	sort.Slice(received, func(i, j int) bool {
		return received[i].Timestamp.Before(received[j].Timestamp)
	})

	for _, e := range received {
		require.Equal(t, model.LabelValue("s3_object"), e.Labels["job"])
		require.Equal(t, model.LabelValue(cfg.BucketName), e.Labels["s3_bucket"])
	}

	ta.Stop()
	ps.Stop()
}

func createLogLines(filename string, lines []string) error {
	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer f.Close()
	for _, line := range lines {
		_, err := f.WriteString(fmt.Sprintf("%s\n", line))
		if err != nil {
			return err
		}
	}

	return nil
}

type mockSQS struct {
	sqsiface.SQSAPI
	messages map[string][]*sqs.Message
}

func (m *mockSQS) ReceiveMessage(in *sqs.ReceiveMessageInput) (*sqs.ReceiveMessageOutput, error) {
	if len(m.messages[*in.QueueUrl]) == 0 {
		return &sqs.ReceiveMessageOutput{}, nil
	}

	response := m.messages[*in.QueueUrl][0:1]
	m.messages[*in.QueueUrl] = m.messages[*in.QueueUrl][1:]
	return &sqs.ReceiveMessageOutput{
		Messages: response,
	}, nil
}

func (m *mockSQS) DeleteMessage(in *sqs.DeleteMessageInput) (*sqs.DeleteMessageOutput, error) {
	return &sqs.DeleteMessageOutput{}, nil
}
