package aws

import (
	"net/url"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAWSConfigFromURL(t *testing.T) {
	for i, tc := range []struct {
		url            string
		expectedKey    string
		expectedSecret string
		expectedRegion string
		expectedEp     string
	}{
		{
			"s3://abc:123@s3.default.svc.cluster.local:4569",
			"abc",
			"123",
			"dummy",
			"http://s3.default.svc.cluster.local:4569",
		},
		{
			"dynamodb://user:pass@dynamodb.default.svc.cluster.local:8000/cortex",
			"user",
			"pass",
			"dummy",
			"http://dynamodb.default.svc.cluster.local:8000",
		},
		{
			// No credentials.
			"s3://s3.default.svc.cluster.local:4569",
			"",
			"",
			"dummy",
			"http://s3.default.svc.cluster.local:4569",
		},
		{
			"s3://keyWithEscapedSlashAtTheEnd%2F:%24%2C%26%2C%2B%2C%27%2C%2F%2C%3A%2C%3B%2C%3D%2C%3F%2C%40@eu-west-2/bucket1",
			"keyWithEscapedSlashAtTheEnd/",
			"$,&,+,',/,:,;,=,?,@",
			"eu-west-2",
			"",
		},
	} {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			parsedURL, err := url.Parse(tc.url)
			require.NoError(t, err)

			cfg, err := ConfigFromURL(parsedURL)
			require.NoError(t, err)

			if cfg.Credentials == nil {
				assert.Equal(t, "", tc.expectedKey)
				assert.Equal(t, "", tc.expectedSecret)
			} else {
				val, err := cfg.Credentials.Get()
				require.NoError(t, err)
				assert.Equal(t, tc.expectedKey, val.AccessKeyID)
				assert.Equal(t, tc.expectedSecret, val.SecretAccessKey)
			}

			require.NotNil(t, cfg.Region)
			assert.Equal(t, tc.expectedRegion, *cfg.Region)

			if tc.expectedEp != "" {
				require.NotNil(t, cfg.Endpoint)
				assert.Equal(t, tc.expectedEp, *cfg.Endpoint)
			}
		})
	}
}
