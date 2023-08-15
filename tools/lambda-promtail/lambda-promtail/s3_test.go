package main

import (
	"context"
	"io"
	"os"
	"reflect"
	"testing"

	"github.com/aws/aws-lambda-go/events"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/logproto"
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
			name: "organization_cloudtrail_logs",
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
							Key: "my-bucket/AWSLogs/o-test123456/123456789012/CloudTrail/us-east-1/2022/01/24/123456789012_CloudTrail_us-east-1_20220124T0000Z_4jhzXFO2Jlvu2b3y.json.gz",
						},
					},
				},
			},
			want: map[string]string{
				"account_id":      "123456789012",
				"bucket":          "cloudtrail_logs_test",
				"bucket_owner":    "test",
				"bucket_region":   "us-east-1",
				"day":             "24",
				"key":             "my-bucket/AWSLogs/o-test123456/123456789012/CloudTrail/us-east-1/2022/01/24/123456789012_CloudTrail_us-east-1_20220124T0000Z_4jhzXFO2Jlvu2b3y.json.gz",
				"month":           "01",
				"organization_id": "o-test123456",
				"region":          "us-east-1",
				"src":             "4jhzXFO2Jlvu2b3y",
				"type":            CLOUDTRAIL_LOG_TYPE,
				"year":            "2022",
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
		{
			name: "missing_type",
			args: args{
				record: events.S3EventRecord{
					AWSRegion: "us-east-1",
					S3: events.S3Entity{
						Bucket: events.S3Bucket{
							Name: "missing_type",
							OwnerIdentity: events.S3UserIdentity{
								PrincipalID: "test",
							},
						},
						Object: events.S3Object{
							Key: "some-object.json",
						},
					},
				},
			},
			want: map[string]string{
				"bucket":        "missing_type",
				"bucket_owner":  "test",
				"bucket_region": "us-east-1",
				"key":           "some-object.json",
			},
			wantErr: true,
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
		{
			name: "missing_parser",
			args: args{
				batchSize: 131072, // Set large enough we don't try and send to promtail
				filename:  "../testdata/kinesis-event.json",
				b: &batch{
					streams: map[string]*logproto.Stream{},
				},
				labels: map[string]string{
					"bucket":        "missing_parser",
					"bucket_owner":  "test",
					"bucket_region": "us-east-1",
					"key":           "some-object.json",
					"type":          "",
				},
			},
			expectedLen:    0,
			expectedStream: "",
			wantErr:        true,
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

func TestStringToRawEvent(t *testing.T) {
	tc := &events.SQSEvent{
		Records: []events.SQSMessage{
			{
				AWSRegion: "eu-west-3",
				MessageId: "someID",
				Body: `{
					"Records": [
						{
							"eventVersion": "2.1",
							"eventSource": "aws:s3",
							"awsRegion": "us-east-1",
							"eventTime": "2023-01-18T01:52:46.432Z",
							"eventName": "ObjectCreated:Put",
							"userIdentity": {
								"principalId": "AWS:SOMEID:AWS.INTERNAL.URL"
							},
							"requestParameters": {
								"sourceIPAddress": "172.15.15.15"
							},
							"responseElements": {
								"x-amz-request-id": "SOMEID",
								"x-amz-id-2": "SOMEID"
							},
							"s3": {
								"s3SchemaVersion": "1.0",
								"configurationId": "tf-s3-queue-SOMEID",
								"bucket": {
									"name": "some-bucket-name",
									"ownerIdentity": {
										"principalId": "SOMEID"

									},
									"arn": "arn:aws:s3:::SOME-BUCKET-ARN"
								},
								"object": {
									"key": "SOME-PREFIX/AWSLogs/ACCOUNT-ID/vpcflowlogs/us-east-1/2023/01/18/end-of-filename.log.gz",
									"size": 1042577,
									"eTag": "SOMEID",
									"versionId": "SOMEID",
									"sequencer": "SOMEID"
								}
							}
						}
					]
				}`,
			},
		},
	}
	for _, record := range tc.Records {
		event, err := stringToRawEvent(record.Body)
		if err != nil {
			t.Error(err)
		}
		_, err = checkEventType(event)
		if err != nil {
			t.Error(err)
		}
	}
}

func TestProcessSNSEvent(t *testing.T) {
	evt := &events.SNSEvent{
		Records: []events.SNSEventRecord{
			{
				SNS: events.SNSEntity{
					Message: `{"pass": "pass"}`,
				},
			},
		},
	}

	ctx := context.Background()
	handlerCalled := false

	err := processSNSEvent(ctx, evt, func(ctx context.Context, ev map[string]interface{}) error {
		handlerCalled = true
		require.Equal(t, map[string]interface{}{"pass": "pass"}, ev)
		return nil
	})
	require.Nil(t, err)
	require.True(t, handlerCalled)
}

func TestProcessSQSEvent(t *testing.T) {
	evt := &events.SQSEvent{
		Records: []events.SQSMessage{
			{
				Body: `{"pass": "pass"}`,
			},
		},
	}

	ctx := context.Background()
	handlerCalled := false

	err := processSQSEvent(ctx, evt, func(ctx context.Context, ev map[string]interface{}) error {
		handlerCalled = true
		require.Equal(t, map[string]interface{}{"pass": "pass"}, ev)
		return nil
	})
	require.Nil(t, err)
	require.True(t, handlerCalled)
}
