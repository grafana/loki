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

func ReadJSONFromFile(t *testing.T, inputFile string) []byte {
	inputJSON, err := os.ReadFile(inputFile)
	if err != nil {
		t.Errorf("could not open test file. details: %v", err)
	}

	return inputJSON
}

func TestLambdaPromtail_KinesisParseEvents(t *testing.T) {
	inputJson, err := os.ReadFile("../testdata/kinesis-event.json")
	mockBatch := &batch{
		streams: map[string]*logproto.Stream{},
	}

	if err != nil {
		t.Errorf("could not open test file. details: %v", err)
	}

	var testEvent events.KinesisEvent
	if err := json.Unmarshal(inputJson, &testEvent); err != nil {
		t.Errorf("could not unmarshal event. details: %v", err)
	}

	ctx := context.TODO()

	err = parseKinesisEvent(ctx, mockBatch, &testEvent)
	require.Nil(t, err)

	labelsStr := "{__aws_kinesis_event_source_arn=\"arn:aws:kinesis:us-east-1:123456789012:stream/simple-stream\", __aws_log_type=\"kinesis\"}"
	require.Contains(t, mockBatch.streams, labelsStr)
}
