package main

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/prometheus/common/model"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

var (
	// regex that parses the log file name fields
	filenameRegex = regexp.MustCompile(`AWSLogs\/(?P<account_id>\d+)\/elasticloadbalancing\/(?P<region>[\w-]+)\/(?P<year>\d+)\/(?P<month>\d+)\/(?P<day>\d+)\/\d+\_elasticloadbalancing\_\w+-\w+-\d_(?:(?:app|nlb)\.*?)?(?P<lb>[a-zA-Z\-]+)`)

	// regex that extracts the timestamp (RFC3339) from message log
	timestampRegex = regexp.MustCompile(`\w+ (?P<timestamp>\d+-\d+-\d+T\d+:\d+:\d+\.\d+Z)`)
)

func getS3Object(ctx context.Context, labels map[string]string) (io.ReadCloser, error) {
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(labels["bucket_region"]))
	if err != nil {
		return nil, err
	}
	s3Client := s3.NewFromConfig(cfg)

	obj, err := s3Client.GetObject(ctx,
		&s3.GetObjectInput{
			Bucket:              aws.String(labels["bucket"]),
			Key:                 aws.String(labels["key"]),
			ExpectedBucketOwner: aws.String(labels["bucketOwner"]),
		})

	if err != nil {
		fmt.Println("Failed to get object %s from bucket %s on account %s", labels["key"], labels["bucket"], labels["bucketOwner"])
		return nil, err
	}

	return obj.Body, nil
}

func parseS3Log(b *batch, labels map[string]string, obj io.ReadCloser) error {
	gzreader, err := gzip.NewReader(obj)
	if err != nil {
		return err
	}

	scanner := bufio.NewScanner(gzreader)

	ls := model.LabelSet{
		model.LabelName("__aws_log_type"):    model.LabelValue("s3_lb"),
		model.LabelName("__s3_log_lb"):       model.LabelValue(labels["lb"]),
		model.LabelName("__s3_log_lb_owner"): model.LabelValue(labels["account_id"]),
	}

	for scanner.Scan() {
		i := 0
		log_line := scanner.Text()
		match := timestampRegex.FindStringSubmatch(log_line)

		timestamp, err := time.Parse(time.RFC3339, match[1])
		if err != nil {
			return err
		}

		b.add(entry{ls, logproto.Entry{
			Line:      log_line,
			Timestamp: timestamp,
		}})
		i++
	}

	return nil
}
func checkIfS3Event(ev map[string]interface{}) (events.S3Event, error) {
	var s3 events.S3Event

	j, _ := json.Marshal(ev)
	d := json.NewDecoder(strings.NewReader(string(j)))
	d.DisallowUnknownFields()

	err := d.Decode(&s3)

	return s3, err
}

func getLabels(record events.S3EventRecord) (map[string]string, error) {

	labels := make(map[string]string)

	labels["key"] = record.S3.Object.Key
	labels["bucket"] = record.S3.Bucket.Name
	labels["bucket_owner"] = record.S3.Bucket.OwnerIdentity.PrincipalID
	labels["bucket_region"] = record.AWSRegion

	match := filenameRegex.FindStringSubmatch(labels["key"])
	for i, name := range filenameRegex.SubexpNames() {
		if i != 0 && name != "" {
			labels[name] = match[i]
		}
	}

	return labels, nil
}

func processS3(ctx context.Context, ev events.S3Event) error {

	batch := newBatch()

	for _, record := range ev.Records {
		labels, err := getLabels(record)
		if err != nil {
			return err
		}

		obj, err := getS3Object(ctx, labels)
		if err != nil {
			return err
		}

		err = parseS3Log(batch, labels, obj)
		if err != nil {
			return err
		}

	}

	err := sendToPromtail(ctx, batch)
	if err != nil {
		return err
	}

	return nil
}
