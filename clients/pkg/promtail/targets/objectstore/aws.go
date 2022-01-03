package objectstore

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/sqs"
	"github.com/aws/aws-sdk-go/service/sqs/sqsiface"
	"github.com/pkg/errors"

	"github.com/grafana/loki/pkg/storage/chunk"
	loki_aws "github.com/grafana/loki/pkg/storage/chunk/aws"
)

type SQSConfig struct {
	loki_aws.S3Config
	Queue string
}

type s3Client struct {
	queueURL *string
	svc      sqsiface.SQSAPI
}

type s3Results struct {
	Records []s3Record
}

type s3Record struct {
	S3        s3
	EventTime string
}

type s3 struct {
	Object object
}

type object struct {
	Key  string
	Size int64
}

var (
	defaultMaxRetries          = 5
	defaultMaxNumberOfMessages = int64(10)
	defaultVisibilityTimeout   = int64(20)
)

func newS3Client(cfg SQSConfig) (Client, error) {
	sess, err := buildSQSClient(cfg)
	if err != nil {
		return nil, err
	}
	svc := sqs.New(sess)
	urlResult, err := svc.GetQueueUrl(&sqs.GetQueueUrlInput{
		QueueName: &cfg.Queue,
	})
	if err != nil {
		return nil, err
	}
	queueURL := urlResult.QueueUrl
	return &s3Client{
		queueURL: queueURL,
		svc:      svc,
	}, nil
}

func buildSQSClient(cfg SQSConfig) (*session.Session, error) {
	var sqsConfig *aws.Config

	sqsConfig = sqsConfig.WithMaxRetries(defaultMaxRetries)

	if cfg.Region != "" {
		sqsConfig = sqsConfig.WithRegion(cfg.Region)
	}

	if cfg.AccessKeyID != "" && cfg.SecretAccessKey == "" ||
		cfg.AccessKeyID == "" && cfg.SecretAccessKey != "" {
		return nil, errors.New("must supply both an Access Key ID and Secret Access Key or neither")
	}

	if cfg.AccessKeyID != "" && cfg.SecretAccessKey != "" {
		creds := credentials.NewStaticCredentials(cfg.AccessKeyID, cfg.SecretAccessKey, "")
		sqsConfig = sqsConfig.WithCredentials(creds)
	}

	tlsConfig := &tls.Config{
		InsecureSkipVerify: cfg.HTTPConfig.InsecureSkipVerify,
	}

	if cfg.HTTPConfig.CAFile != "" {
		tlsConfig.RootCAs = x509.NewCertPool()
		data, err := os.ReadFile(cfg.HTTPConfig.CAFile)
		if err != nil {
			return nil, err
		}
		tlsConfig.RootCAs.AppendCertsFromPEM(data)
	}

	transport := http.RoundTripper(&http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
			DualStack: true,
		}).DialContext,
		MaxIdleConns:          100,
		IdleConnTimeout:       cfg.HTTPConfig.IdleConnTimeout,
		MaxIdleConnsPerHost:   100,
		TLSHandshakeTimeout:   3 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ResponseHeaderTimeout: cfg.HTTPConfig.ResponseHeaderTimeout,
		TLSClientConfig:       tlsConfig,
	})

	httpClient := &http.Client{
		Transport: transport,
	}

	sqsConfig = sqsConfig.WithHTTPClient(httpClient)

	sess, err := session.NewSession(sqsConfig)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create new s3 session")
	}
	return sess, nil
}

func (s3Client *s3Client) ReceiveMessage(timeout int64) ([]messageObject, error) {
	msgResult, err := s3Client.svc.ReceiveMessage(&sqs.ReceiveMessageInput{
		AttributeNames: []*string{
			aws.String(sqs.MessageSystemAttributeNameSentTimestamp),
		},
		MessageAttributeNames: []*string{
			aws.String(sqs.QueueAttributeNameAll),
		},
		QueueUrl:            s3Client.queueURL,
		MaxNumberOfMessages: aws.Int64(defaultMaxNumberOfMessages), // hard coded. We might take this as user input in future versions.
		WaitTimeSeconds:     aws.Int64(timeout),
		VisibilityTimeout:   aws.Int64(defaultVisibilityTimeout), // hard coded. We might take this as user input in future versions.
	})
	if err != nil {
		return nil, err
	}
	return s3Client.processMessage(msgResult)
}

func (s3Client *s3Client) processMessage(messages interface{}) ([]messageObject, error) {
	var messageObjects []messageObject
	m := messages.(*sqs.ReceiveMessageOutput)
	for _, message := range m.Messages {
		content := *message.Body
		var sqsResult s3Results
		err := json.Unmarshal([]byte(content), &sqsResult)
		if err != nil {
			return nil, err
		}
		if len(sqsResult.Records) < 1 {
			continue
		}

		result := sqsResult.Records[0]
		layout := "2006-01-02T15:04:05.000Z"
		t, err := time.Parse(layout, result.EventTime)

		if err != nil {
			return nil, err
		}
		messageObjects = append(messageObjects, messageObject{
			Object: chunk.StorageObject{
				Key:        result.S3.Object.Key,
				ModifiedAt: t,
			},
			Acknowledge: s3Client.acknowledgeMessage(message),
		})
	}

	return messageObjects, nil
}

func (s3Client *s3Client) acknowledgeMessage(message interface{}) ackMessage {
	return func() error {
		m := message.(*sqs.Message)
		_, err := s3Client.svc.DeleteMessage(&sqs.DeleteMessageInput{QueueUrl: s3Client.queueURL, ReceiptHandle: m.ReceiptHandle})
		if err != nil {
			return err
		}
		return nil
	}
}
