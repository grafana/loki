package alibaba

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_OssClient(t *testing.T) {

	ak := os.Getenv("loki_test_oss_ak")
	sk := os.Getenv("loki_test_oss_sk")
	endpoint := os.Getenv("loki_test_oss_endpoint")
	bucket := os.Getenv("loki_test_oss_ak")
	if ak == "" || sk == "" || endpoint == "" || bucket == "" {
		return
	}
	ossConfig := OssConfig{
		AccessKeyID:     ak,
		SecretAccessKey: sk,
		Endpoint:        endpoint,
		Bucket:          bucket,
	}

	client, err := NewOssObjectClient(context.Background(), ossConfig)
	require.NoError(t, err)

	existKey := "Prom/prometheus"
	_, _, err = client.GetObject(context.Background(), existKey)
	require.NoError(t, err)

	_, _, err = client.GetObject(context.Background(), "testNoExistKey")

	result := client.IsObjectNotFoundErr(err)

	require.Equal(t, true, result)

}
