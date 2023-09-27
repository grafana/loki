package v1

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/grafana/loki/pkg/chunkenc"
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

	schema := Schema{version: DefaultSchemaVersion, encoding: chunkenc.EncSnappy}
	enc := &encoding.Encbuf{}
	decompressedLen, err := src.Encode(enc, schema.CompressorPool(), Crc32HashPool.Get())
	require.Nil(t, err)

	var dst BloomPage
	dec := encoding.DecWith(enc.Get())
	require.Nil(t, dst.Decode(&dec, schema.DecompressorPool(), decompressedLen))
	require.Equal(t, src, dst)
}

func TestBloomBlockEncoding(t *testing.T) {
	pages := []BloomPage{
		{
			Blooms: mkBasicBlooms(2),
		},
		{
			Blooms: mkBasicBlooms(2),
		},
	}

	pageItr := NewSliceIter[BloomPage](pages)
	src := NewBloomBlock(chunkenc.EncSnappy)
	buf := bytes.NewBuffer(nil)

	_, err := src.WriteTo(pageItr, buf)
	require.Nil(t, err)

	data := buf.Bytes()
	var dst BloomBlock
	require.Nil(t, dst.DecodeHeaders(bytes.NewReader(data)))

	require.Equal(t, src, dst)
}

func TestBloomBlockPageIteration(t *testing.T) {
	pages := []BloomPage{
		{
			Blooms: mkBasicBlooms(2),
		},
		{
			Blooms: mkBasicBlooms(2),
		},
	}

	pageItr := NewSliceIter[BloomPage](pages)
	src := NewBloomBlock(chunkenc.EncSnappy)
	buf := bytes.NewBuffer(nil)

	_, err := src.WriteTo(pageItr, buf)
	require.Nil(t, err)

	data := buf.Bytes()
	var dst BloomBlock
	require.Nil(t, dst.DecodeHeaders(bytes.NewReader(data)))
	require.Equal(t, src, dst)

	for i := range pages {
		srcPage := pages[i]
		decoder, err := dst.BloomPageDecoder(bytes.NewReader(data), BloomOffset{Page: i})
		require.Nil(t, err)
		require.Equal(t, len(srcPage.Blooms), decoder.N())

		for i := 0; i < decoder.N(); i++ {

			bloom, err := decoder.Next()
			require.Nil(t, err)
			require.Equal(t, srcPage.Blooms[i], bloom)
		}

		_, err = decoder.Next()
		require.NotNil(t, err)
	}
}
