package sarama

import (
	"bytes"
	"fmt"
	"sync"

	snappy "github.com/eapache/go-xerial-snappy"
	"github.com/klauspost/compress/gzip"
	"github.com/pierrec/lz4/v4"
)

var (
	lz4WriterPool = sync.Pool{
		New: func() interface{} {
			lz := lz4.NewWriter(nil)
			if err := lz.Apply(lz4.BlockSizeOption(lz4.Block64Kb)); err != nil {
				panic(err)
			}
			return lz
		},
	}

	gzipWriterPool = sync.Pool{
		New: func() interface{} {
			return gzip.NewWriter(nil)
		},
	}
	gzipWriterPoolForCompressionLevel1 = sync.Pool{
		New: func() interface{} {
			gz, err := gzip.NewWriterLevel(nil, 1)
			if err != nil {
				panic(err)
			}
			return gz
		},
	}
	gzipWriterPoolForCompressionLevel2 = sync.Pool{
		New: func() interface{} {
			gz, err := gzip.NewWriterLevel(nil, 2)
			if err != nil {
				panic(err)
			}
			return gz
		},
	}
	gzipWriterPoolForCompressionLevel3 = sync.Pool{
		New: func() interface{} {
			gz, err := gzip.NewWriterLevel(nil, 3)
			if err != nil {
				panic(err)
			}
			return gz
		},
	}
	gzipWriterPoolForCompressionLevel4 = sync.Pool{
		New: func() interface{} {
			gz, err := gzip.NewWriterLevel(nil, 4)
			if err != nil {
				panic(err)
			}
			return gz
		},
	}
	gzipWriterPoolForCompressionLevel5 = sync.Pool{
		New: func() interface{} {
			gz, err := gzip.NewWriterLevel(nil, 5)
			if err != nil {
				panic(err)
			}
			return gz
		},
	}
	gzipWriterPoolForCompressionLevel6 = sync.Pool{
		New: func() interface{} {
			gz, err := gzip.NewWriterLevel(nil, 6)
			if err != nil {
				panic(err)
			}
			return gz
		},
	}
	gzipWriterPoolForCompressionLevel7 = sync.Pool{
		New: func() interface{} {
			gz, err := gzip.NewWriterLevel(nil, 7)
			if err != nil {
				panic(err)
			}
			return gz
		},
	}
	gzipWriterPoolForCompressionLevel8 = sync.Pool{
		New: func() interface{} {
			gz, err := gzip.NewWriterLevel(nil, 8)
			if err != nil {
				panic(err)
			}
			return gz
		},
	}
	gzipWriterPoolForCompressionLevel9 = sync.Pool{
		New: func() interface{} {
			gz, err := gzip.NewWriterLevel(nil, 9)
			if err != nil {
				panic(err)
			}
			return gz
		},
	}
)

func compress(cc CompressionCodec, level int, data []byte) ([]byte, error) {
	switch cc {
	case CompressionNone:
		return data, nil
	case CompressionGZIP:
		return gzipCompress(level, data)
	case CompressionSnappy:
		return snappy.Encode(data), nil
	case CompressionLZ4:
		return lz4Compress(data)
	case CompressionZSTD:
		return zstdCompress(ZstdEncoderParams{level}, nil, data)
	default:
		return nil, PacketEncodingError{fmt.Sprintf("unsupported compression codec (%d)", cc)}
	}
}

func gzipCompress(level int, data []byte) ([]byte, error) {
	var (
		buf    bytes.Buffer
		writer *gzip.Writer
		pool   *sync.Pool
	)

	switch level {
	case CompressionLevelDefault:
		pool = &gzipWriterPool
	case 1:
		pool = &gzipWriterPoolForCompressionLevel1
	case 2:
		pool = &gzipWriterPoolForCompressionLevel2
	case 3:
		pool = &gzipWriterPoolForCompressionLevel3
	case 4:
		pool = &gzipWriterPoolForCompressionLevel4
	case 5:
		pool = &gzipWriterPoolForCompressionLevel5
	case 6:
		pool = &gzipWriterPoolForCompressionLevel6
	case 7:
		pool = &gzipWriterPoolForCompressionLevel7
	case 8:
		pool = &gzipWriterPoolForCompressionLevel8
	case 9:
		pool = &gzipWriterPoolForCompressionLevel9
	default:
		var err error
		writer, err = gzip.NewWriterLevel(&buf, level)
		if err != nil {
			return nil, err
		}
	}
	if pool != nil {
		writer = pool.Get().(*gzip.Writer)
		writer.Reset(&buf)
		defer pool.Put(writer)
	}
	if _, err := writer.Write(data); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func lz4Compress(data []byte) ([]byte, error) {
	writer := lz4WriterPool.Get().(*lz4.Writer)
	defer lz4WriterPool.Put(writer)

	var buf bytes.Buffer
	writer.Reset(&buf)
	if _, err := writer.Write(data); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
