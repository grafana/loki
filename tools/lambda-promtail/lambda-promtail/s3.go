package main

import (
	"bufio"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/prometheus/common/model"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type logDefinition struct {
	logType        string
	timestampType  string
	filenameRegex  *regexp.Regexp
	timestampRegex *regexp.Regexp
	skipHeader     bool
}

const (
	S3_LB       string = "s3_lb"
	S3_VPC_FLOW string = "s3_vpc_flow"
	S3_WAF             = "s3_waf"
)

var (
	logDefinitionList = []logDefinition{
		logDefinition{
			logType:       S3_LB,
			timestampType: "rfc3339",
			// regex that parses the log file name fields for AWS Loadbalancer logs in S3
			// source:  https://docs.aws.amazon.com/elasticloadbalancing/latest/application/load-balancer-access-logs.html#access-log-file-format
			// format:  bucket[/prefix]/AWSLogs/aws-account-id/elasticloadbalancing/region/yyyy/mm/dd/aws-account-id_elasticloadbalancing_region_app.load-balancer-id_end-time_ip-address_random-string.log.gz
			// example: my-bucket/AWSLogs/123456789012/elasticloadbalancing/us-east-1/2022/01/24/123456789012_elasticloadbalancing_us-east-1_app.my-loadbalancer.b13ea9d19f16d015_20220124T0000Z_0.0.0.0_2et2e1mx.log.gz
			filenameRegex: regexp.MustCompile(`AWSLogs\/(?P<account_id>\d+)\/elasticloadbalancing\/(?P<region>[\w-]+)\/(?P<year>\d+)\/(?P<month>\d+)\/(?P<day>\d+)\/\d+\_elasticloadbalancing\_\w+-\w+-\d_(?:(?:app|nlb|net)\.*?)?(?P<src>[a-zA-Z0-9\-]+)`),
			// regex that extracts the timestamp from the AWS Loadbalancer message log
			// format: timestamp (RFC3339)
			// example: h2 2022-12-20T23:55:02.599911Z ...
			timestampRegex: regexp.MustCompile(`\w+ (?P<timestamp>\d+-\d+-\d+T\d+:\d+:\d+\.\d+Z)`),
			skipHeader:     false,
		},
		logDefinition{
			logType:       S3_VPC_FLOW,
			timestampType: "seconds",
			// regex that parses the log file name fields for AWS VPC Flow Logs in S3
			// source: https://docs.aws.amazon.com/vpc/latest/userguide/flow-logs-s3.html#flow-logs-s3-path
			// format: bucket-and-optional-prefix/AWSLogs/account_id/vpcflowlogs/region/year/month/day/aws_account_id_vpcflowlogs_region_flow_log_id_YYYYMMDDTHHmmZ_hash.log.gz
			// example: 123456789012_vpcflowlogs_us-east-1_fl-1234abcd_20180620T1620Z_fe123456.log.gz
			filenameRegex: regexp.MustCompile(`AWSLogs\/(?P<account_id>\d+)\/vpcflowlogs\/(?P<region>[\w-]+)\/(?P<year>\d+)\/(?P<month>\d+)\/(?P<day>\d+)\/\d+\_vpcflowlogs\_\w+-\w+-\d_(?:(?:app|nlb|net)\.*?)?(?P<src>[a-zA-Z0-9\-]+)`),
			// regex that extracts the timestamp from the AWS VPC Flow Logs message log
			// format: timestamp (seconds)
			// example: ... 1669842701 1669842702 ACCEPT
			timestampRegex: regexp.MustCompile(`(?P<begin_date>\d+) (?P<end_date>\d+) (?:ACCEPT|REJECT)`),
			skipHeader:     true,
		},
		logDefinition{
			logType:       S3_WAF,
			timestampType: "milliseconds",
			// regex that parses the log file name fields for AWS WAFv2 logs in S3
			// source:  https://docs.aws.amazon.com/waf/latest/developerguide/logging-s3.html
			// format:  bucket[/prefix]/AWSLogs/aws-account-id/WAFLogs/region/webacl-name/YYYY/MM/dd/HH/mm/aws-account-id_waflogs_region_webacl-name_YYYYMMddTHHmmZ_random-string.log.gz
			// example: aws-waf-logs-test/AWSLogs/11111111111/WAFLogs/us-east-1/TEST-WEBACL/2021/10/28/19/50/11111111111_waflogs_us-east-1_TEST-WEBACL_20211028T1950Z_e0ca43b5.log.gz
			filenameRegex: regexp.MustCompile(`AWSLogs\/(?P<account_id>\d+)\/WAFLogs\/(?P<region>[\w-]+)\/(?P<src>[a-zA-Z0-9\-]+)\/(?P<year>\d+)\/(?P<month>\d+)\/(?P<day>\d+)\/(?P<hour>\d+)\/(?P<minute>\d+)\/\d+\_waflogs\_`),
			// regex that extracts the timestamp from the AWS WAFv2 message log
			// format: timestamp (milliseconds)
			// example: {"timestamp":1671624901861,...
			timestampRegex: regexp.MustCompile(`"timestamp":(?P<timestamp>\d+)`),
			skipHeader:     false,
		},
	}
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

	logType := labels["type"]

	ls := model.LabelSet{
		model.LabelName("__aws_log_type"):                          model.LabelValue(logType),
		model.LabelName(fmt.Sprintf("__aws_%s_lb", logType)):       model.LabelValue(labels["src"]),
		model.LabelName(fmt.Sprintf("__aws_%s_lb_owner", logType)): model.LabelValue(labels["account_id"]),
	}
	ls = applyExtraLabels(ls)

	logDef, found := findLogDefinitionByType(logType)
	if found {
		timestamp := time.Now()
		var err error
		var lineCount int
		for scanner.Scan() {
			log_line := scanner.Text()
			lineCount++
			if lineCount == 1 && logDef.skipHeader {
				continue
			}
			if printLogLine {
				fmt.Println(log_line)
			}
			match := logDef.timestampRegex.FindStringSubmatch(log_line)
			if len(match) > 0 {
				timestamp, err = parseLogTime(logDef.timestampType, match[1])
				if err != nil {
					return err
				}
			}

			if err := b.add(ctx, entry{ls, logproto.Entry{
				Line:      log_line,
				Timestamp: timestamp,
			}}); err != nil {
				return err
			}
		}
	}
	return nil
}

func parseLogTime(timestampType string, timeToParse string) (time.Time, error) {
	switch timestampType {
	case "rfc3339":
		return time.Parse(time.RFC3339, timeToParse)
	case "seconds":
		timeToParseNumber, err := strconv.ParseInt(timeToParse, 10, 64)
		if err != nil {
			return time.Time{}, err
		}
		return time.Unix(timeToParseNumber, 0), nil
	case "milliseconds":
		timeToParseNumber, err := strconv.ParseInt(timeToParse, 10, 64)
		if err != nil {
			return time.Time{}, err
		}
		return time.UnixMilli(timeToParseNumber), nil
	default:
		return time.Time{}, errors.New("cannot parse log timestamp string")
	}
}

func findLogDefinitionByType(logType string) (logDefinition, bool) {
	for _, e := range logDefinitionList {
		if e.logType == logType {
			return e, true
		}
	}
	return logDefinition{}, false
}

func getLabels(record events.S3EventRecord) (map[string]string, error) {
	labels := make(map[string]string)

	labels["key"] = record.S3.Object.Key
	labels["bucket"] = record.S3.Bucket.Name
	labels["bucket_owner"] = record.S3.Bucket.OwnerIdentity.PrincipalID
	labels["bucket_region"] = record.AWSRegion

	logType, match, matchNames := parseLogFilename(labels["key"])

	if logType != "" {
		labels["type"] = logType

		for i, name := range matchNames {
			if i != 0 && name != "" {
				labels[name] = match[i]
			}
		}
	}

	return labels, nil
}

func parseLogFilename(bucketKey string) (string, []string, []string) {
	for _, e := range logDefinitionList {
		match := e.filenameRegex.FindStringSubmatch(bucketKey)
		if match != nil {
			return e.logType, match, e.filenameRegex.SubexpNames()
		}
	}

	fmt.Printf("Unknown S3 log format: %s\n", bucketKey)
	return "", nil, nil
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
