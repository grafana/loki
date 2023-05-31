package main

import (
	"context"
	"io"
	"os"
	"reflect"
	"testing"

	"github.com/aws/aws-lambda-go/events"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/stretchr/testify/require"
)

func Test_getLabels(t *testing.T) {
	type args struct {
		record events.S3EventRecord
	}
	tests := []struct {
		name    string
		args    args
		want    map[string]string
		wantErr bool
	}{
		{
			name: "s3_lb",
			args: args{
				record: events.S3EventRecord{
					AWSRegion: "us-east-1",
					S3: events.S3Entity{
						Bucket: events.S3Bucket{
							Name: "elb_logs_test",
							OwnerIdentity: events.S3UserIdentity{
								PrincipalID: "test",
							},
						},
						Object: events.S3Object{
							Key: "my-bucket/AWSLogs/123456789012/elasticloadbalancing/us-east-1/2022/01/24/123456789012_elasticloadbalancing_us-east-1_app.my-loadbalancer.b13ea9d19f16d015_20220124T0000Z_0.0.0.0_2et2e1mx.log.gz",
						},
					},
				},
			},
			want: map[string]string{
				"account_id":    "123456789012",
				"bucket":        "elb_logs_test",
				"bucket_owner":  "test",
				"bucket_region": "us-east-1",
				"day":           "24",
				"key":           "my-bucket/AWSLogs/123456789012/elasticloadbalancing/us-east-1/2022/01/24/123456789012_elasticloadbalancing_us-east-1_app.my-loadbalancer.b13ea9d19f16d015_20220124T0000Z_0.0.0.0_2et2e1mx.log.gz",
				"month":         "01",
				"region":        "us-east-1",
				"src":           "my-loadbalancer",
				"type":          LB_LOG_TYPE,
				"year":          "2022",
			},
			wantErr: false,
		},
		{
			name: "s3_flow_logs",
			args: args{
				record: events.S3EventRecord{
					AWSRegion: "us-east-1",
					S3: events.S3Entity{
						Bucket: events.S3Bucket{
							Name: "vpc_logs_test",
							OwnerIdentity: events.S3UserIdentity{
								PrincipalID: "test",
							},
						},
						Object: events.S3Object{
							Key: "my-bucket/AWSLogs/123456789012/vpcflowlogs/us-east-1/2022/01/24/123456789012_vpcflowlogs_us-east-1_fl-1234abcd_20180620T1620Z_fe123456.log.gz",
						},
					},
				},
			},
			want: map[string]string{
				"account_id":    "123456789012",
				"bucket":        "vpc_logs_test",
				"bucket_owner":  "test",
				"bucket_region": "us-east-1",
				"day":           "24",
				"key":           "my-bucket/AWSLogs/123456789012/vpcflowlogs/us-east-1/2022/01/24/123456789012_vpcflowlogs_us-east-1_fl-1234abcd_20180620T1620Z_fe123456.log.gz",
				"month":         "01",
				"region":        "us-east-1",
				"src":           "fl-1234abcd",
				"type":          FLOW_LOG_TYPE,
				"year":          "2022",
			},
			wantErr: false,
		},
		{
			name: "cloudtrail_digest_logs",
			args: args{
				record: events.S3EventRecord{
					AWSRegion: "us-east-1",
					S3: events.S3Entity{
						Bucket: events.S3Bucket{
							Name: "cloudtrail_digest_logs_test",
							OwnerIdentity: events.S3UserIdentity{
								PrincipalID: "test",
							},
						},
						Object: events.S3Object{
							Key: "my-bucket/AWSLogs/123456789012/CloudTrail-Digest/us-east-1/2022/01/24/123456789012_CloudTrail-Digest_us-east-1_20220124T0000Z_4jhzXFO2Jlvu2b3y.json.gz",
						},
					},
				},
			},
			want: map[string]string{
				"account_id":    "123456789012",
				"bucket":        "cloudtrail_digest_logs_test",
				"bucket_owner":  "test",
				"bucket_region": "us-east-1",
				"day":           "24",
				"key":           "my-bucket/AWSLogs/123456789012/CloudTrail-Digest/us-east-1/2022/01/24/123456789012_CloudTrail-Digest_us-east-1_20220124T0000Z_4jhzXFO2Jlvu2b3y.json.gz",
				"month":         "01",
				"region":        "us-east-1",
				"src":           "4jhzXFO2Jlvu2b3y",
				"type":          CLOUDTRAIL_DIGEST_LOG_TYPE,
				"year":          "2022",
			},
			wantErr: false,
		},
		{
			name: "cloudtrail_logs",
			args: args{
				record: events.S3EventRecord{
					AWSRegion: "us-east-1",
					S3: events.S3Entity{
						Bucket: events.S3Bucket{
							Name: "cloudtrail_logs_test",
							OwnerIdentity: events.S3UserIdentity{
								PrincipalID: "test",
							},
						},
						Object: events.S3Object{
							Key: "my-bucket/AWSLogs/123456789012/CloudTrail/us-east-1/2022/01/24/123456789012_CloudTrail_us-east-1_20220124T0000Z_4jhzXFO2Jlvu2b3y.json.gz",
						},
					},
				},
			},
			want: map[string]string{
				"account_id":    "123456789012",
				"bucket":        "cloudtrail_logs_test",
				"bucket_owner":  "test",
				"bucket_region": "us-east-1",
				"day":           "24",
				"key":           "my-bucket/AWSLogs/123456789012/CloudTrail/us-east-1/2022/01/24/123456789012_CloudTrail_us-east-1_20220124T0000Z_4jhzXFO2Jlvu2b3y.json.gz",
				"month":         "01",
				"region":        "us-east-1",
				"src":           "4jhzXFO2Jlvu2b3y",
				"type":          CLOUDTRAIL_LOG_TYPE,
				"year":          "2022",
			},
			wantErr: false,
		},
		{
			name: "s3_cloudfront",
			args: args{
				record: events.S3EventRecord{
					AWSRegion: "us-east-1",
					S3: events.S3Entity{
						Bucket: events.S3Bucket{
							Name: "cloudfront_logs_test",
							OwnerIdentity: events.S3UserIdentity{
								PrincipalID: "test",
							},
						},
						Object: events.S3Object{
							Key: "my/bucket/prefix/E2K2LNL5N3WR51.2022-07-18-12.a10a8496.gz",
						},
					},
				},
			},
			want: map[string]string{
				"bucket":        "cloudfront_logs_test",
				"bucket_owner":  "test",
				"bucket_region": "us-east-1",
				"day":           "18",
				"key":           "my/bucket/prefix/E2K2LNL5N3WR51.2022-07-18-12.a10a8496.gz",
				"month":         "07",
				"prefix":        "my/bucket/prefix",
				"src":           "E2K2LNL5N3WR51",
				"type":          CLOUDFRONT_LOG_TYPE,
				"year":          "2022",
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := getLabels(tt.args.record)
			if (err != nil) != tt.wantErr {
				t.Errorf("getLabels() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("getLabels() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_parseS3Log(t *testing.T) {
	type args struct {
		b         *batch
		labels    map[string]string
		obj       io.ReadCloser
		filename  string
		batchSize int
	}
	tests := []struct {
		name           string
		args           args
		wantErr        bool
		expectedLen    int
		expectedStream string
	}{
		{
			name: "vpcflowlogs",
			args: args{
				batchSize: 1024, // Set large enough we don't try and send to promtail
				filename:  "../testdata/vpcflowlog.log.gz",
				b: &batch{
					streams: map[string]*logproto.Stream{},
				},
				labels: map[string]string{
					"type":       FLOW_LOG_TYPE,
					"src":        "source",
					"account_id": "123456789",
				},
			},
			expectedLen:    1,
			expectedStream: `{__aws_log_type="s3_vpc_flow", __aws_s3_vpc_flow="source", __aws_s3_vpc_flow_owner="123456789"}`,
			wantErr:        false,
		},
		{
			name: "albaccesslogs",
			args: args{
				batchSize: 1024, // Set large enough we don't try and send to promtail
				filename:  "../testdata/albaccesslog.log.gz",
				b: &batch{
					streams: map[string]*logproto.Stream{},
				},
				labels: map[string]string{
					"type":       LB_LOG_TYPE,
					"src":        "source",
					"account_id": "123456789",
				},
			},
			expectedLen:    1,
			expectedStream: `{__aws_log_type="s3_lb", __aws_s3_lb="source", __aws_s3_lb_owner="123456789"}`,
			wantErr:        false,
		},
		{
			name: "cloudtraillogs",
			args: args{
				batchSize: 131072, // Set large enough we don't try and send to promtail
				filename:  "../testdata/cloudtrail-log-file.json.gz",
				b: &batch{
					streams: map[string]*logproto.Stream{},
				},
				labels: map[string]string{
					"type":       CLOUDTRAIL_LOG_TYPE,
					"src":        "source",
					"account_id": "123456789",
				},
			},
			expectedLen:    1,
			expectedStream: `{__aws_log_type="s3_cloudtrail", __aws_s3_cloudtrail="source", __aws_s3_cloudtrail_owner="123456789"}`,
			wantErr:        false,
		},
		{
			name: "cloudtrail_digest_logs",
			args: args{
				batchSize: 131072, // Set large enough we don't try and send to promtail
				filename:  "../testdata/cloudtrail-log-file.json.gz",
				b: &batch{
					streams: map[string]*logproto.Stream{},
				},
				labels: map[string]string{
					"type":       CLOUDTRAIL_DIGEST_LOG_TYPE,
					"src":        "source",
					"account_id": "123456789",
				},
			},
			expectedLen:    0,
			expectedStream: ``,
			wantErr:        false,
		},
		{
			name: "cloudfrontlogs",
			args: args{
				batchSize: 131072, // Set large enough we don't try and send to promtail
				filename:  "../testdata/cloudfront.log.gz",
				b: &batch{
					streams: map[string]*logproto.Stream{},
				},
				labels: map[string]string{
					"type":   CLOUDFRONT_LOG_TYPE,
					"src":    "DISTRIBUTIONID",
					"prefix": "path/to/file",
				},
			},
			expectedLen:    1,
			expectedStream: `{__aws_log_type="s3_cloudfront", __aws_s3_cloudfront="DISTRIBUTIONID", __aws_s3_cloudfront_owner="path/to/file"}`,
			wantErr:        false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var err error
			batchSize = tt.args.batchSize
			tt.args.obj, err = os.Open(tt.args.filename)
			if err != nil {
				t.Errorf("parseS3Log() failed to open test file: %s - %v", tt.args.filename, err)
			}
			if err := parseS3Log(context.Background(), tt.args.b, tt.args.labels, tt.args.obj); (err != nil) != tt.wantErr {
				t.Errorf("parseS3Log() error = %v, wantErr %v", err, tt.wantErr)
			}
			require.Len(t, tt.args.b.streams, tt.expectedLen)
			if tt.expectedStream != "" {
				stream, ok := tt.args.b.streams[tt.expectedStream]
				require.True(t, ok, "batch does not contain stream: %s", tt.expectedStream)
				require.NotNil(t, stream)
			}
		})
	}
}
