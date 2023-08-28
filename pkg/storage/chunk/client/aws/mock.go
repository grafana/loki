package aws

import (
	"bytes"
	"fmt"
	"io"
	"sync"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3iface"
)

type mockS3 struct {
	s3iface.S3API
	sync.RWMutex
	objects map[string][]byte
}

func newMockS3() *mockS3 {
	return &mockS3{
		objects: map[string][]byte{},
	}
}

func (m *mockS3) PutObjectWithContext(_ aws.Context, req *s3.PutObjectInput, _ ...request.Option) (*s3.PutObjectOutput, error) {
	m.Lock()
	defer m.Unlock()

	buf, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}

	m.objects[*req.Key] = buf
	return &s3.PutObjectOutput{}, nil
}

func (m *mockS3) GetObjectWithContext(_ aws.Context, req *s3.GetObjectInput, _ ...request.Option) (*s3.GetObjectOutput, error) {
	m.RLock()
	defer m.RUnlock()

	buf, ok := m.objects[*req.Key]
	if !ok {
		return nil, fmt.Errorf("Not found")
	}

	return &s3.GetObjectOutput{
		Body: io.NopCloser(bytes.NewReader(buf)),
	}, nil
}
