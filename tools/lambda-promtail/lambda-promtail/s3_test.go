package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"testing"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/stretchr/testify/assert"

	"github.com/grafana/loki/pkg/logproto"
)

type MockS3Client struct {
	obj s3.GetObjectOutput
}

func NewMockS3Client(s string) *MockS3Client {
	var b bytes.Buffer
	w := gzip.NewWriter(&b)
	w.Write([]byte("hello, world\n"))
	w.Close()
	reader := bytes.NewReader(b.Bytes())
	return &MockS3Client{
		obj: s3.GetObjectOutput{
			Body: io.NopCloser(reader),
		},
	}
}

func (c MockS3Client) GetObject(ctx context.Context, input *s3.GetObjectInput) (*s3.GetObjectOutput, error) {
	return &c.obj, nil
}

func TestParseS3Log(t *testing.T) {
	// if no timestamp is found we'll asign time.Now(), so later in this
	// test we can check that the timestamp is > time.Now()
	now := time.Now()
	s3Clients = make(map[string]S3Client)
	b := &MockBatch{
		streams: map[string]*logproto.Stream{},
	}
	s3Clients["us-central-0"] = NewMockS3Client("this is a test")
	a := events.S3Entity{
		Bucket: events.S3Bucket{
			Name:          "asdf",
			OwnerIdentity: events.S3UserIdentity{PrincipalID: "hello"},
			Arn:           "12345",
		},
		Object: events.S3Object{Key: "qwerty"},
	}
	parseS3Record(context.Background(),
		events.S3EventRecord{
			S3:        a,
			AWSRegion: "us-central-0",
		},
		b)
	for _, a := range b.streams {
		assert.True(t, a.Entries[0].Timestamp.After(now), "")
	}
}
