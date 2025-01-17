package main

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"regexp"
	"strconv"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/v3/pkg/logproto"

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
	// if the timestamp is a string that can be parsed or a Unix timestamp
	timestampType string
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
	LB_NLB_TYPE                string = "net"
	LB_ALB_TYPE                string = "app"
	WAF_LOG_TYPE               string = "WAFLogs"
	GUARDDUTY_LOG_TYPE         string = "GuardDuty"
)

var (
	// AWS Application Load Balancers
	// source:  https://docs.aws.amazon.com/elasticloadbalancing/latest/application/load-balancer-access-logs.html#access-log-file-format
	// format:  bucket[/prefix]/AWSLogs/aws-account-id/elasticloadbalancing/region/yyyy/mm/dd/aws-account-id_elasticloadbalancing_region_app.load-balancer-id_end-time_ip-address_random-string.log.gz
	// example: my-bucket/AWSLogs/123456789012/elasticloadbalancing/us-east-1/2022/01/24/123456789012_elasticloadbalancing_us-east-1_app.my-loadbalancer.b13ea9d19f16d015_20220124T0000Z_0.0.0.0_2et2e1mx.log.gz
	// AWS Network Load Balancers
	// source:  https://docs.aws.amazon.com/elasticloadbalancing/latest/network/load-balancer-access-logs.html#access-log-file-format
	// format:  bucket[/prefix]/AWSLogs/aws-account-id/elasticloadbalancing/region/yyyy/mm/dd/aws-account-id_elasticloadbalancing_region_net.load-balancer-id_end-time_random-string.log.gz
	// example: my-bucket/prefix/AWSLogs/123456789012/elasticloadbalancing/us-east-2/2016/05/01/123456789012_elasticloadbalancing_us-east-2_net.my-loadbalancer.1234567890abcdef_201605010000Z_2soosksi.log.gz
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
	// AWS WAF logs
	// source: https://docs.aws.amazon.com/waf/latest/developerguide/logging-s3.html
	// format: aws-waf-logs-suffix[/prefix]/AWSLogs/aws-account-id/WAFLogs/region/webacl-name/year/month/day/hour/minute/aws-account-id_waflogs_region_webacl-name_timestamp_hash.log.gz
	// example: aws-waf-logs-test/AWSLogs/11111111111/WAFLogs/us-east-1/TEST-WEBACL/2021/10/28/19/50/11111111111_waflogs_us-east-1_TEST-WEBACL_20211028T1950Z_e0ca43b5.log.gz
	// AWS GuardDuty
	// source: https://docs.aws.amazon.com/guardduty/latest/ug/guardduty_exportfindings.html
	// format: my-bucket/AWSLogs/aws-account-id/GuardDuty/region/year/month/day/random-string.jsonl.gz
	// example: my-bucket/AWSLogs/123456789012/GuardDuty/us-east-1/2024/05/30/07a3f2ce-1485-3031-b842-e1f324c4a48d.jsonl.gz
	defaultFilenameRegex     = regexp.MustCompile(`AWSLogs\/(?P<account_id>\d+)\/(?P<type>[a-zA-Z0-9_\-]+)\/(?P<region>[\w-]+)\/(?P<year>\d+)\/(?P<month>\d+)\/(?P<day>\d+)\/\d+\_(?:elasticloadbalancing|vpcflowlogs)_(?:\w+-\w+-(?:\w+-)?\d)_(?:(?P<lb_type>app|net)\.*?)?(?P<src>[a-zA-Z0-9\-]+)`)
	defaultTimestampRegex    = regexp.MustCompile(`(?P<timestamp>\d+-\d+-\d+T\d+:\d+:\d+(?:\.\d+Z)?)`)
	cloudtrailFilenameRegex  = regexp.MustCompile(`AWSLogs\/(?P<organization_id>o-[a-z0-9]{10,32})?\/?(?P<account_id>\d+)\/(?P<type>[a-zA-Z0-9_\-]+)\/(?P<region>[\w-]+)\/(?P<year>\d+)\/(?P<month>\d+)\/(?P<day>\d+)\/\d+\_(?:CloudTrail|CloudTrail-Digest)_(?:\w+-\w+-(?:\w+-)?\d)_(?:(?:app|nlb|net)\.*?)?.+_(?P<src>[a-zA-Z0-9\-]+)`)
	cloudfrontFilenameRegex  = regexp.MustCompile(`(?P<prefix>.*)\/(?P<src>[A-Z0-9]+)\.(?P<year>\d+)-(?P<month>\d+)-(?P<day>\d+)-(.+)`)
	cloudfrontTimestampRegex = regexp.MustCompile(`(?P<timestamp>\d+-\d+-\d+\s\d+:\d+:\d+)`)
	wafFilenameRegex         = regexp.MustCompile(`AWSLogs\/(?P<account_id>\d+)\/(?P<type>WAFLogs)\/(?P<region>[\w-]+)\/(?P<src>[\w-]+)\/(?P<year>\d+)\/(?P<month>\d+)\/(?P<day>\d+)\/(?P<hour>\d+)\/(?P<minute>\d+)\/\d+\_waflogs\_[\w-]+_[\w-]+_\d+T\d+Z_\w+`)
	wafTimestampRegex        = regexp.MustCompile(`"timestamp":\s*(?P<timestamp>\d+),`)
	guarddutyFilenameRegex   = regexp.MustCompile(`AWSLogs\/(?P<account_id>\d+)\/(?P<type>GuardDuty)\/(?P<region>[\w-]+)\/(?P<year>\d+)\/(?P<month>\d+)\/(?P<day>\d+)\/.+`)
	parsers                  = map[string]parserConfig{
		FLOW_LOG_TYPE: {
			logTypeLabel:    "s3_vpc_flow",
			filenameRegex:   defaultFilenameRegex,
			ownerLabelKey:   "account_id",
			timestampRegex:  defaultTimestampRegex,
			timestampFormat: time.RFC3339,
			timestampType:   "string",
			skipHeaderCount: 1,
		},
		LB_LOG_TYPE: {
			logTypeLabel:    "s3_lb",
			filenameRegex:   defaultFilenameRegex,
			ownerLabelKey:   "account_id",
			timestampFormat: time.RFC3339,
			timestampRegex:  defaultTimestampRegex,
			timestampType:   "string",
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
			timestampType:   "string",
			skipHeaderCount: 2,
		},
		WAF_LOG_TYPE: {
			logTypeLabel:   "s3_waf",
			filenameRegex:  wafFilenameRegex,
			ownerLabelKey:  "account_id",
			timestampRegex: wafTimestampRegex,
			timestampType:  "unix",
		},
		GUARDDUTY_LOG_TYPE: {
			logTypeLabel:    "s3_guardduty",
			filenameRegex:   guarddutyFilenameRegex,
			ownerLabelKey:   "account_id",
			timestampFormat: time.RFC3339,
			timestampRegex:  defaultTimestampRegex,
			timestampType:   "string",
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

func parseS3Log(ctx context.Context, b *batch, labels map[string]string, obj io.ReadCloser, log *log.Logger) error {
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

	ls = applyLabels(ls)

	// extract the timestamp of the nested event and sends the rest as raw json
	if labels["type"] == CLOUDTRAIL_LOG_TYPE || labels["type"] == GUARDDUTY_LOG_TYPE {
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
		logLine := scanner.Text()
		lineCount++
		if lineCount <= parser.skipHeaderCount {
			continue
		}
		if printLogLine {
			fmt.Println(logLine)
		}

		timestamp := time.Now()
		match := parser.timestampRegex.FindStringSubmatch(logLine)
		if len(match) > 0 {
			if labels["lb_type"] == LB_NLB_TYPE {
				// NLB logs don't have .SSSSSSZ suffix. RFC3339 requires a TZ specifier, use UTC
				match[1] += "Z"
			}

			switch parser.timestampType {
			case "string":
				timestamp, err = time.Parse(parser.timestampFormat, match[1])
				if err != nil {
					return err
				}
			case "unix":
				sec, nsec, err := getUnixSecNsec(match[1])
				if err != nil {
					return err
				}
				timestamp = time.Unix(sec, nsec).UTC()
			default:
				level.Warn(*log).Log("msg", fmt.Sprintf("timestamp type of %s parser unknown, using current time", labels["type"]))
			}
		}

		if err := b.add(ctx, entry{ls, logproto.Entry{
			Line:      logLine,
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
	for key, p := range parsers {
		if p.filenameRegex.MatchString(labels["key"]) {
			if labels["type"] == "" {
				labels["type"] = key
			}
			match := p.filenameRegex.FindStringSubmatch(labels["key"])
			for i, name := range p.filenameRegex.SubexpNames() {
				if i != 0 && name != "" && match[i] != "" {
					labels[name] = match[i]
				}
			}
		}
	}
	if labels["type"] == "" {
		return labels, fmt.Errorf("type of S3 event could not be determined for object %q", record.S3.Object.Key)
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
				Bucket: aws.String(labels["bucket"]),
				Key:    aws.String(labels["key"]),
			})
		if err != nil {
			return fmt.Errorf("failed to get object %s from bucket %s, %s", labels["key"], labels["bucket"], err)
		}
		err = parseS3Log(ctx, batch, labels, obj.Body, log)
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

func processSNSEvent(ctx context.Context, evt *events.SNSEvent, handler func(ctx context.Context, ev map[string]interface{}) error) error {
	for _, record := range evt.Records {
		event, err := stringToRawEvent(record.SNS.Message)
		if err != nil {
			return err
		}
		err = handler(ctx, event)
		if err != nil {
			return err
		}
	}
	return nil
}

func processSQSEvent(ctx context.Context, evt *events.SQSEvent, handler func(ctx context.Context, ev map[string]interface{}) error) error {
	for _, record := range evt.Records {
		// retrieve nested
		event, err := stringToRawEvent(record.Body)
		if err != nil {
			return err
		}
		err = handler(ctx, event)
		if err != nil {
			return err
		}
	}
	return nil
}

func stringToRawEvent(body string) (map[string]interface{}, error) {
	result := make(map[string]interface{})
	err := json.Unmarshal([]byte(body), &result)
	if err != nil {
		return nil, err
	}
	return result, nil
}

// getUnixSecNsec returns the Unix time seconds and nanoseconds in the string s.
// It assumes that the first 10 digits of the parsed int is the Unix time in seconds and the rest is the nanoseconds part.
// This assumption will hold until 2286-11-20 17:46:40 UTC, so it's a safe assumption.
// It also makes use of the fact that the log10 of a number in base 10 is its number of digits - 1.
// It returns early if the fractional seconds is 0 because getting the log10 of 0 results in -Inf.
// For example, given a string 1234567890123:
//
//	iLog10 = 12  // the parsed int is 13 digits long
//	multiplier = 0.001  // to get the seconds part it must be divided by 1000
//	sec = 1234567890123 * 0.001 = 1234567890  // this is the seconds part of the Unix time
//	fractionalSec = 123  // the rest of the parsed int
//	fractionalSecLog10 = 2  // it is 3 digits long
//	multiplier = 1000000  // nano is 10^-9, so the nanoseconds part is 9 digits long
//	nsec = 123000000  // this is the nanoseconds part of the Unix time
func getUnixSecNsec(s string) (sec int64, nsec int64, err error) {
	const (
		UNIX_SEC_LOG10     = 9
		UNIX_NANOSEC_LOG10 = 8
	)

	i, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return sec, nsec, err
	}

	iLog10 := int(math.Log10(float64(i)))
	multiplier := math.Pow10(UNIX_SEC_LOG10 - iLog10)
	sec = int64(float64(i) * multiplier)

	fractionalSec := float64(i % sec)
	if fractionalSec == 0 {
		return sec, 0, err
	}

	fractionalSecLog10 := int(math.Log10(fractionalSec))
	multiplier = math.Pow10(UNIX_NANOSEC_LOG10 - fractionalSecLog10)
	nsec = int64(fractionalSec * multiplier)

	return sec, nsec, err
}
