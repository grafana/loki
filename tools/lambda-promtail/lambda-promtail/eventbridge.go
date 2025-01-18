package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/aws/aws-lambda-go/events"
	"github.com/go-kit/log"
)

// S3Detail encodes the message structure in EventBridge s3 notifications.
// https://docs.aws.amazon.com/AmazonS3/latest/userguide/ev-events.html
type S3Detail struct {
	Version string `json:"version"`
	Bucket  struct {
		Name string `json:"name"`
	} `json:"bucket"`
	Object S3ObjectDetail `json:"object"`
}

type S3ObjectDetail struct {
	Key       string `json:"key"`
	Size      int    `json:"size"`
	ETag      string `json:"etag"`
	VersionID string `json:"version-id"`
	Sequencer string `json:"sequencer"`
}

type s3EventProcessor func(ctx context.Context, ev *events.S3Event, pc Client, log *log.Logger) error

func processEventBridgeEvent(ctx context.Context, ev *events.CloudWatchEvent, pc Client, log *log.Logger, process s3EventProcessor) error {
	// lambda-promtail should only be used with S3 object creation events, since those indicate that a new file has been
	// added to bucket, and need to be fetched and parsed accordingly.
	if !(ev.Source == "aws.s3" && ev.DetailType == "Object Created") {
		return fmt.Errorf("event bridge event type not supported")
	}

	var eventDetail S3Detail
	if err := json.Unmarshal(ev.Detail, &eventDetail); err != nil {
		return err
	}

	// TODO(thepalbi): how to fill bucket owner?
	var s3Event = events.S3Event{
		Records: []events.S3EventRecord{
			{
				AWSRegion: ev.Region,
				S3: events.S3Entity{
					Bucket: events.S3Bucket{
						Name: eventDetail.Bucket.Name,
					},
					Object: events.S3Object{
						Key: eventDetail.Object.Key,
					},
				},
			},
		},
	}

	return process(ctx, &s3Event, pc, log)
}
