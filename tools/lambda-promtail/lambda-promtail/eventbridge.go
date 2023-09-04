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

func processEventBridgeEvent(ctx context.Context, ev *events.CloudWatchEvent, pc Client, log *log.Logger) error {
	if !(ev.Source == "aws.s3" && ev.DetailType == "Object Created") {
		return fmt.Errorf("event bridge event type not supported")
	}

	var s3Detail S3Detail
	if err := json.Unmarshal(ev.Detail, &s3Detail); err != nil {
		return err
	}

	// TODO(thepalbi): how to fill bucket owner?
	infos := []S3EventInfo{{
		ObjectKey:    s3Detail.Object.Key,
		BucketName:   s3Detail.Bucket.Name,
		BucketRegion: ev.Region,
	}}

	return doProcessS3Event(ctx, infos, pc, log)
}
