package ibmcloud

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/IBM/ibm-cos-sdk-go/aws/credentials/ibmiam/token"
	"github.com/IBM/ibm-cos-sdk-go/aws/request"
	"github.com/IBM/ibm-cos-sdk-go/service/s3"
	"github.com/IBM/ibm-cos-sdk-go/service/s3/s3iface"
	"github.com/grafana/dskit/backoff"
	"github.com/grafana/dskit/flagext"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/storage/chunk/client"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/hedging"
)

var (
	bucket    = "test"
	timestamp = time.Now().Local()

	testData = map[string][]byte{
		"key-1": []byte("test data 1"),
		"key-2": []byte("test data 2"),
		"key-3": []byte("test data 3"),
	}

	testDeleteData = map[string][]byte{
		"key-1": []byte("test data 1")}

	testListData = map[string][]byte{
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

func newMockCosClient(data map[string][]byte) *mockCosClient {
	return &mockCosClient{
		data:   data,
		bucket: bucket,
	}
}

func (cosClient *mockCosClient) GetObjectWithContext(_ context.Context, input *s3.GetObjectInput, _ ...request.Option) (*s3.GetObjectOutput, error) {
	if *input.Bucket != cosClient.bucket {
		return &s3.GetObjectOutput{}, errMissingBucket
	}

	data, ok := cosClient.data[*input.Key]
	if !ok {
		return &s3.GetObjectOutput{}, errMissingKey
	}

	contentLength := int64(len(data))
	body := io.NopCloser(bytes.NewReader(data))
	output := s3.GetObjectOutput{
		Body:          body,
		ContentLength: &contentLength,
	}

	return &output, nil
}

func (cosClient *mockCosClient) PutObjectWithContext(_ context.Context, input *s3.PutObjectInput, _ ...request.Option) (*s3.PutObjectOutput, error) {
	if *input.Bucket != cosClient.bucket {
		return &s3.PutObjectOutput{}, errMissingBucket
	}

	dataBytes, err := io.ReadAll(input.Body)
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

func (cosClient *mockCosClient) DeleteObjectWithContext(_ context.Context, input *s3.DeleteObjectInput, _ ...request.Option) (*s3.DeleteObjectOutput, error) {
	if *input.Bucket != cosClient.bucket {
		return &s3.DeleteObjectOutput{}, errMissingBucket
	}

	if _, ok := cosClient.data[*input.Key]; !ok {
		return &s3.DeleteObjectOutput{}, errMissingObject
	}

	delete(cosClient.data, *input.Key)

	return &s3.DeleteObjectOutput{}, nil
}

func (cosClient *mockCosClient) ListObjectsV2WithContext(_ context.Context, input *s3.ListObjectsV2Input, _ ...request.Option) (*s3.ListObjectsV2Output, error) {
	if *input.Bucket != cosClient.bucket {
		return &s3.ListObjectsV2Output{}, errMissingBucket
	}

	var objects []*s3.Object
	if *input.Prefix != "key" {
		return &s3.ListObjectsV2Output{
			Contents: objects,
		}, nil
	}

	for object := range cosClient.data {
		key := object
		objects = append(objects, &s3.Object{
			Key:          &key,
			LastModified: &timestamp,
		})
	}

	return &s3.ListObjectsV2Output{
		Contents: objects,
	}, nil
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
			"Access key ID, Secret Access key, APIKey and CR token file path are empty",
			COSConfig{
				BucketNames:       "test",
				Endpoint:          "test",
				Region:            "dummy",
				ServiceInstanceID: "dummy",
				AuthEndpoint:      "dummy",
			},
			errors.Wrap(errInvalidCredentials, errCOSConfig),
		},
		{
			"Service Instance ID is empty",
			COSConfig{
				BucketNames:       "test",
				Endpoint:          "test",
				Region:            "dummy",
				AccessKeyID:       "dummy",
				SecretAccessKey:   flagext.SecretWithValue("dummy"),
				APIKey:            flagext.SecretWithValue("dummy"),
				ServiceInstanceID: "",
				AuthEndpoint:      "dummy",
			},
			errors.Wrap(errServiceInstanceID, errCOSConfig),
		},
		{
			"CR token file path with empty trusted profile name and ID",
			COSConfig{
				BucketNames:     "test",
				Endpoint:        "test",
				Region:          "dummy",
				AuthEndpoint:    "dummy",
				CRTokenFilePath: "dummy",
			},
			errors.Wrap(errTrustedProfile, errCOSConfig),
		},
		{
			"valid config with Access key ID and Secret Access key",
			COSConfig{
				BucketNames:     "test",
				Endpoint:        "test",
				Region:          "dummy",
				AccessKeyID:     "dummy",
				SecretAccessKey: flagext.SecretWithValue("dummy"),
			},
			nil,
		},
		{
			"valid config with APIKey",
			COSConfig{
				BucketNames:       "test",
				Endpoint:          "test",
				Region:            "dummy",
				APIKey:            flagext.SecretWithValue("dummy"),
				ServiceInstanceID: "dummy",
				AuthEndpoint:      "dummy",
			},
			nil,
		},
		{
			"valid config with trusted profile",
			COSConfig{
				BucketNames:        "test",
				Endpoint:           "test",
				Region:             "dummy",
				AuthEndpoint:       "dummy",
				CRTokenFilePath:    "dummy",
				TrustedProfileName: "test",
			},
			nil,
		},
	}
	for _, tt := range tests {
		if crTokenFilePath := tt.cosConfig.CRTokenFilePath; crTokenFilePath != "" {
			file, err := createTempFile(crTokenFilePath, "test-token")
			require.NoError(t, err)
			defer os.Remove(file.Name())

			tt.cosConfig.CRTokenFilePath = file.Name()
		}

		cosClient, err := NewCOSObjectClient(tt.cosConfig, hedging.Config{})
		if tt.expectedError != nil {
			require.Equal(t, tt.expectedError.Error(), err.Error())
			continue
		}

		require.NotNil(t, cosClient.cos)
		require.NotNil(t, cosClient.hedgedCOS)
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

		cosClient.hedgedCOS = newMockCosClient(testData)

		reader, _, err := cosClient.GetObject(context.Background(), tt.key)
		if tt.wantErr != nil {
			require.Equal(t, tt.wantErr.Error(), err.Error())
			continue
		}
		require.NoError(t, err)

		data, err := io.ReadAll(reader)
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

		cosClient.cos = newMockCosClient(testData)

		body := bytes.NewReader(tt.Body)

		err = cosClient.PutObject(context.Background(), tt.key, body)
		if tt.wantErr != nil {
			require.Equal(t, tt.wantErr.Error(), err.Error())
			continue
		}
		require.NoError(t, err)

		cosClient.hedgedCOS = newMockCosClient(testData)

		reader, _, err := cosClient.GetObject(context.Background(), tt.key)
		if tt.wantErr != nil {
			require.Equal(t, tt.wantErr.Error(), err.Error())
			continue
		}
		require.NoError(t, err)

		data, err := io.ReadAll(reader)
		require.NoError(t, err)
		require.Equal(t, tt.Body, data)
	}
}

func Test_DeleteObject(t *testing.T) {
	tests := []struct {
		key        string
		wantErr    error
		wantGetErr error
	}{
		{
			"key-1",
			errMissingObject,
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

		cosClient.cos = newMockCosClient(testDeleteData)

		err = cosClient.DeleteObject(context.Background(), tt.key)
		require.NoError(t, err)

		cosClient.hedgedCOS = newMockCosClient(testDeleteData)

		// call GetObject() for confirming the deleted object is no longer exist in bucket
		reader, _, err := cosClient.GetObject(context.Background(), tt.key)
		if tt.wantGetErr != nil {
			require.Equal(t, tt.wantGetErr.Error(), err.Error())
			continue
		}
		require.Nil(t, reader)

		err = cosClient.DeleteObject(context.Background(), tt.key)
		if tt.wantErr != nil {
			require.Equal(t, tt.wantErr.Error(), err.Error())
			continue
		}
	}
}

func Test_List(t *testing.T) {
	tests := []struct {
		prefix     string
		delimiter  string
		storageObj []client.StorageObject
		wantErr    error
	}{
		{
			"key",
			"-",
			[]client.StorageObject{{Key: "key-1", ModifiedAt: timestamp}, {Key: "key-2", ModifiedAt: timestamp}, {Key: "key-3", ModifiedAt: timestamp}},
			nil,
		},
		{
			"test",
			"/",
			nil,
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

		cosClient.cos = newMockCosClient(testListData)

		storageObj, _, err := cosClient.List(context.Background(), tt.prefix, tt.delimiter)
		if tt.wantErr != nil {
			require.Equal(t, tt.wantErr.Error(), err.Error())
			continue
		}
		require.NoError(t, err)

		sort.Slice(storageObj, func(i, j int) bool {
			return storageObj[i].Key < storageObj[j].Key
		})

		require.Equal(t, tt.storageObj, storageObj)
	}
}

func Test_APIKeyAuth(t *testing.T) {
	testToken := "test"
	tokenType := "Bearer"
	resp := "testGet"

	mockCosServer := mockCOSServer(testToken, tokenType, resp)
	defer mockCosServer.Close()

	mockAuthServer := mockAuthServer(testToken, tokenType)
	defer mockAuthServer.Close()

	cosConfig := COSConfig{
		BucketNames:       "dummy",
		Endpoint:          mockCosServer.URL,
		Region:            "dummy",
		APIKey:            flagext.SecretWithValue("dummy"),
		ServiceInstanceID: "test",
		ForcePathStyle:    true,
		AuthEndpoint:      mockAuthServer.URL,
		BackoffConfig: backoff.Config{
			MaxRetries: 1,
		},
	}

	cosClient, err := NewCOSObjectClient(cosConfig, hedging.Config{})
	require.NoError(t, err)

	reader, _, err := cosClient.GetObject(context.Background(), "key-1")
	require.NoError(t, err)

	data, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.Equal(t, resp, strings.Trim(string(data), "\n"))
}

func Test_TrustedProfileAuth(t *testing.T) {
	testToken := "test"
	tokenType := "Bearer"
	resp := "testGet"

	mockCosServer := mockCOSServer(testToken, tokenType, resp)
	defer mockCosServer.Close()

	mockAuthServer := mockAuthServer(testToken, tokenType)
	defer mockAuthServer.Close()

	file, err := createTempFile("crtoken", "test cr token")
	require.NoError(t, err)
	defer os.Remove(file.Name())

	cosConfig := COSConfig{
		BucketNames:        "dummy",
		Endpoint:           mockCosServer.URL,
		Region:             "dummy",
		ForcePathStyle:     true,
		AuthEndpoint:       mockAuthServer.URL,
		CRTokenFilePath:    file.Name(),
		TrustedProfileName: "test",
		BackoffConfig: backoff.Config{
			MaxRetries: 1,
		},
	}

	cosClient, err := NewCOSObjectClient(cosConfig, hedging.Config{})
	require.NoError(t, err)

	reader, _, err := cosClient.GetObject(context.Background(), "key-1")
	require.NoError(t, err)

	data, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.Equal(t, resp, strings.Trim(string(data), "\n"))
}

func mockCOSServer(accessToken, tokenType, resp string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != fmt.Sprintf("%s %s", tokenType, accessToken) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
		if _, err := fmt.Fprintln(w, resp); err != nil {
			fmt.Println("failed to write data", err)
			return
		}
	}))
}

func mockAuthServer(accessToken, tokenType string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		token := token.Token{
			AccessToken:  accessToken,
			RefreshToken: "test",
			TokenType:    tokenType,
			ExpiresIn:    int64((time.Hour * 24).Seconds()),
			Expiration:   time.Now().Add(time.Hour * 24).Unix(),
		}

		data, err := json.Marshal(token)
		if err != nil {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		w.WriteHeader(http.StatusOK)
		if _, err = w.Write(data); err != nil {
			fmt.Println("failed to write data", err)
			return
		}
	}))
}
