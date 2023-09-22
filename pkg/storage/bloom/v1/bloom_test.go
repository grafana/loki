package v1

import (
	"fmt"
	"testing"

	"github.com/grafana/loki/pkg/util/encoding"
	"github.com/owen-d/BoomFilters/boom"
	"github.com/stretchr/testify/require"
)

func mkBasicBlooms(n int) []Bloom {
	var blooms []Bloom
	for i := 0; i < n; i++ {
		var bloom Bloom
		bloom.sbf = *boom.NewScalableBloomFilter(1024, 0.01, 0.8)
		bloom.sbf.Add([]byte(fmt.Sprint(i)))
		blooms = append(blooms, bloom)
	}
	return blooms
}

func mkBasicBloomPage(n int) BloomPage {
	return BloomPage{
		Blooms: mkBasicBlooms(n),
	}
}

func TestBloomPageEncoding(t *testing.T) {
	blooms := mkBasicBlooms(2)
	src := BloomPage{
		Blooms: blooms,
	}

	enc := &encoding.Encbuf{}
	src.Encode(enc, Crc32HashPool.Get())

	var dst BloomPage
	dec := encoding.DecWith(enc.Get())
	require.Nil(t, dst.Decode(&dec))
	require.Equal(t, src, dst)
}
