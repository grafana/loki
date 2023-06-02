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

type parserConfig struct {
	// value to use for __aws_log_type label
	logTypeLabel string
	// regex matching filename and and exporting labels from it
	filenameRegex *regexp.Regexp
	// regex that extracts the timestamp from the log sample
	timestampRegex *regexp.Regexp
	// time format to use to convert the timestamp to time.Time
	timestampFormat string
	// how many lines or jsonToken to skip at the beginning of the file
	skipHeaderCount int
	// key of the metadata label to use as a value for the__aws_<logType>_owner label
	ownerLabelKey string
}

const (
	FLOW_LOG_TYPE              string = "vpcflowlogs"
	LB_LOG_TYPE                string = "elasticloadbalancing"
	CLOUDTRAIL_LOG_TYPE        string = "CloudTrail"
	CLOUDTRAIL_DIGEST_LOG_TYPE string = "CloudTrail-Digest"
	CLOUDFRONT_LOG_TYPE        string = "cloudfront"
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
	// CloudFront
	// source https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/AccessLogs.html#AccessLogsFileNaming
	// example: example-prefix/EMLARXS9EXAMPLE.2019-11-14-20.RT4KCN4SGK9.gz
	defaultFilenameRegex     = regexp.MustCompile(`AWSLogs\/(?P<account_id>\d+)\/(?P<type>[a-zA-Z0-9_\-]+)\/(?P<region>[\w-]+)\/(?P<year>\d+)\/(?P<month>\d+)\/(?P<day>\d+)\/\d+\_(?:elasticloadbalancing|vpcflowlogs)\_\w+-\w+-\d_(?:(?:app|nlb|net)\.*?)?(?P<src>[a-zA-Z0-9\-]+)`)
	defaultTimestampRegex    = regexp.MustCompile(`\w+ (?P<timestamp>\d+-\d+-\d+T\d+:\d+:\d+\.\d+Z)`)
	cloudtrailFilenameRegex  = regexp.MustCompile(`AWSLogs\/(?P<account_id>\d+)\/(?P<type>[a-zA-Z0-9_\-]+)\/(?P<region>[\w-]+)\/(?P<year>\d+)\/(?P<month>\d+)\/(?P<day>\d+)\/\d+\_(?:CloudTrail|CloudTrail-Digest)\_\w+-\w+-\d_(?:(?:app|nlb|net)\.*?)?.+_(?P<src>[a-zA-Z0-9\-]+)`)
	cloudfrontFilenameRegex  = regexp.MustCompile(`(?P<prefix>.*)\/(?P<src>[A-Z0-9]+)\.(?P<year>\d+)-(?P<month>\d+)-(?P<day>\d+)-(.+)`)
	cloudfrontTimestampRegex = regexp.MustCompile(`(?P<timestamp>\d+-\d+-\d+\s\d+:\d+:\d+)`)
	parsers                  = map[string]parserConfig{
		FLOW_LOG_TYPE: {
			logTypeLabel:    "s3_vpc_flow",
			filenameRegex:   defaultFilenameRegex,
			ownerLabelKey:   "account_id",
			timestampRegex:  defaultTimestampRegex,
			timestampFormat: time.RFC3339,
			skipHeaderCount: 1,
		},
		LB_LOG_TYPE: {
			logTypeLabel:    "s3_lb",
			filenameRegex:   defaultFilenameRegex,
			ownerLabelKey:   "account_id",
			timestampFormat: time.RFC3339,
			timestampRegex:  defaultTimestampRegex,
		},
		CLOUDTRAIL_LOG_TYPE: {
			logTypeLabel:    "s3_cloudtrail",
			ownerLabelKey:   "account_id",
			skipHeaderCount: 3,
			filenameRegex:   cloudtrailFilenameRegex,
		},
		CLOUDFRONT_LOG_TYPE: {
			logTypeLabel:    "s3_cloudfront",
			filenameRegex:   cloudfrontFilenameRegex,
			ownerLabelKey:   "prefix",
			timestampRegex:  cloudfrontTimestampRegex,
			timestampFormat: "2006-01-02\x0915:04:05",
			skipHeaderCount: 2,
		},
	}
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
	parser, ok := parsers[labels["type"]]
	if !ok {
		if labels["type"] == CLOUDTRAIL_DIGEST_LOG_TYPE {
			return nil
		}
		return fmt.Errorf("could not find parser for type %s", labels["type"])
	}
	gzreader, err := gzip.NewReader(obj)
	if err != nil {
		return err
	}

	scanner := bufio.NewScanner(gzreader)

	ls := model.LabelSet{
		model.LabelName("__aws_log_type"):                                   model.LabelValue(parser.logTypeLabel),
		model.LabelName(fmt.Sprintf("__aws_%s", parser.logTypeLabel)):       model.LabelValue(labels["src"]),
		model.LabelName(fmt.Sprintf("__aws_%s_owner", parser.logTypeLabel)): model.LabelValue(labels[parser.ownerLabelKey]),
	}

	ls = applyExtraLabels(ls)

	// extract the timestamp of the nested event and sends the rest as raw json
	if labels["type"] == CLOUDTRAIL_LOG_TYPE {
		records := make(chan Record)
		jsonStream := NewJSONStream(records)
		go jsonStream.Start(gzreader, parser.skipHeaderCount)
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
		if lineCount <= parser.skipHeaderCount {
			continue
		}
		if printLogLine {
			fmt.Println(log_line)
		}

		timestamp := time.Now()
		match := parser.timestampRegex.FindStringSubmatch(log_line)
		if len(match) > 0 {
			timestamp, err = time.Parse(parser.timestampFormat, match[1])
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
	var matchingType *string
	for key, p := range parsers {
		if p.filenameRegex.MatchString(labels["key"]) {
			matchingType = aws.String(key)
			match := p.filenameRegex.FindStringSubmatch(labels["key"])
			for i, name := range p.filenameRegex.SubexpNames() {
				if i != 0 && name != "" {
					labels[name] = match[i]
				}
			}
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
