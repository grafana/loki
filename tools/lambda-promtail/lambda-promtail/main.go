package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
)

const (
	// We use snappy-encoded protobufs over http by default.
	contentType = "application/x-protobuf"

	maxErrMsgLen = 1024
)

var (
	writeAddress       *url.URL
	username, password string
	keepStream         bool
)

func init() {
	addr := os.Getenv("WRITE_ADDRESS")
	if addr == "" {
		panic(errors.New("required environmental variable WRITE_ADDRESS not present, format: https://<hostname>/loki/api/v1/push"))
	}

	var err error
	writeAddress, err = url.Parse(addr)
	if err != nil {
		panic(err)
	}

	fmt.Println("write address: ", writeAddress.String())

	username = os.Getenv("USERNAME")
	password = os.Getenv("PASSWORD")

	// If either username or password is set then both must be.
	if (username != "" && password == "") || (username == "" && password != "") {
		panic("both username and password must be set if either one is set")
	}

	keep := os.Getenv("KEEP_STREAM")
	// Anything other than case-insensitive 'true' is treated as 'false'.
	if strings.EqualFold(keep, "true") {
		keepStream = true
	}
	fmt.Println("keep stream: ", keepStream)
}

func checkEventType(ev map[string]interface{}) (interface{}, error) {
	var s3Event events.S3Event
	var cwEvent events.CloudwatchLogsEvent

	types := [...]interface{}{&s3Event, &cwEvent}

	j, _ := json.Marshal(ev)
	reader := strings.NewReader(string(j))
	d := json.NewDecoder(reader)
	d.DisallowUnknownFields()

	for _, t := range types {
		err := d.Decode(t)

		if err == nil {
			return t, err
		}

		reader.Seek(0, 0)
	}

	return nil, fmt.Errorf("unknown event type!")
}

func handler(ctx context.Context, ev map[string]interface{}) error {
	event, err := checkEventType(ev)
	if err != nil {
		fmt.Println("invalid event: %s", ev)
		return err
	}

	switch event.(type) {
	case *events.S3Event:
		return processS3Event(ctx, event.(*events.S3Event))
	case *events.CloudwatchLogsEvent:
		return processCWEvent(ctx, event.(*events.CloudwatchLogsEvent))
	}

	return err
}

func main() {
	lambda.Start(handler)
}
