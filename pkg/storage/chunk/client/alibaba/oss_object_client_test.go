package alibaba

import (
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v2"
)

func TestOSSConfig_UnmarshalYAML(t *testing.T) {
	in := []byte(`bucket: foobar
endpoint: oss.test
`)

	dst := &OssConfig{}
	require.NoError(t, yaml.UnmarshalStrict(in, dst))
	require.Equal(t, "foobar", dst.Bucket)
	require.Equal(t, "oss.test", dst.Endpoint)
}
