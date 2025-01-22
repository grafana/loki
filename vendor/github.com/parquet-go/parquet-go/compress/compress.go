// Package compress provides the generic APIs implemented by parquet compression
// codecs.
//
// https://github.com/apache/parquet-format/blob/master/Compression.md
package compress

import (
	"bytes"
	"io"
	"sync"

	"github.com/parquet-go/parquet-go/format"
)

// The Codec interface represents parquet compression codecs implemented by the
// compress sub-packages.
//
// Codec instances must be safe to use concurrently from multiple goroutines.
type Codec interface {
	// Returns a human-readable name for the codec.
	String() string

	// Returns the code of the compression codec in the parquet format.
	CompressionCodec() format.CompressionCodec

	// Writes the compressed version of src to dst and returns it.
	//
	// The method automatically reallocates the output buffer if its capacity
	// was too small to hold the compressed data.
	Encode(dst, src []byte) ([]byte, error)

	// Writes the uncompressed version of src to dst and returns it.
	//
	// The method automatically reallocates the output buffer if its capacity
	// was too small to hold the uncompressed data.
	Decode(dst, src []byte) ([]byte, error)
}

type Reader interface {
	io.ReadCloser
	Reset(io.Reader) error
}

type Writer interface {
	io.WriteCloser
	Reset(io.Writer)
}

type Compressor struct {
	writers sync.Pool // *writer
}

type writer struct {
	output bytes.Buffer
	writer Writer
}

func (c *Compressor) Encode(dst, src []byte, newWriter func(io.Writer) (Writer, error)) ([]byte, error) {
	w, _ := c.writers.Get().(*writer)
	if w != nil {
		w.output = *bytes.NewBuffer(dst[:0])
		w.writer.Reset(&w.output)
	} else {
		w = new(writer)
		w.output = *bytes.NewBuffer(dst[:0])
		var err error
		if w.writer, err = newWriter(&w.output); err != nil {
			return dst, err
		}
	}

	defer func() {
		w.output = *bytes.NewBuffer(nil)
		w.writer.Reset(io.Discard)
		c.writers.Put(w)
	}()

	if _, err := w.writer.Write(src); err != nil {
		return w.output.Bytes(), err
	}
	if err := w.writer.Close(); err != nil {
		return w.output.Bytes(), err
	}
	return w.output.Bytes(), nil
}

type Decompressor struct {
	readers sync.Pool // *reader
}

type reader struct {
	input  bytes.Reader
	reader Reader
}

func (d *Decompressor) Decode(dst, src []byte, newReader func(io.Reader) (Reader, error)) ([]byte, error) {
	r, _ := d.readers.Get().(*reader)
	if r != nil {
		r.input.Reset(src)
		if err := r.reader.Reset(&r.input); err != nil {
			return dst, err
		}
	} else {
		r = new(reader)
		r.input.Reset(src)
		var err error
		if r.reader, err = newReader(&r.input); err != nil {
			return dst, err
		}
	}

	defer func() {
		r.input.Reset(nil)
		if err := r.reader.Reset(nil); err == nil {
			d.readers.Put(r)
		}
	}()

	if cap(dst) == 0 {
		dst = make([]byte, 0, 2*len(src))
	} else {
		dst = dst[:0]
	}

	for {
		n, err := r.reader.Read(dst[len(dst):cap(dst)])
		dst = dst[:len(dst)+n]

		if err != nil {
			if err == io.EOF {
				err = nil
			}
			return dst, err
		}

		if len(dst) == cap(dst) {
			tmp := make([]byte, len(dst), 2*len(dst))
			copy(tmp, dst)
			dst = tmp
		}
	}
}
