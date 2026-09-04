package push

import (
	"io"
	"sync"

	"github.com/klauspost/compress/zstd"
)

// zstdDecoderPool recycles zstd decoders across requests.
//
// A decoder is expensive to build: at the default concurrency it allocates a set of block
// decoders and their buffers, which on OTLP push bodies measures around 11MB per request —
// more than the decompressed body itself, and the largest single cost on the zstd path.
//
// The trade is resident memory. A decoder keeps its decode history while it sits idle in
// the pool, and a stream from a default encoder declares an 8MB window, so an idle decoder
// retains ~9MB regardless of how small the body was. Retention is therefore roughly peak
// concurrent zstd requests times that, bounded by sync.Pool being emptied at every GC.
// The window is chosen by the sender, so WithDecoderMaxWindow cannot cap it without
// rejecting valid streams.
var zstdDecoderPool sync.Pool

// getZstdDecoder returns a decoder reading src. Every decoder it hands out must be passed
// to putZstdDecoder.
func getZstdDecoder(src io.Reader) (*zstd.Decoder, error) {
	d, _ := zstdDecoderPool.Get().(*zstd.Decoder)
	if d == nil {
		// Concurrency 1 decodes on the calling goroutine. Push requests already arrive in
		// parallel, so there is little to win from splitting one body across goroutines,
		// and it means a decoder sitting in the pool holds no goroutine and one set of
		// block buffers rather than several.
		return zstd.NewReader(src, zstd.WithDecoderConcurrency(1))
	}
	if err := d.Reset(src); err != nil {
		putZstdDecoder(d)
		return nil, err
	}
	return d, nil
}

// putZstdDecoder releases a decoder obtained from getZstdDecoder. It must be called for
// every one of them: a stream abandoned part way through — a body that trips a size limit
// or fails to decode — otherwise leaks the decoder's buffers, and its decode goroutines
// for the lifetime of the process.
//
// Reset(nil) drains any in-flight decode and drops the reference to the request body.
// Unlike Close it leaves the decoder reusable.
func putZstdDecoder(d *zstd.Decoder) {
	_ = d.Reset(nil)
	zstdDecoderPool.Put(d)
}
