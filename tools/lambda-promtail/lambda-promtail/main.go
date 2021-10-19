package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/gogo/protobuf/proto"
	"github.com/golang/snappy"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/pkg/logproto"
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
		panic(errors.New("required environmental variable WRITE_ADDRESS not present"))
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

func handler(ctx context.Context, ev events.CloudwatchLogsEvent) error {
	data, err := ev.AWSLogs.Parse()
	if err != nil {
		fmt.Println("error parsing log event: ", err)
		return err
	}
	labels := model.LabelSet{
		model.LabelName("__aws_cloudwatch_log_group"): model.LabelValue(data.LogGroup),
		model.LabelName("__aws_cloudwatch_owner"):     model.LabelValue(data.Owner),
	}
	if keepStream {
		labels[model.LabelName("__aws_cloudwatch_log_stream")] = model.LabelValue(data.LogStream)
	}

	stream := logproto.Stream{
		Labels:  labels.String(),
		Entries: make([]logproto.Entry, 0, len(data.LogEvents)),
	}

	for _, entry := range data.LogEvents {
		stream.Entries = append(stream.Entries, logproto.Entry{
			Line: entry.Message,
			// It's best practice to ignore timestamps from cloudwatch as promtail is responsible for adding those.
			Timestamp: util.TimeFromMillis(entry.Timestamp),
		})
	}

	buf, err := proto.Marshal(&logproto.PushRequest{
		Streams: []logproto.Stream{stream},
	})
	if err != nil {
		return err
	}

	// Push to promtail
	buf = snappy.Encode(nil, buf)
	req, err := http.NewRequest("POST", writeAddress.String(), bytes.NewReader(buf))
	if err != nil {
		fmt.Println("error: ", err)
		return err
	}
	req.Header.Set("Content-Type", contentType)

	// If either is not empty both should be (see initS), but just to be safe.
	if username != "" && password != "" {
		fmt.Println("adding basic auth to request")
		req.SetBasicAuth(username, password)
	}

	resp, err := http.DefaultClient.Do(req.WithContext(ctx))
	if err != nil {
		fmt.Println("error: ", err)
		return err
	}

	if resp.StatusCode/100 != 2 {
		scanner := bufio.NewScanner(io.LimitReader(resp.Body, maxErrMsgLen))
		line := ""
		if scanner.Scan() {
			line = scanner.Text()
		}
		err = fmt.Errorf("server returned HTTP status %s (%d): %s", resp.Status, resp.StatusCode, line)
		fmt.Println("error:", err)
	}
	return err
}

func main() {
	lambda.Start(handler)
}
