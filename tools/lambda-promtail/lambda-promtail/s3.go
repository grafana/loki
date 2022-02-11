package main

import (
	"bufio"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"regexp"
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
	// source:  https://docs.aws.amazon.com/elasticloadbalancing/latest/application/load-balancer-access-logs.html#access-log-file-format
	// format:  bucket[/prefix]/AWSLogs/aws-account-id/elasticloadbalancing/region/yyyy/mm/dd/aws-account-id_elasticloadbalancing_region_app.load-balancer-id_end-time_ip-address_random-string.log.gz
	// example: my-bucket/AWSLogs/123456789012/elasticloadbalancing/us-east-1/2022/01/24/123456789012_elasticloadbalancing_us-east-1_app.my-loadbalancer.b13ea9d19f16d015_20220124T0000Z_0.0.0.0_2et2e1mx.log.gz
	filenameRegex = regexp.MustCompile(`AWSLogs\/(?P<account_id>\d+)\/elasticloadbalancing\/(?P<region>[\w-]+)\/(?P<year>\d+)\/(?P<month>\d+)\/(?P<day>\d+)\/\d+\_elasticloadbalancing\_\w+-\w+-\d_(?:(?:app|nlb)\.*?)?(?P<lb>[a-zA-Z\-]+)`)

	// regex that extracts the timestamp (RFC3339) from message log
	timestampRegex = regexp.MustCompile(`\w+ (?P<timestamp>\d+-\d+-\d+T\d+:\d+:\d+\.\d+Z)`)
)

func getS3Object(ctx context.Context, labels map[string]string) (io.ReadCloser, error) {
	var s3Client *s3.Client

	if c, ok := s3Clients[labels["bucket_region"]]; ok {
		s3Client = c
	} else {
		cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(labels["bucket_region"]))
		if err != nil {
			return nil, err
		}
		s3Client = s3.NewFromConfig(cfg)
		s3Clients[labels["bucket_region"]] = s3Client
	}

	obj, err := s3Client.GetObject(ctx,
		&s3.GetObjectInput{
			Bucket:              aws.String(labels["bucket"]),
			Key:                 aws.String(labels["key"]),
			ExpectedBucketOwner: aws.String(labels["bucketOwner"]),
		})

	if err != nil {
		fmt.Printf("Failed to get object %s from bucket %s on account %s\n", labels["key"], labels["bucket"], labels["bucketOwner"])
		return nil, err
	}

	return obj.Body, nil
}

func parseS3Log(ctx context.Context, b *batch, labels map[string]string, obj io.ReadCloser) error {
	gzreader, err := gzip.NewReader(obj)
	if err != nil {
		return err
	}

	scanner := bufio.NewScanner(gzreader)

	ls := model.LabelSet{
		model.LabelName("__aws_log_type"):        model.LabelValue("s3_lb"),
		model.LabelName("__aws_s3_log_lb"):       model.LabelValue(labels["lb"]),
		model.LabelName("__aws_s3_log_lb_owner"): model.LabelValue(labels["account_id"]),
	}

	ls = applyExtraLabels(ls)

	for scanner.Scan() {
		i := 0
		log_line := scanner.Text()
		match := timestampRegex.FindStringSubmatch(log_line)

		timestamp, err := time.Parse(time.RFC3339, match[1])
		if err != nil {
			return err
		}

		b.add(ctx, entry{ls, logproto.Entry{
			Line:      log_line,
			Timestamp: timestamp,
		}})
		i++
	}

	return nil
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

func processS3Event(ctx context.Context, ev *events.S3Event) error {

	batch, _ := newBatch(ctx)

	for _, record := range ev.Records {
		labels, err := getLabels(record)
		if err != nil {
			return err
		}

		obj, err := getS3Object(ctx, labels)
		if err != nil {
			return err
		}

		err = parseS3Log(ctx, batch, labels, obj)
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
