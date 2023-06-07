package baidubce

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v2"

	"github.com/grafana/dskit/flagext"
)

func Test_ConfigRedactsCredentials(t *testing.T) {
	underTest := BOSStorageConfig{
		AccessKeyID:     "access key id",
		SecretAccessKey: flagext.SecretWithValue("secret access key"),
	}

	output, err := yaml.Marshal(underTest)
	require.NoError(t, err)

	require.True(t, bytes.Contains(output, []byte("access key id")))
	require.False(t, bytes.Contains(output, []byte("secret access id")))
}
