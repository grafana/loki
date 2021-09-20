package configstore

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCodec(t *testing.T) {
	exampleConfig := `name: 'test'
host_filter: false
scrape_configs:
  - job_name: process-1
    static_configs:
      - targets: ['process-1:80']
        labels:
          cluster: 'local'
          origin: 'agent'`

	c := &yamlCodec{}
	bb, err := c.Encode(exampleConfig)
	require.NoError(t, err)

	out, err := c.Decode(bb)
	require.NoError(t, err)
	require.Equal(t, exampleConfig, out)
}

// TestCodec_Decode_Nil makes sure that if Decode is called with an empty value,
// which may happen when a key is deleted, that no error occurs and instead an
// nil value is returned.
func TestCodec_Decode_Nil(t *testing.T) {
	c := &yamlCodec{}

	input := [][]byte{nil, make([]byte, 0)}
	for _, bb := range input {
		out, err := c.Decode(bb)
		require.Nil(t, err)
		require.Nil(t, out)
	}
}
