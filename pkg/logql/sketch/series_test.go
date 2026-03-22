package sketch

import (
	"bufio"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTopKMatrixProto(t *testing.T) {
	original, err := newCMSTopK(100, 2048, 5)
	require.NoError(t, err)

	// Load topk with real world data set.
	f, err := os.Open("testdata/shakspeare.txt")
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

	deserialized, err := TopKMatrixFromProto(proto)
	require.NoError(t, err)
	require.Len(t, deserialized, 1)

	require.Equal(t, original.sketch.Counters, deserialized[0].topk.sketch.Counters)
	require.Equal(t, *original.hll, *deserialized[0].topk.hll)
	oCardinality, _ := original.Cardinality()
	fmt.Println("ocardinality: ", oCardinality)
	dCardinality, _ := deserialized[0].topk.Cardinality()
	fmt.Println("dcardinality: ", dCardinality)

	require.Equal(t, oCardinality, dCardinality)
	require.Equal(t, uint64(100), deserialized[0].ts)
}

func TestTopKMatrixProtoMerge(t *testing.T) {
	original, err := newCMSTopK(100, 2048, 5)
	require.NoError(t, err)

	// Load topk with real world data set.
	f, err := os.Open("testdata/shakspeare.txt")
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

	deserialized, err := TopKMatrixFromProto(proto)
	require.NoError(t, err)
	require.Len(t, deserialized, 1)

	require.Equal(t, original.sketch.Counters, deserialized[0].topk.sketch.Counters)
	require.Equal(t, *original.hll, *deserialized[0].topk.hll)
	oCardinality, _ := original.Cardinality()
	dCardinality, _ := deserialized[0].topk.Cardinality()
	require.Equal(t, oCardinality, dCardinality)
	require.Equal(t, uint64(100), deserialized[0].ts)
}
