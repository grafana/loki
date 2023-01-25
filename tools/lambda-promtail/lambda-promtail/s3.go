package main

import (
	"bufio"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"regexp"
	"strconv"
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
	// AWS Application Load Balancers
	// source:  https://docs.aws.amazon.com/elasticloadbalancing/latest/application/load-balancer-access-logs.html#access-log-file-format
	// format:  bucket[/prefix]/AWSLogs/aws-account-id/elasticloadbalancing/region/yyyy/mm/dd/aws-account-id_elasticloadbalancing_region_app.load-balancer-id_end-time_ip-address_random-string.log.gz
	// example: my-bucket/AWSLogs/123456789012/elasticloadbalancing/us-east-1/2022/01/24/123456789012_elasticloadbalancing_us-east-1_app.my-loadbalancer.b13ea9d19f16d015_20220124T0000Z_0.0.0.0_2et2e1mx.log.gz
	// VPC Flow Logs
	// source: https://docs.aws.amazon.com/vpc/latest/userguide/flow-logs-s3.html#flow-logs-s3-path
	// format: bucket-and-optional-prefix/AWSLogs/account_id/vpcflowlogs/region/year/month/day/aws_account_id_vpcflowlogs_region_flow_log_id_YYYYMMDDTHHmmZ_hash.log.gz
	// example: 123456789012_vpcflowlogs_us-east-1_fl-1234abcd_20180620T1620Z_fe123456.log.gz
	// AWS WAF logs
	// source:  https://docs.aws.amazon.com/waf/latest/developerguide/logging-s3.html
	// format:  bucket[/prefix]/AWSLogs/aws-account-id/WAFLogs/region/webacl-name/YYYY/MM/dd/HH/mm/aws-account-id_waflogs_region_webacl-name_YYYYMMddTHHmmZ_random-string.log.gz
	// example: aws-waf-logs-test/AWSLogs/11111111111/WAFLogs/us-east-1/TEST-WEBACL/2021/10/28/19/50/11111111111_waflogs_us-east-1_TEST-WEBACL_20211028T1950Z_e0ca43b5.log.gz
	filenameRegex = regexp.MustCompile(`AWSLogs/(?P<account_id>\d+)/(?P<type>\w+)/(?P<region>[\w-]+)/(?:[\w-]+/)?(?P<year>\d+)/(?P<month>\d+)/(?P<day>\d+)/(?:(?P<hour>\d+)/)?(?:(?P<minute>\d+)/)?\d+_(?:elasticloadbalancing|vpcflowlogs|waflogs)_\w+-\w+-\d_(?:(?:app|nlb|net)\.*?)?(?P<src>[a-zA-Z0-9\-]+)`)

	// regex that extracts the timestamp from message log
	timestampRegexList = []*regexp.Regexp {
		regexp.MustCompile(`\w+ (?P<timestamp>\d+-\d+-\d+T\d+:\d+:\d+\.\d+Z)`), //h2 2022-12-20T23:55:02.599911Z ...
		regexp.MustCompile(`(?P<begin_date>\d{8,}) (?P<end_date>\d{8,}) (?:ACCEPT|REJECT)`), //... 1669842701 1669842702 ACCEPT ... (seconds)
		regexp.MustCompile(`"timestamp":(?P<timestamp>\d+)`), //{"timestamp":1671624901861,... (milliseconds)
	}
)

const (
	FLOW_LOG_TYPE string = "vpcflowlogs"
	LB_LOG_TYPE   string = "elasticloadbalancing"
	WAF_LOG_TYPE  string = "waflogs"
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

	skipHeader := false
	logType := labels["type"]
	if labels["type"] == FLOW_LOG_TYPE {
		skipHeader = true
		logType = "s3_vpc_flow"
	} else if labels["type"] == LB_LOG_TYPE {
		logType = "s3_lb"
	}

	ls := model.LabelSet{
		model.LabelName("__aws_log_type"):                          model.LabelValue(logType),
		model.LabelName(fmt.Sprintf("__aws_%s_lb", logType)):       model.LabelValue(labels["src"]),
		model.LabelName(fmt.Sprintf("__aws_%s_lb_owner", logType)): model.LabelValue(labels["account_id"]),
	}

	ls = applyExtraLabels(ls)

	var lineCount int
	for scanner.Scan() {
		log_line := scanner.Text()
		lineCount++
		if lineCount == 1 && skipHeader {
			continue
		}
		if printLogLine {
			fmt.Println(log_line)
		}

		timestamp := parseLogLineTimestamp(log_line)

		if err := b.add(ctx, entry{ls, logproto.Entry{
			Line:      log_line,
			Timestamp: timestamp,
		}}); err != nil {
			return err
		}
	}

	return nil
}

func parseLogLineTimestamp(log_line string) time.Time {
    for _, regex := range timestampRegexList {
        match := regex.FindStringSubmatch(log_line)
        if len(match) > 0 {
            var timestamp time.Time
            var err error
            var timeToParseNumber int64

            //Try RFC3339 format
            timestamp, err = time.Parse(time.RFC3339, match[1])
            if err == nil {
                return timestamp
            }

            //Try milliseconds/seconds format
            timeToParseNumber, err = strconv.ParseInt(match[1], 10, 64)
            if err == nil {
				//https://stackoverflow.com/questions/23929145/how-to-test-if-a-given-time-stamp-is-in-seconds-or-milliseconds
				dateNowSecs := time.Now().Unix()
				if timeToParseNumber > dateNowSecs {
					return time.UnixMilli(timeToParseNumber)
				}
				return time.Unix(timeToParseNumber, 0)
			}
        }
    }

	//Use current time if no timestamp can be detected
    return time.Now()
}

func getLabels(record events.S3EventRecord) (map[string]string, error) {

	labels := make(map[string]string)

	labels["key"] = record.S3.Object.Key
	labels["bucket"] = record.S3.Bucket.Name
	labels["bucket_owner"] = record.S3.Bucket.OwnerIdentity.PrincipalID
	labels["bucket_region"] = record.AWSRegion

	match := filenameRegex.FindStringSubmatch(labels["key"])
	if len(match) > 0 {
		for i, name := range filenameRegex.SubexpNames() {
			if i != 0 && name != "" {
				labels[name] = match[i]
			}
		}
		labels["type"] = strings.ToLower(labels["type"])
	} else {
		fmt.Printf("Unknown AWS S3 log filename format: %s\n", labels["key"])
	}

	return labels, nil
}

func processS3Event(ctx context.Context, ev *events.S3Event) error {
	batch, err := newBatch(ctx)
	if err != nil {
		return err
	}

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

	err = sendToPromtail(ctx, batch)
	if err != nil {
		return err
	}

	return nil
}
