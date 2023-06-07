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
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/pkg/logproto"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

const (
	FLOW_LOG_TYPE              string = "vpcflowlogs"
	LB_LOG_TYPE                string = "elasticloadbalancing"
	CLOUDTRAIL_LOG_TYPE        string = "CloudTrail"
	CLOUDTRAIL_DIGEST_LOG_TYPE string = "CloudTrail-Digest"
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
	// CloudTrail
	// source: https://docs.aws.amazon.com/awscloudtrail/latest/userguide/cloudtrail-log-file-examples.html#cloudtrail-log-filename-format
	// example: 111122223333_CloudTrail_us-east-2_20150801T0210Z_Mu0KsOhtH1ar15ZZ.json.gz
	defaultFilenameRegex    = regexp.MustCompile(`AWSLogs\/(?P<account_id>\d+)\/(?P<type>[a-zA-Z0-9_\-]+)\/(?P<region>[\w-]+)\/(?P<year>\d+)\/(?P<month>\d+)\/(?P<day>\d+)\/\d+\_(?:elasticloadbalancing|vpcflowlogs)\_\w+-\w+-\d_(?:(?:app|nlb|net)\.*?)?(?P<src>[a-zA-Z0-9\-]+)`)
	cloudtrailFilenameRegex = regexp.MustCompile(`AWSLogs\/(?P<account_id>\d+)\/(?P<type>[a-zA-Z0-9_\-]+)\/(?P<region>[\w-]+)\/(?P<year>\d+)\/(?P<month>\d+)\/(?P<day>\d+)\/\d+\_(?:CloudTrail|CloudTrail-Digest)\_\w+-\w+-\d_(?:(?:app|nlb|net)\.*?)?.+_(?P<src>[a-zA-Z0-9\-]+)`)
	filenameRegexes         = map[string]*regexp.Regexp{
		FLOW_LOG_TYPE:       defaultFilenameRegex,
		LB_LOG_TYPE:         defaultFilenameRegex,
		CLOUDTRAIL_LOG_TYPE: cloudtrailFilenameRegex,
	}
	// regex that extracts the timestamp (RFC3339) from message log
	timestampRegex = regexp.MustCompile(`\w+ (?P<timestamp>\d+-\d+-\d+T\d+:\d+:\d+\.\d+Z)`)
)

func getS3Client(ctx context.Context, region string) (*s3.Client, error) {
	var s3Client *s3.Client

	if c, ok := s3Clients[region]; ok {
		s3Client = c
	} else {
		cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
		if err != nil {
			return nil, err
		}
		s3Client = s3.NewFromConfig(cfg)
		s3Clients[region] = s3Client
	}
	return s3Client, nil
}

func parseS3Log(ctx context.Context, b *batch, labels map[string]string, obj io.ReadCloser) error {
	gzreader, err := gzip.NewReader(obj)
	if err != nil {
		return err
	}

	scanner := bufio.NewScanner(gzreader)

	skipHeader := false
	logType := ""
	switch labels["type"] {
	case FLOW_LOG_TYPE:
		skipHeader = true
		logType = "s3_vpc_flow"
	case LB_LOG_TYPE:
		logType = "s3_lb"
	case CLOUDTRAIL_LOG_TYPE:
		logType = "s3_cloudtrail"
	case CLOUDTRAIL_DIGEST_LOG_TYPE:
		// do not ingest digest files' content
		return nil
	}

	ls := model.LabelSet{
		model.LabelName("__aws_log_type"):                       model.LabelValue(logType),
		model.LabelName(fmt.Sprintf("__aws_%s", logType)):       model.LabelValue(labels["src"]),
		model.LabelName(fmt.Sprintf("__aws_%s_owner", logType)): model.LabelValue(labels["account_id"]),
	}

	ls = applyExtraLabels(ls)

	// extract the timestamp of the nested event and sends the rest as raw json
	if labels["type"] == CLOUDTRAIL_LOG_TYPE {
		records := make(chan Record)
		jsonStream := NewJSONStream(records)
		go jsonStream.Start(gzreader, 3)
		// Stream json file
		for record := range jsonStream.records {
			if record.Error != nil {
				return record.Error
			}
			trailEntry, err := parseCloudtrailRecord(record)
			if err != nil {
				return err
			}
			if err := b.add(ctx, entry{ls, trailEntry}); err != nil {
				return err
			}
		}
		return nil
	}

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

		match := timestampRegex.FindStringSubmatch(log_line)
		timestamp := time.Now()
		if len(match) > 0 {
			timestamp, err = time.Parse(time.RFC3339, match[1])
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

	return nil
}

func getLabels(record events.S3EventRecord) (map[string]string, error) {
	labels := make(map[string]string)

	labels["key"] = record.S3.Object.Key
	labels["bucket"] = record.S3.Bucket.Name
	labels["bucket_owner"] = record.S3.Bucket.OwnerIdentity.PrincipalID
	labels["bucket_region"] = record.AWSRegion
	var matchingExp *regexp.Regexp
	var matchingType *string
	for key, exp := range filenameRegexes {
		if exp.MatchString(labels["key"]) {
			matchingExp = exp
			matchingType = aws.String(key)
		}
	}
	match := matchingExp.FindStringSubmatch(labels["key"])
	for i, name := range matchingExp.SubexpNames() {
		if i != 0 && name != "" {
			labels[name] = match[i]
		}
	}
	if labels["type"] == "" {
		labels["type"] = *matchingType
	}
	return labels, nil
}

func processS3Event(ctx context.Context, ev *events.S3Event, pc Client, log *log.Logger) error {
	batch, err := newBatch(ctx, pc)
	if err != nil {
		return err
	}
	for _, record := range ev.Records {
		labels, err := getLabels(record)
		if err != nil {
			return err
		}
		level.Info(*log).Log("msg", fmt.Sprintf("fetching s3 file: %s", labels["key"]))
		s3Client, err := getS3Client(ctx, labels["bucket_region"])
		if err != nil {
			return err
		}
		obj, err := s3Client.GetObject(ctx,
			&s3.GetObjectInput{
				Bucket:              aws.String(labels["bucket"]),
				Key:                 aws.String(labels["key"]),
				ExpectedBucketOwner: aws.String(labels["bucketOwner"]),
			})
		if err != nil {
			return fmt.Errorf("Failed to get object %s from bucket %s on account %s\n, %s", labels["key"], labels["bucket"], labels["bucketOwner"], err)
		}
		err = parseS3Log(ctx, batch, labels, obj.Body)
		if err != nil {
			return err
		}
	}

	err = pc.sendToPromtail(ctx, batch)
	if err != nil {
		return err
	}

	return nil
}
