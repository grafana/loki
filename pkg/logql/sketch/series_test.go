package sketch

import (
	"bufio"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTopKMatrixProto(t *testing.T) {
	// Load topk with real world data set.
	const link = "https://www.gutenberg.org/cache/epub/100/pg100.txt"

	original, err := NewCMSTopK(100, 2048, 5)
	require.NoError(t, err)
	resp, err := http.Get(link)
	require.NoError(t, err)

	scanner := bufio.NewScanner(resp.Body)
	scanner.Split(bufio.ScanWords)
	for scanner.Scan() {
		s := scanner.Text()
		original.Observe(s)
	}

	series := TopKMatrix([]TopKVector{{ts: 100, topk: original}})

	// Roundtrip serialization
	proto, err := series.ToProto()
	require.NoError(t, err)
	require.Len(t, proto.Values, 1)
	require.Len(t, proto.Values[0].Topk.Cms.Counters, 2048*5)

	deserialized, err := FromProto(proto)
	require.NoError(t, err)
	require.Len(t, deserialized, 1)

	require.Equal(t, original.sketch.counters, deserialized[0].topk.sketch.counters)
	require.Equal(t, *original.hll, *deserialized[0].topk.hll)
	require.Equal(t, uint64(100), deserialized[0].ts)
}
