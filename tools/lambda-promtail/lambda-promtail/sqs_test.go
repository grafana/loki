package main

import (
	"testing"

	"github.com/aws/aws-lambda-go/events"
)

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
