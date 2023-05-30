package main

import (
	"context"
	"encoding/json"

	"github.com/aws/aws-lambda-go/events"
)

func processSQSEvent(ctx context.Context, evt *events.SQSEvent) error {
	for _, record := range evt.Records {
		// retrieve nested
		event, err := stringToRawEvent(record.Body)
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

func stringToRawEvent(body string) (map[string]interface{}, error) {
	result := make(map[string]interface{})
	err := json.Unmarshal([]byte(body), &result)
	if err != nil {
		return nil, err
	}
	return result, nil
}
