package ibmcloud

import (
	"bytes"
	"context"
	"io/ioutil"
	"testing"

	"github.com/IBM/ibm-cos-sdk-go/aws/request"
	"github.com/IBM/ibm-cos-sdk-go/service/s3"
	"github.com/IBM/ibm-cos-sdk-go/service/s3/s3iface"
	"github.com/grafana/dskit/backoff"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/loki/pkg/storage/chunk/client/hedging"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
)

var (
	bucket   = "test"
	testData = map[string][]byte{
		"key-1": []byte("test data 1"),
		"key-2": []byte("test data 2"),
		"key-3": []byte("test data 3"),
	}
	errMissingBucket = errors.New("bucket not found")
	errMissingKey    = errors.New("key not found")
	errMissingObject = errors.New("Object data not found")
)

type mockCosClient struct {
	s3iface.S3API
	data   map[string][]byte
	bucket string
}

func newMockCosClient() *mockCosClient {
	return &mockCosClient{
		data:   testData,
		bucket: bucket,
	}
}

func (cosClient *mockCosClient) GetObjectWithContext(ctx context.Context, input *s3.GetObjectInput, opts ...request.Option) (*s3.GetObjectOutput, error) {
	if *input.Bucket != cosClient.bucket {
		return &s3.GetObjectOutput{}, errMissingBucket
	}
	data, ok := cosClient.data[*input.Key]
	if !ok {
		return &s3.GetObjectOutput{}, errMissingKey
	}
	contentLength := int64(len(data))
	body := ioutil.NopCloser(bytes.NewReader(data))
	output := s3.GetObjectOutput{
		Body:          body,
		ContentLength: &contentLength,
	}

	return &output, nil
}

func (cosClient *mockCosClient) PutObjectWithContext(ctx context.Context, input *s3.PutObjectInput, opts ...request.Option) (*s3.PutObjectOutput, error) {
	if *input.Bucket != cosClient.bucket {
		return &s3.PutObjectOutput{}, errMissingBucket
	}
	dataBytes, err := ioutil.ReadAll(input.Body)
	if err != nil {
		return &s3.PutObjectOutput{}, errMissingObject
	}
	if string(dataBytes) == "" {
		return &s3.PutObjectOutput{}, errMissingObject
	}
	_, ok := cosClient.data[*input.Key]
	if !ok {
		cosClient.data[*input.Key] = dataBytes
	}
	return &s3.PutObjectOutput{}, nil
}

func Test_COSConfig(t *testing.T) {
	tests := []struct {
		name          string
		cosConfig     COSConfig
		expectedError error
	}{
		{
			"empty accessKeyID and secretAccessKey",
			COSConfig{
				BucketNames: "test",
				Endpoint:    "test",
				Region:      "dummy",
				AccessKeyID: "dummy",
			},
			errors.Wrap(errInvalidCOSHMACCredentials, errCOSConfig),
		},
		{
			"region is empty",
			COSConfig{
				BucketNames:     "test",
				Endpoint:        "test",
				Region:          "",
				AccessKeyID:     "dummy",
				SecretAccessKey: flagext.SecretWithValue("dummy"),
			},
			errors.Wrap(errEmptyRegion, errCOSConfig),
		},
		{
			"endpoint is empty",
			COSConfig{
				BucketNames:     "test",
				Endpoint:        "",
				Region:          "dummy",
				AccessKeyID:     "dummy",
				SecretAccessKey: flagext.SecretWithValue("dummy"),
			},
			errors.Wrap(errEmptyEndpoint, errCOSConfig),
		},
		{
			"bucket is empty",
			COSConfig{
				BucketNames:     "",
				Endpoint:        "",
				Region:          "dummy",
				AccessKeyID:     "dummy",
				SecretAccessKey: flagext.SecretWithValue("dummy"),
			},
			errEmptyBucket,
		},
		{
			"valid config",
			COSConfig{
				BucketNames:     "test",
				Endpoint:        "test",
				Region:          "dummy",
				AccessKeyID:     "dummy",
				SecretAccessKey: flagext.SecretWithValue("dummy"),
			},
			nil,
		},
	}
	for _, tt := range tests {
		cosClient, err := NewCOSObjectClient(tt.cosConfig, hedging.Config{})
		if tt.expectedError != nil {
			require.Equal(t, tt.expectedError.Error(), err.Error())
			continue
		}
		require.NotNil(t, cosClient.cos)
		require.NotNil(t, cosClient.hedgedS3)
		require.Equal(t, []string{tt.cosConfig.BucketNames}, cosClient.bucketNames)
	}
}

func Test_GetObject(t *testing.T) {
	tests := []struct {
		key       string
		wantBytes []byte
		wantErr   error
	}{
		{
			"key-1",
			[]byte("test data 1"),
			nil,
		},
		{
			"key-0",
			nil,
			errors.Wrap(errMissingKey, "failed to get cos object"),
		},
	}

	for _, tt := range tests {
		cosConfig := COSConfig{
			BucketNames:     bucket,
			Endpoint:        "test",
			Region:          "dummy",
			AccessKeyID:     "dummy",
			SecretAccessKey: flagext.SecretWithValue("dummy"),
			BackoffConfig: backoff.Config{
				MaxRetries: 1,
			},
		}

		cosClient, err := NewCOSObjectClient(cosConfig, hedging.Config{})
		require.NoError(t, err)

		cosClient.hedgedS3 = newMockCosClient()

		reader, _, err := cosClient.GetObject(context.Background(), tt.key)
		if tt.wantErr != nil {
			require.Equal(t, tt.wantErr.Error(), err.Error())
			continue
		}
		require.NoError(t, err)

		data, err := ioutil.ReadAll(reader)
		require.NoError(t, err)
		require.Equal(t, tt.wantBytes, data)
	}
}

func Test_PutObject(t *testing.T) {
	tests := []struct {
		key       string
		Body      []byte
		wantBytes []byte
		wantErr   error
	}{
		{
			"key-5",
			[]byte("test data 5"),
			[]byte("test data 5"),
			nil,
		},
	}

	for _, tt := range tests {
		cosConfig := COSConfig{
			BucketNames:     bucket,
			Endpoint:        "test",
			Region:          "dummy",
			AccessKeyID:     "dummy",
			SecretAccessKey: flagext.SecretWithValue("dummy"),
			BackoffConfig: backoff.Config{
				MaxRetries: 1,
			},
		}

		cosClient, err := NewCOSObjectClient(cosConfig, hedging.Config{})
		require.NoError(t, err)

		cosClient.cos = newMockCosClient()

		body := bytes.NewReader(tt.Body)

		err = cosClient.PutObject(context.Background(), tt.key, body)
		if tt.wantErr != nil {
			require.Equal(t, tt.wantErr.Error(), err.Error())
			continue
		}
		require.NoError(t, err)

		cosClient.hedgedS3 = newMockCosClient()

		reader, _, err := cosClient.GetObject(context.Background(), tt.key)
		if tt.wantErr != nil {
			require.Equal(t, tt.wantErr.Error(), err.Error())
			continue
		}
		require.NoError(t, err)

		data, err := ioutil.ReadAll(reader)
		require.NoError(t, err)
		require.Equal(t, tt.Body, data)
	}
}
