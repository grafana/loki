package sketch

import (
	"bufio"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTopKMatrixProto(t *testing.T) {
	original, err := NewCMSTopK(100, 2048, 5)
	require.NoError(t, err)

	// Load topk with real world data set.
	f, err := os.Open("testdata/pg100.txt")
	require.NoError(t, err)
	scanner := bufio.NewScanner(f)
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
	require.Len(t, proto.Values[0].Topk.List, 100)

	deserialized, err := FromProto(proto)
	require.NoError(t, err)
	require.Len(t, deserialized, 1)

	require.Equal(t, original.sketch.counters, deserialized[0].topk.sketch.counters)
	require.Equal(t, *original.hll, *deserialized[0].topk.hll)
	require.Equal(t, uint64(100), deserialized[0].ts)
}
