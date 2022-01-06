package main

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

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

func handler(ctx context.Context, ev map[string]interface{}) error {
	s3event, err := checkIfS3Event(ev)
	if err == nil {
		return processS3(ctx, s3event)
	}

	cwevent, err := checkIfCWEvent(ev)
	if err == nil {
		return processCW(ctx, cwevent)
	}

	fmt.Println("invalid event: %s", ev)

	return nil
}

func main() {
	lambda.Start(handler)
}
