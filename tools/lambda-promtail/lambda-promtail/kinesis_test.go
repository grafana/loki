package main

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/aws/aws-lambda-go/events"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/logproto"
)

type MockBatch struct {
	streams map[string]*logproto.Stream
	size    int
}

func (b *MockBatch) add(ctx context.Context, e entry) error {
	b.streams[e.labels.String()] = &logproto.Stream{
		Labels: e.labels.String(),
	}
	return nil
}

func (b *MockBatch) flushBatch(ctx context.Context) error {
	return nil
}
func (b *MockBatch) encode() ([]byte, int, error) {
	return nil, 0, nil
}
func (b *MockBatch) createPushRequest() (*logproto.PushRequest, int) {
	return nil, 0
}

func ReadJSONFromFile(t *testing.T, inputFile string) []byte {
	inputJSON, err := os.ReadFile(inputFile)
	if err != nil {
		t.Errorf("could not open test file. details: %v", err)
	}

	return inputJSON
}

func TestLambdaPromtail_KinesisParseEvents(t *testing.T) {
	inputJson, err := os.ReadFile("../testdata/kinesis-event.json")

	if err != nil {
		t.Errorf("could not open test file. details: %v", err)
	}

	var testEvent events.KinesisEvent
	if err := json.Unmarshal(inputJson, &testEvent); err != nil {
		t.Errorf("could not unmarshal event. details: %v", err)
	}

	ctx := context.TODO()
	b := &MockBatch{
		streams: map[string]*logproto.Stream{},
	}

	err = parseKinesisEvent(ctx, b, &testEvent)
	require.Nil(t, err)

	labels_str := "{__aws_kinesis_event_source_arn=\"arn:aws:kinesis:us-east-1:123456789012:stream/simple-stream\", __aws_log_type=\"kinesis\"}"
	require.Contains(t, b.streams, labels_str)
}
