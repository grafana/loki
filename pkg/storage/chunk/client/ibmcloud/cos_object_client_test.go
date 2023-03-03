package ibmcloud

import (
	"testing"

	"github.com/grafana/dskit/flagext"
	"github.com/grafana/loki/pkg/storage/chunk/client/hedging"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
)

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
