package ingester

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/chunkenc"
	"github.com/grafana/loki/pkg/logproto"
)

func fillChunk(t testing.TB, c chunkenc.Chunk) {
	t.Helper()
	var i int64
	entry := &logproto.Entry{
		Timestamp: time.Unix(0, 0),
		Line:      "entry for line 0",
	}

	for c.SpaceFor(entry) {
		require.NoError(t, c.Append(entry))
		i++
		entry.Timestamp = time.Unix(0, i)
		entry.Line = fmt.Sprintf("entry for line %d", i)
	}
}

func dummyConf() *Config {
	var conf Config
	conf.BlockSize = 256 * 1024
	conf.TargetChunkSize = 1500 * 1024

	return &conf
}
