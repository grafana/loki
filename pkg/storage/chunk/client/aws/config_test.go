package aws

import (
	"net/url"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/grafana/dskit/flagext"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/storage/chunk/client/hedging"
)

func TestCredentialsFromURL(t *testing.T) {
	tests := []struct {
		name           string
		urlStr         string
		expectedKey    string
		expectedSecret string
	}{
		{
			name:           "URL with username and password",
			urlStr:         "s3://mykey:mysecret@us-east-1",
			expectedKey:    "mykey",
			expectedSecret: "mysecret",
		},
		{
			name:           "URL with only username",
			urlStr:         "s3://mykey@us-east-1",
			expectedKey:    "mykey",
			expectedSecret: "",
		},
		{
			name:           "URL without credentials (should not error)",
			urlStr:         "s3://us-east-1",
			expectedKey:    "",
			expectedSecret: "",
		},
		{
			name:           "URL with endpoint without credentials",
			urlStr:         "s3://s3.amazonaws.com",
			expectedKey:    "",
			expectedSecret: "",
		},
		{
			name:           "URL with credentials and bucket",
			urlStr:         "s3://key:secret@us-east-1/bucket",
			expectedKey:    "key",
			expectedSecret: "secret",
		},
		{
			name:           "URL with credentials and bucket",
			urlStr:         "s3://key:secret@us-east-1/bucket",
			expectedKey:    "key",
			expectedSecret: "secret",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u, err := url.Parse(tt.urlStr)
			require.NoError(t, err)

			key, secret := credentialsFromURL(u)
			require.Equal(t, tt.expectedKey, key)
			require.Equal(t, tt.expectedSecret, secret)
		})
	}
}

func urlValue(s string) flagext.URLValue {
	url := &flagext.URLValue{}
	_ = url.Set(s)
	return *url
}

func TestS3ClientOptions(t *testing.T) {

	t.Run("s3 schema with region as hostname", func(t *testing.T) {
		cfg := S3Config{
			S3: urlValue("s3://us-east-0/bucket"),
		}
		fn, _ := s3ClientConfigFunc(cfg, hedging.Config{}, false)
		opts := s3.Options{}
		fn(&opts)

		require.Equal(t, "us-east-0", opts.Region)
		require.Nil(t, opts.BaseEndpoint)
	})

	t.Run("https schema with region as hostname", func(t *testing.T) {
		cfg := S3Config{
			S3: urlValue("https://us-east-0/bucket"),
		}
		fn, _ := s3ClientConfigFunc(cfg, hedging.Config{}, false)
		opts := s3.Options{}
		fn(&opts)

		require.Equal(t, "us-east-0", opts.Region)
		require.Nil(t, opts.BaseEndpoint)
	})

	t.Run("s3 schema with endpoint hostname", func(t *testing.T) {
		cfg := S3Config{
			S3: urlValue("s3://s3.us-east-0.amazonaws.com/bucket"),
		}
		fn, _ := s3ClientConfigFunc(cfg, hedging.Config{}, false)
		opts := s3.Options{}
		fn(&opts)

		require.Equal(t, InvalidAWSRegion, opts.Region) // it is still required to set region explicitly
		require.Equal(t, "http://s3.us-east-0.amazonaws.com", *opts.BaseEndpoint)
	})

	t.Run("https schema with endpoint hostname", func(t *testing.T) {
		cfg := S3Config{
			S3: urlValue("https://s3.us-east-0.amazonaws.com/bucket"),
		}
		fn, _ := s3ClientConfigFunc(cfg, hedging.Config{}, false)
		opts := s3.Options{}
		fn(&opts)

		require.Equal(t, InvalidAWSRegion, opts.Region) // it is still required to set region explicitly
		require.Equal(t, "https://s3.us-east-0.amazonaws.com", *opts.BaseEndpoint)
	})

	t.Run("access key and secret in url", func(t *testing.T) {
		cfg := S3Config{
			S3: urlValue("s3://accesskey:secret@us-east-0/bucket"),
		}
		fn, _ := s3ClientConfigFunc(cfg, hedging.Config{}, false)
		opts := s3.Options{}
		fn(&opts)

		cred, err := opts.Credentials.Retrieve(t.Context())
		require.NoError(t, err)
		require.Equal(t, "accesskey", cred.AccessKeyID)
		require.Equal(t, "secret", cred.SecretAccessKey)
	})

	t.Run("fields override url config", func(t *testing.T) {
		cfg := S3Config{
			S3:              urlValue("s3://accesskey:secret@us-east-0/bucket"),
			AccessKeyID:     "loki",
			SecretAccessKey: flagext.SecretWithValue("s3cr3t"),
			Endpoint:        "s3.eu-south-0.amazonaws.com",
		}
		fn, _ := s3ClientConfigFunc(cfg, hedging.Config{}, false)
		opts := s3.Options{}
		fn(&opts)

		cred, err := opts.Credentials.Retrieve(t.Context())
		require.NoError(t, err)
		require.Equal(t, "loki", cred.AccessKeyID)
		require.Equal(t, "s3cr3t", cred.SecretAccessKey)
		require.Equal(t, "https://s3.eu-south-0.amazonaws.com", *opts.BaseEndpoint)
	})

	t.Run("insecure=false flag uses https for endpoint", func(t *testing.T) {
		cfg := S3Config{
			Endpoint: "minio:9100",
			Insecure: false,
		}
		fn, _ := s3ClientConfigFunc(cfg, hedging.Config{}, false)
		opts := s3.Options{}
		fn(&opts)

		require.Equal(t, "https://minio:9100", *opts.BaseEndpoint)
	})

	t.Run("insecure=true flag uses http for endpoint", func(t *testing.T) {
		cfg := S3Config{
			Endpoint: "minio:9100",
			Insecure: true,
		}
		fn, _ := s3ClientConfigFunc(cfg, hedging.Config{}, false)
		opts := s3.Options{}
		fn(&opts)

		require.Equal(t, "http://minio:9100", *opts.BaseEndpoint)
	})

	t.Run("region is set to invalid when url is not present", func(t *testing.T) {
		cfg := S3Config{}
		fn, _ := s3ClientConfigFunc(cfg, hedging.Config{}, false)
		opts := s3.Options{}
		fn(&opts)

		require.Equal(t, InvalidAWSRegion, opts.Region)
	})

}
