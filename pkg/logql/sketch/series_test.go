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
		original.Observe(s, 1)
	}

	series := TopKMatrix([]TopKVector{{TS: 100, Topk: original}})

	// Roundtrip serialization
	proto, err := series.ToProto()
	require.NoError(t, err)
	require.Len(t, proto.Values, 1)
	require.Len(t, proto.Values[0].Topk.Cms.Counters, 2048*5)
	require.Len(t, proto.Values[0].Topk.Results[0].List, 100, "expected length of 100 but was actually %d", len(proto.Values[0].Topk.Results[0].List))

	deserialized, err := TopKMatrixFromProto(proto)
	require.NoError(t, err)
	require.Len(t, deserialized, 1)

	require.Equal(t, original.sketch.counters, deserialized[0].Topk.sketch.counters)
	require.Equal(t, *original.hll, *deserialized[0].Topk.hll)
	oCardinality, _ := original.Cardinality()
	fmt.Println("ocardinality: ", oCardinality)
	dCardinality, _ := deserialized[0].Topk.Cardinality()
	fmt.Println("dcardinality: ", dCardinality)

	require.Equal(t, oCardinality, dCardinality)
	require.Equal(t, uint64(100), deserialized[0].TS)
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
		original.Observe(s, 1)
	}

	series := TopKMatrix([]TopKVector{{TS: 100, Topk: original}})

	// Roundtrip serialization
	proto, err := series.ToProto()
	require.NoError(t, err)
	require.Len(t, proto.Values, 1)
	require.Len(t, proto.Values[0].Topk.Cms.Counters, 2048*5)
	require.Len(t, proto.Values[0].Topk.Results[0].List, 100)

	deserialized, err := TopKMatrixFromProto(proto)
	require.NoError(t, err)
	require.Len(t, deserialized, 1)

	require.Equal(t, original.sketch.counters, deserialized[0].Topk.sketch.counters)
	require.Equal(t, *original.hll, *deserialized[0].Topk.hll)
	oCardinality, _ := original.Cardinality()
	dCardinality, _ := deserialized[0].Topk.Cardinality()
	require.Equal(t, oCardinality, dCardinality)
	require.Equal(t, uint64(100), deserialized[0].TS)
}
