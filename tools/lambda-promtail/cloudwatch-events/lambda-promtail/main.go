package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/arn"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
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

func handler(ctx context.Context, ev events.CloudWatchEvent) error {

	sess, err := session.NewSession(&aws.Config{
		Region: aws.String("us-east-1")},
	)

	// Create EC2 service client to retrieve resource Tags if any
	svc := ec2.New(sess)
	var tags map[string]string = make(map[string]string)

	for i := 0; i < len(ev.Resources); i++ {
		resourceArn, _ := arn.Parse(ev.Resources[i])
		resourceIDSplit := strings.Split(resourceArn.Resource, "/")
		resourceID := resourceIDSplit[len(resourceIDSplit)-1]
		if resourceArn.Service == "ec2" {
			input := &ec2.DescribeTagsInput{
				Filters: []*ec2.Filter{
					{
						Name: aws.String("resource-id"),
						Values: []*string{
							aws.String(resourceID),
						},
					},
				},
			}

			result, _ := svc.DescribeTags(input)
			for _, tag := range result.Tags {
				tags[*tag.Key] = *tag.Value
			}
		}
	}

	//setup loki stream
	stream := logproto.Stream{
		Labels: model.LabelSet{
			model.LabelName("__aws_cloudwatch_event_detail_type"): model.LabelValue(ev.DetailType),
			model.LabelName("__aws_cloudwatch_event_source"):      model.LabelValue(ev.Source),
			model.LabelName("__aws_cloudwatch_event_account_id"):  model.LabelValue(ev.AccountID),
		}.String(),
		Entries: make([]logproto.Entry, 0, 1),
	}

	jsonDetail, _ := json.Marshal(&ev.Detail)
	resourcesJSON, _ := json.Marshal(&ev.Resources)
	tagsJSON, _ := json.Marshal(&tags)

	//combine detail, resources and tags
	jsonDetail = append(jsonDetail[:len(jsonDetail)-1], ", \"resources\":"...)
	jsonDetail = append(jsonDetail, resourcesJSON...)
	jsonDetail = append(jsonDetail, ",\"tags\":"...)
	jsonDetail = append(jsonDetail, tagsJSON...)
	jsonDetail = append(jsonDetail, "}"...)

	stream.Entries = append(stream.Entries, logproto.Entry{
		Line: string(jsonDetail),
		// It's best practice to ignore timestamps from cloudwatch as promtail is responsible for adding those.
		Timestamp: ev.Time,
	})

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

	resp, err := http.DefaultClient.Do(req.WithContext(ctx))
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
