package main

import (
	"context"

	"github.com/aws/aws-lambda-go/events"
)

func processSNSEvent(ctx context.Context, evt *events.SNSEvent) error {
	for _, record := range evt.Records {
		event, err := stringToRawEvent(record.SNS.Message)
		if err != nil {
			return err
		}
		err = handler(ctx, event)
		if err != nil {
			return err
		}
	}
	return nil
}
