// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package memberlist

import (
	"bytes"
	"compress/lzw"
	"fmt"
	"io"
	"sync"
)

const (
	// lzwLitWidth is the literal width passed to compress/lzw. The value
	// is part of the wire format and must not be changed.
	lzwLitWidth = 8

	// maxPooledLZWBufferCap bounds the capacity of *bytes.Buffer values
	// retained by lzwBufferPool. LZW output for a UDP packet (UDPBufferSize,
	// default 1400 B) is small and bounded; 256 KiB covers any realistic
	// per-call size without retaining outsized buffers.
	maxPooledLZWBufferCap = 256 * 1024
)

// lzwBufferPool recycles *bytes.Buffer values used inside lzwCompress to
// hold the LZW-encoded output. The buffer is acquired and released within
// a single compressPayload call; it never escapes to the network or is
// held across goroutines, so it is safe to pool here.
var lzwBufferPool = sync.Pool{
	New: func() any {
		return new(bytes.Buffer)
	},
}

func getLZWBuffer() *bytes.Buffer {
	buf := lzwBufferPool.Get().(*bytes.Buffer)
	buf.Reset()
	return buf
}

func releaseLZWBuffer(b *bytes.Buffer) {
	if b.Cap() > maxPooledLZWBufferCap {
		return
	}
	b.Reset()
	lzwBufferPool.Put(b)
}

// lzwReaderInitSrc is a shared, never-mutated *bytes.Reader handed to
// lzw.NewReader inside the lzwReaderPool.New function as a placeholder.
// Callers MUST Reset the returned lzw.Reader before any Read; this
// invariant is what makes the shared placeholder safe across goroutines.
var lzwReaderInitSrc = bytes.NewReader(nil)

// lzwWriterPool recycles lzw encoder state machines.
// (*lzw.Writer).Reset zeros the entire internal struct (*w = Writer{}) and
// re-inits, so the errClosed state set by our post-use Close is cleared on
// the next Reset before any other call. We rely on this Reset-zeros-all-state
// behavior; it has been stable since Go 1.5 but is not formally documented.
var lzwWriterPool = sync.Pool{
	New: func() any {
		return lzw.NewWriter(io.Discard, lzw.LSB, lzwLitWidth).(*lzw.Writer)
	},
}

// lzwReaderPool recycles lzw decoder state machines.
// Same Reset-zeros-all-state assumption as lzwWriterPool.
var lzwReaderPool = sync.Pool{
	New: func() any {
		// Type-assert *lzw.Reader so we can call Reset on subsequent Gets.
		return lzw.NewReader(lzwReaderInitSrc, lzw.LSB, lzwLitWidth).(*lzw.Reader)
	},
}

// lzwCompress lzw compresses src and returns the pooled scratch
// buffer holding the encoded bytes. The caller MUST releaseLZWBuffer the
// returned buffer once the bytes are no longer needed.
func lzwCompress(src []byte) (*bytes.Buffer, error) {
	buf := getLZWBuffer()
	w := lzwWriterPool.Get().(*lzw.Writer)
	// The writer is reusable after the next Reset, so we return it to the
	// pool unconditionally.
	defer lzwWriterPool.Put(w)
	// Reset to io.Discard before Put so the pooled writer doesn't retain
	// the pool *bytes.Buffer across its idle period. Same shape as
	// lzwDecompress (see the comment there for why two top-level defers
	// are open-coded instead of a single closure).
	defer w.Reset(io.Discard, lzw.LSB, lzwLitWidth)
	w.Reset(buf, lzw.LSB, lzwLitWidth)

	if _, err := w.Write(src); err != nil {
		releaseLZWBuffer(buf)
		return nil, fmt.Errorf("lzw compress: %w", err)
	}
	// Ensure we flush everything out
	if err := w.Close(); err != nil {
		releaseLZWBuffer(buf)
		return nil, fmt.Errorf("lzw compress: %w", err)
	}

	return buf, nil
}

// lzwDecompress returns a freshly allocated []byte holding the decompressed
// payload.
func lzwDecompress(src []byte) ([]byte, error) {
	r := lzwReaderPool.Get().(*lzw.Reader)
	defer lzwReaderPool.Put(r)
	// Reset to the never-mutated placeholder before Put so the pooled
	// reader doesn't retain src (or its bytes.Reader wrapper) across
	// its idle period in the pool. Two top-level defers stay open-coded;
	// a defer of an anonymous closure would heap-allocate the closure literal.
	defer r.Reset(lzwReaderInitSrc, lzw.LSB, lzwLitWidth)
	r.Reset(bytes.NewReader(src), lzw.LSB, lzwLitWidth)

	// io.LimitedReader as a value, not via io.LimitReader. The wrapper
	// function returns &LimitedReader{...} unconditionally; using a value
	// type at least makes the intent explicit. NOTE: in practice escape
	// analysis still moves &lr to the heap because io.Copy takes an
	// io.Reader interface and the compiler can't prove the interface
	// doesn't escape across that boundary. Net cost: +1 alloc/op (24 B)
	// per LZW decompress vs. an unbounded io.Copy — the price of bomb
	// defense. See BenchmarkDecompressBuffer for numbers.
	lr := io.LimitedReader{R: r, N: maxDecompressBytes + 1}

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, &lr); err != nil {
		_ = r.Close()
		return nil, fmt.Errorf("lzwDecompress: read from src: %w", err)
	}
	_ = r.Close()
	if buf.Len() > maxDecompressBytes {
		return nil, fmt.Errorf("memberlist: LZW-decompressed payload exceeds %d bytes", maxDecompressBytes)
	}
	// Clone tightens the returned slice. io.Copy filled buf via growth-
	// doubling, so buf.Bytes() typically has 25-50 % unused capacity that
	// would otherwise stay attached to the returned slice for as long as
	// the caller holds it. LZW can't pre-size (decoded length isn't
	// carried in the wire format), so Clone-after-decode is the cheapest
	// route to a tight return. Contrast snappyDecompress, where
	// snappy.DecodedLen lets us make([]byte, n) up front.
	return bytes.Clone(buf.Bytes()), nil
}
