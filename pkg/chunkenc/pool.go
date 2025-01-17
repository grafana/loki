package chunkenc

import (
	"bytes"
	"sync"

	"github.com/prometheus/prometheus/util/pool"

	"github.com/grafana/loki/v3/pkg/logproto"
)

var (
	// BytesBufferPool is a bytes buffer used for lines decompressed.
	// Buckets [0.5KB,1KB,2KB,4KB,8KB]
	BytesBufferPool = pool.New(1<<9, 1<<13, 2, func(size int) interface{} { return make([]byte, 0, size) })

	// LabelsPool is a matrix of bytes buffers used to store label names and values.
	// Buckets [8, 16, 32, 64, 128, 256].
	// Since we store label names and values, the number of labels we can store is the half the bucket size.
	// So we will be able to store from 0 to 128 labels.
	LabelsPool = pool.New(1<<3, 1<<8, 2, func(size int) interface{} { return make([][]byte, 0, size) })

	SymbolsPool = pool.New(1<<3, 1<<8, 2, func(size int) interface{} { return make([]symbol, 0, size) })

	// SamplesPool pooling array of samples [512,1024,...,16k]
	SamplesPool = pool.New(1<<9, 1<<14, 2, func(size int) interface{} { return make([]logproto.Sample, 0, size) })

	// Pool of crc32 hash
	crc32HashPool = sync.Pool{
		New: func() interface{} {
			return newCRC32()
		},
	}

	serializeBytesBufferPool = sync.Pool{
		New: func() interface{} {
			return &bytes.Buffer{}
		},
	}

	// EncodeBufferPool is a pool used to binary encode.
	EncodeBufferPool = sync.Pool{
		New: func() interface{} {
			return &encbuf{
				b: make([]byte, 0, 256),
			}
		},
	}
)
