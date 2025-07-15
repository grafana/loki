package main

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/aws/aws-lambda-go/events"
	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"
)

type testPromtailClient struct{}

func (t testPromtailClient) sendToPromtail(ctx context.Context, b *batch) error {
	return nil
}

func Test_processEventBridgeEvent(t *testing.T) {
	logger := log.NewNopLogger()
	t.Run("s3 object created event", func(t *testing.T) {
		bs, err := os.ReadFile("../testdata/eventbridge-s3-event.json")
		require.NoError(t, err)

		var ebEvent events.CloudWatchEvent
		require.NoError(t, json.Unmarshal(bs, &ebEvent))

		processor := s3EventProcessor(func(ctx context.Context, ev *events.S3Event, pc Client, log *log.Logger) error {
			require.Len(t, ev.Records, 1)
			require.Equal(t, events.S3EventRecord{
				AWSRegion: "us-east-2",
				S3: events.S3Entity{
					Bucket: events.S3Bucket{
						Name: "bucket",
					},
					Object: events.S3Object{
						Key: "pizza.txt",
					},
				},
			}, ev.Records[0])
			return nil
		})

		err = processEventBridgeEvent(context.Background(), &ebEvent, testPromtailClient{}, &logger, processor)
		require.NoError(t, err)

		t.Run("s3 object created event", func(t *testing.T) {
			var ebEvent = events.CloudWatchEvent{
				Source: "aws.s3",
				// picking a different s3 event type
				// https://docs.aws.amazon.com/AmazonS3/latest/userguide/ev-mapping-troubleshooting.html
				DetailType: "Object Restore Initiated",
			}

			processor := s3EventProcessor(func(ctx context.Context, ev *events.S3Event, pc Client, log *log.Logger) error {
				return nil
			})

			err = processEventBridgeEvent(context.Background(), &ebEvent, testPromtailClient{}, &logger, processor)
			require.Error(t, err, "expected process to fail due to unsupported event type")
		})
	})
}
