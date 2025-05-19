package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/aws/aws-lambda-go/events"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
)

// Notification represents the top-level SNS message structure
type Notification struct {
	Type             string `json:"Type"`
	MessageID        string `json:"MessageId"`
	TopicArn         string `json:"TopicArn"`
	Message          string `json:"Message"`
	Timestamp        string `json:"Timestamp"`
	SignatureVersion string `json:"SignatureVersion"`
	Signature        string `json:"Signature"`
	SigningCertURL   string `json:"SigningCertURL"`
	UnsubscribeURL   string `json:"UnsubscribeURL"`
}

type S3Record struct {
}

func parseSQSEvent(ctx context.Context, b *batch, ev *events.SQSEvent, log *log.Logger, handler func(ctx context.Context, ev map[string]interface{}) error) error {
	if ev == nil {
		return nil
	}
	for _, record := range ev.Records {
		var notification Notification
		var sesNotification SESNotification
		err := json.Unmarshal([]byte(record.Body), &notification)
		if err == nil {
			sesNotErr := json.Unmarshal([]byte(notification.Message), &sesNotification)
			if sesNotErr == nil {
				level.Info(*log).Log("msg", fmt.Sprintf("Its an sesMessage FROM an SQS message"))
				sesErr := parseSESEvent(ctx, b, log, &sesNotification)
				if sesErr != nil {
					return sesErr
				}
				continue // We want to stop processing the record as its a sesNotification message.
			}
		}

		//Check if its NOT a notification event
		processSQSS3Event(ctx, ev, handler)
		level.Info(*log).Log("msg", fmt.Sprintf("Currently only SQS messages from SES are supported. The current message is NOT an SES notification. Moving on."))
	}
	return nil
}

func processSQSEvent(ctx context.Context, ev *events.SQSEvent, pClient Client, log *log.Logger, handler func(ctx context.Context, ev map[string]interface{}) error) error {
	batch, _ := newBatch(ctx, pClient)

	err := parseSQSEvent(ctx, batch, ev, log, handler)
	if err != nil {
		return err
	}

	err = pClient.sendToPromtail(ctx, batch)
	if err != nil {
		return err
	}
	return nil
}
