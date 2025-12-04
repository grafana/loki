package huaweicloud

import (
	"bytes"
	"testing"

	"github.com/huaweicloud/huaweicloud-sdk-go-obs/obs"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v2"

	"github.com/grafana/dskit/flagext"
)

func Test_ConfigRedactsCredentials(t *testing.T) {
	underTest := ObsConfig{
		AccessKeyID:     "access key id",
		SecretAccessKey: flagext.SecretWithValue("secret access key"),
	}

	output, err := yaml.Marshal(underTest)
	require.NoError(t, err)

	require.True(t, bytes.Contains(output, []byte("access key id")))
	require.False(t, bytes.Contains(output, []byte("secret access key")))
}

func TestIsObjectNotFoundErr(t *testing.T) {
	tests := []struct {
		err      error
		expected bool
		name     string
	}{
		{
			name: "NoSuchKey error code is recognized as object not found",
			err: obs.ObsError{
				Code: NoSuchKeyErr,
			},
			expected: true,
		},
		{
			name:     "Nil error isnt recognized as object not found",
			err:      nil,
			expected: false,
		},
		{
			name: "Other error code isnt recognized as object not found",
			err: obs.ObsError{
				Code: "AccessDenied",
			},
			expected: false,
		},
		{
			name:     "Non-ObsError isnt recognized as object not found",
			err:      errors.New("some other error"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &ObsObjectClient{}
			require.Equal(t, tt.expected, client.IsObjectNotFoundErr(tt.err))
		})
	}
}
