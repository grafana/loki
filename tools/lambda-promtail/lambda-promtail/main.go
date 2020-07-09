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

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/gogo/protobuf/proto"
	"github.com/golang/snappy"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/prometheus/common/model"
)

const (
	// We use snappy-encoded protobufs over http by default.
	contentType = "application/x-protobuf"

	maxErrMsgLen = 1024
)

var promtailAddress *url.URL

func init() {
	addr := os.Getenv("PROMTAIL_ADDRESS")
	if addr == "" {
		panic(errors.New("required environmental variable PROMTAIL_ADDRESS not present"))
	}
	var err error
	promtailAddress, err = url.Parse(addr)
	if err != nil {
		panic(err)
	}
}

func handler(ctx context.Context, ev events.CloudwatchLogsEvent) error {

	data, err := ev.AWSLogs.Parse()
	if err != nil {
		return err
	}

	stream := logproto.Stream{
		Labels: model.LabelSet{
			model.LabelName("__aws_lambda_log_group"):  model.LabelValue(data.LogGroup),
			model.LabelName("__aws_lambda_log_stream"): model.LabelValue(data.LogStream),
			model.LabelName("__aws_lambda_owner"):      model.LabelValue(data.Owner),
		}.String(),
		Entries: make([]logproto.Entry, 0, len(data.LogEvents)),
	}

	for _, entry := range data.LogEvents {
		stream.Entries = append(stream.Entries, logproto.Entry{
			// We ignore timestamps from cloudwatch here as promtail is responsible for adding those.
			Line: entry.Message,
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
	req, err := http.NewRequest("POST", promtailAddress.String(), bytes.NewReader(buf))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", contentType)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}

	if resp.StatusCode/100 != 2 {
		scanner := bufio.NewScanner(io.LimitReader(resp.Body, maxErrMsgLen))
		line := ""
		if scanner.Scan() {
			line = scanner.Text()
		}
		err = fmt.Errorf("server returned HTTP status %s (%d): %s", resp.Status, resp.StatusCode, line)
	}

	return err
}

func main() {
	lambda.Start(handler)
}
