package main

import (
	"context"
	"testing"

	"github.com/aws/aws-lambda-go/events"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/stretchr/testify/require"
)

func mockHandler(ctx context.Context, ev map[string]interface{}) error {
	return nil
}

func TestLambdaPromtail_SQSParseEventsNoMatch(t *testing.T) {
	tc := &events.SQSEvent{
		Records: []events.SQSMessage{
			{
				AWSRegion: "eu-west-3",
				MessageId: "someID",
				Body: `{
					"Type": "Notification",
					"MessageID": "14e9618f-4853-5042-8562-01a699ab45a77",
					"TopicArn": "arn:aws:sns:us-east-1:833333333333:ses-logging",
					"Message": "",
					"TimeStamp": "2025-02-14T12:57:11.812Z",
					"SignatureVersion": "1",
					"Signature": "NOTVALID==",
					"SigningCertURL": "https://sns.us-east-1.amazonaws.com/SimpleNotificationService-9c6465fa7f59f5cacd23014631ec1136.pem",
					"UnsubscribeURL": "https://sns.us-east-1.amazonaws.com/?Action=Unsubscribe&SubscriptionArn=arn:aws:sns:us-east-1:833333333333:ses-logging:04f5b10f-4d82-4f8f-a3dc-a8e155ac8a63"
				}`,
			},
		},
	}

	log := NewLogger("Info")
	mockBatch := &batch{
		streams: map[string]*logproto.Stream{},
	}
	ctx := context.TODO()

	err := parseSQSEvent(ctx, mockBatch, tc, log, mockHandler)
	require.Equal(t, 0, mockBatch.size) //The size of the batch is equal to the entire Mail object above
	require.Nil(t, err)

}

func TestLambdaPromtail_SQSParseEventsDelivery(t *testing.T) {
	tc := &events.SQSEvent{
		Records: []events.SQSMessage{
			{
				AWSRegion: "eu-west-3",
				MessageId: "someID",
				Body: `{
					"Type": "Notification",
					"MessageID": "14e9618f-4853-5042-8562-01a699ab45a77",
					"TopicArn": "arn:aws:sns:us-east-1:833333333333:ses-logging",
					"Message": "{\"NotificationType\":\"Delivery\",\"Mail\":{\"Timestamp\":\"2025-02-14T12:57:11.087Z\",\"Source\":\"SomethingElse <no-reply@somethingelse.io>\",\"SourceArn\":\"arn:aws:ses:us-east-1:833333333333:identity/somethingelse\",\"SourceIp\":\"8.8.8.8\",\"callerIdentity\":\"somethingelse\",\"SendingAccountId\":\"833333333333\",\"MessageId\":\"010001950488d4af-37dcfd0a-1ac4-4e29-9f95-5bc34246739e-000000\",\"Destination\":[\"no-reply@somethingelse.io\",\"test1@example.com\",\"test2@example.com\",\"test4@example.com\"],\"HeadersTruncated\":false,\"Headers\":[{\"name\":\"Date\",\"value\":\"Fri, 14 Feb 2025 12:57:11 +0000\"},{\"name\":\"To\",\"value\":\"no-reply@somethingelse.io\"},{\"name\":\"Bcc\",\"value\":\"test1@example.com, test3@example.com, test4@example.com, test2@example.com\"},{\"name\":\"From\",\"value\":\"SomethingElse <no-reply@somethingelse.io>\"},{\"name\":\"Subject\",\"value\":\"Waiting Time alert\"},{\"name\":\"Message-ID\",\"value\":\"<fbkz964rt04ra5ah2pi1g@somethingelse.io>\"},{\"name\":\"Content-ID\",\"value\":\"<4aurhl4rt04ra5ah2pgsi@9u2fxy4rt04ra5ah2pgti.local>\"},{\"name\":\"Content-Type\",\"value\":\"multipart/mixed; boundary=Boundary_6xru504rt04ra5ah2pgmm\"},{\"name\":\"MIME-Version\",\"value\":\"1.0 (Ruby MIME v0.4.4)\"}],\"CommonHeaders\":{\"From\":[\"SomethingElse <no-reply@somethingelse.io>\"],\"Date\":\"Fri, 14 Feb 2025 12:57:11 +0000\",\"To\":[\"no-reply@somethingelse.io\"],\"bcc\":[\"test1@example.com, test3@example.com, test4@example.com, test2@example.com\"],\"MessageId\":\"<fbkz964rt04ra5ah2pi1g@somethingelse.io>\",\"Subject\":\"Waiting Time alert\"}, \"Tags\": {\"foo\": \"bar\"}},\"Delivery\":{\"Timestamp\":\"2025-02-14T12:57:11.721Z\",\"ProcessingTimeMillis\":634,\"Recipients\":[\"no-reply@somethingelse.io\"],\"SmtpResponse\":\"250 2.0.0 OK  1739537831 af79cd13be357-7c07c622885si322193485a.23 - gsmtp\",\"RemoteMtaIp\":\"142.251.111.26\",\"ReportingMTA\":\"a9-110.smtp-out.amazonses.com\"}}",
					"TimeStamp": "2025-02-14T12:57:11.812Z",
					"SignatureVersion": "1",
					"Signature": "NOTVALID==",
					"SigningCertURL": "https://sns.us-east-1.amazonaws.com/SimpleNotificationService-9c6465fa7f59f5cacd23014631ec1136.pem",
					"UnsubscribeURL": "https://sns.us-east-1.amazonaws.com/?Action=Unsubscribe&SubscriptionArn=arn:aws:sns:us-east-1:833333333333:ses-logging:04f5b10f-4d82-4f8f-a3dc-a8e155ac8a63"
				}`,
			},
		},
	}

	mockBatch := &batch{
		streams: map[string]*logproto.Stream{},
	}

	ctx := context.TODO()
	log := NewLogger("Info")
	err := parseSQSEvent(ctx, mockBatch, tc, log, mockHandler)
	require.Nil(t, err)
	require.Equal(t, 1107, mockBatch.size) //The size of the batch is equal to the entire Mail object above
}

func TestLambdaPromtail_SQSParseEventsBounce(t *testing.T) {
	tc := &events.SQSEvent{
		Records: []events.SQSMessage{
			{
				AWSRegion: "eu-west-3",
				MessageId: "someID",
				Body: `{
					"Type": "Notification",
					"MessageID": "14e9618f-4853-5042-8562-01a699ab45a77",
					"TopicArn": "arn:aws:sns:us-east-1:833333333333:ses-logging",
					"Message": "{\"NotificationType\":\"Bounce\",\"Bounce\":{\"FeedbackId\":\"01000194ff989284-585da6bb-2576-42fe-9344-4e1830509fc0-000000\",\"BounceType\":\"Permanent\",\"BounceSubType\":\"OnAccountSuppressionList\",\"BouncedRecipients\":[{\"EmailAddress\":\"bouncedEmail1@example2.com\",\"Action\":\"failed\",\"Status\":\"5.1.1\",\"DiagnosticCode\":\"Amazon SES did not send the message to this address because it is on the suppression list for your account. For more information about removing addresses from the suppression list, see the Amazon SES Developer Guide at https://docs.aws.amazon.com/ses/latest/DeveloperGuide/sending-email-suppression-list.html\"}],\"Timestamp\":\"2025-02-13T13:56:16.000Z\",\"reportingMTA\":\"dns; amazonses.com\"},\"Mail\":{\"Timestamp\":\"2025-02-13T13:56:16.286Z\",\"Source\":\"SomethingElse \u003cno-reply@somethingelse.io\u003e\",\"SourceArn\":\"arn:aws:ses:us-east-1:833333333333:identity/somethingelse\",\"SourceIp\":\"54.204.98.19\",\"CallerIdentity\":\"somethingelse\",\"SendingAccountId\":\"833333333333\",\"MessageId\":\"01000194ff98911e-6997aea6-18fa-47f0-a52e-5bb426b2d147-000000\",\"Destination\":[\"no-reply@somethingelse.io\",\"test1@example2.com\",\"bouncedEmail1@example2.com\",\"test1@example3.com\",\"test2@example3.com\",\"test3@example3.com\",\"test4@example3.com\"],\"HeadersTruncated\":false,\"Headers\":[{\"name\":\"Date\",\"value\":\"Thu, 13 Feb 2025 13:56:16 +0000\"},{\"name\":\"To\",\"value\":\"no-reply@somethingelse.io\"},{\"name\":\"Bcc\",\"value\":\"test1@example2.com, bouncedEmail1@example2.com, test1@example3.com, test2@example3.com, test3@example3.com, test4@example3.com\"},{\"name\":\"From\",\"value\":\"SomethingElse \u003cno-reply@somethingelse.io\u003e\"},{\"name\":\"Subject\",\"value\":\"Defective Items Found in the WRS Drivers Vehicle Inspection Report\"},{\"name\":\"Message-ID\",\"value\":\"\u003c6rlbxuixbs4r9upueg53k@somethingelse.io\u003e\"},{\"name\":\"Content-ID\",\"value\":\"\u003cc7ykn6ixbs4r9upueg3yq@av5rr9ixbs4r9upueg3ze.local\u003e\"},{\"name\":\"Content-Type\",\"value\":\"multipart/mixed; boundary=Boundary_448fs7ixbs4r9upueg3tc\"},{\"name\":\"MIME-Version\",\"value\":\"1.0 (Ruby MIME v0.4.4)\"}],\"CommonHeaders\":{\"from\":[\"SomethingElse \u003cno-reply@somethingelse.io\u003e\"],\"date\":\"Thu, 13 Feb 2025 13:56:16 +0000\",\"to\":[\"no-reply@somethingelse.io\"],\"bcc\":[\"test1@example2.com\",\"bouncedEmail1@example2.com\",\"test1@example3.com\",\"test2@example3.com\",\"test3@example3.com\",\"test4@example3.com\"],\"MessageId\":\"\u003c6rlbxuixbs4r9upueg53k@somethingelse.io\u003e\",\"Subject\":\"Defective Items Found in the WRS Drivers Vehicle Inspection Report\"}}}",
					"TimeStamp": "2025-02-14T12:57:11.812Z",
					"SignatureVersion": "1",
					"Signature": "NOTVALID==",
					"SigningCertURL": "https://sns.us-east-1.amazonaws.com/SimpleNotificationService-9c6465fa7f59f5cacd23014631ec1136.pem",
					"UnsubscribeURL": "https://sns.us-east-1.amazonaws.com/?Action=Unsubscribe&SubscriptionArn=arn:aws:sns:us-east-1:833333333333:ses-logging:04f5b10f-4d82-4f8f-a3dc-a8e155ac8a63"
				}`,
			},
		},
	}

	mockBatch := &batch{
		streams: map[string]*logproto.Stream{},
	}

	ctx := context.TODO()
	log := NewLogger("Info")
	err := parseSQSEvent(ctx, mockBatch, tc, log, mockHandler)
	require.Nil(t, err)
	require.Equal(t, 1748, mockBatch.size) //The size of the batch is equal to the entire Mail object above
}

func TestLambdaPromtail_SQSParseEventsComplaint(t *testing.T) {
	tc := &events.SQSEvent{
		Records: []events.SQSMessage{
			{
				AWSRegion: "eu-west-3",
				MessageId: "someID",
				Body: `{
					"Type": "Notification",
					"MessageID": "14e9618f-4853-5042-8562-01a699ab45a77",
					"TopicArn": "arn:aws:sns:us-east-1:833333333333:ses-logging",
					"Message": "{\"NotificationType\":\"Complaint\",\"Complaint\":{\"UserAgent\":\"Some Client 123456\",\"Type\":\"User complaining\",\"FeedbackId\":\"01000194ff989284-585da6bb-2576-42fe-9344-4e1830509fc0-000000\",\"ComplainedRecipients\":[{\"EmailAddress\":\"wining@smallestfiddle.com\"}],\"Timestamp\":\"2025-02-13T13:56:16.000Z\",\"ArrivalDate\":\"2025-02-13T13:56:16.000Z\"},\"Mail\":{\"Timestamp\":\"2025-02-13T13:56:16.286Z\",\"Source\":\"SomethingElse \u003cno-reply@somethingelse.io\u003e\",\"SourceArn\":\"arn:aws:ses:us-east-1:833333333333:identity/somethingelse\",\"SourceIp\":\"54.204.98.19\",\"CallerIdentity\":\"somethingelse\",\"SendingAccountId\":\"833333333333\",\"MessageId\":\"01000194ff98911e-6997aea6-18fa-47f0-a52e-5bb426b2d147-000000\",\"Destination\":[\"no-reply@somethingelse.io\",\"test1@example2.com\",\"bouncedEmail1@example2.com\",\"test1@example3.com\",\"test2@example3.com\",\"test3@example3.com\",\"test4@example3.com\"],\"HeadersTruncated\":false,\"Headers\":[{\"name\":\"Date\",\"value\":\"Thu, 13 Feb 2025 13:56:16 +0000\"},{\"name\":\"To\",\"value\":\"no-reply@somethingelse.io\"},{\"name\":\"Bcc\",\"value\":\"test1@example2.com, bouncedEmail1@example2.com, test1@example3.com, test2@example3.com, test3@example3.com, test4@example3.com\"},{\"name\":\"From\",\"value\":\"SomethingElse \u003cno-reply@somethingelse.io\u003e\"},{\"name\":\"Subject\",\"value\":\"Defective Items Found in the WRS Drivers Vehicle Inspection Report\"},{\"name\":\"Message-ID\",\"value\":\"\u003c6rlbxuixbs4r9upueg53k@somethingelse.io\u003e\"},{\"name\":\"Content-ID\",\"value\":\"\u003cc7ykn6ixbs4r9upueg3yq@av5rr9ixbs4r9upueg3ze.local\u003e\"},{\"name\":\"Content-Type\",\"value\":\"multipart/mixed; boundary=Boundary_448fs7ixbs4r9upueg3tc\"},{\"name\":\"MIME-Version\",\"value\":\"1.0 (Ruby MIME v0.4.4)\"}],\"CommonHeaders\":{\"from\":[\"SomethingElse \u003cno-reply@somethingelse.io\u003e\"],\"date\":\"Thu, 13 Feb 2025 13:56:16 +0000\",\"to\":[\"no-reply@somethingelse.io\"],\"bcc\":[\"test1@example2.com\",\"bouncedEmail1@example2.com\",\"test1@example3.com\",\"test2@example3.com\",\"test3@example3.com\",\"test4@example3.com\"],\"MessageId\":\"\u003c6rlbxuixbs4r9upueg53k@somethingelse.io\u003e\",\"Subject\":\"I need off of this\"}}}",
					"TimeStamp": "2025-02-14T12:57:11.812Z",
					"SignatureVersion": "1",
					"Signature": "NOTVALID==",
					"SigningCertURL": "https://sns.us-east-1.amazonaws.com/SimpleNotificationService-9c6465fa7f59f5cacd23014631ec1136.pem",
					"UnsubscribeURL": "https://sns.us-east-1.amazonaws.com/?Action=Unsubscribe&SubscriptionArn=arn:aws:sns:us-east-1:833333333333:ses-logging:04f5b10f-4d82-4f8f-a3dc-a8e155ac8a63"
				}`,
			},
		},
	}

	mockBatch := &batch{
		streams: map[string]*logproto.Stream{},
	}

	ctx := context.TODO()
	log := NewLogger("Info")
	err := parseSQSEvent(ctx, mockBatch, tc, log, mockHandler)
	require.Nil(t, err)
	require.Equal(t, 1332, mockBatch.size) //The size of the batch is equal to the entire Mail object above
}
