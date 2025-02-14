package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/aws/aws-lambda-go/events"
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

func parseSQSEvent(ctx context.Context, b *batch, ev *events.SQSEvent) error {
	if ev == nil {
		return nil
	}
	for _, record := range ev.Records {
		var notification Notification
		var sesNotification SESNotification
		// fmt.Println("HERE--------------")
		// fmt.Println(record.Body)
		err := json.Unmarshal([]byte(record.Body), &notification)
		if err == nil {
			fmt.Println("Its an sqsMessage")
			// fmt.Println(notification)
			// fmt.Println(notification.Type)

			// Now lets see if its an sesNotification

			// fmt.Println(notification.Message)
			// fmt.Println("HERE--------------22")

			sesNotErr := json.Unmarshal([]byte(notification.Message), &sesNotification)
			if sesNotErr == nil {
				fmt.Println("Its an sesMessage FROM an SQS message")
				// fmt.Println(sesNotification)
				// fmt.Println(sesNotification.NotificationType)
				// fmt.Println("Now calling the ses module")
				sesErr := parseSESEvent(ctx, b, &sesNotification)
				if sesErr != nil {
					// fmt.Println("Error processing SES Event")
					return sesErr
				}
				continue // We want to stop processing the record as its a sesNotification message.
			}
		}

		//Check if its NOT a notification event
		processSQSS3Event(ctx, ev, handler)
		fmt.Println("Currently only SQS messages from SES are supported. The current message is NOT an SES notification. Moving on.")
	}
	return nil
}

func processSQSEvent(ctx context.Context, ev *events.SQSEvent, pClient Client) error {
	batch, _ := newBatch(ctx, pClient)

	err := parseSQSEvent(ctx, batch, ev)
	if err != nil {
		return err
	}

	err = pClient.sendToPromtail(ctx, batch)
	if err != nil {
		return err
	}
	return nil
}
