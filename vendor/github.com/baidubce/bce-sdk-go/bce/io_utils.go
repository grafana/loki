package bce

import (
	"fmt"
	"io"
)

type TeeReadNopCloser struct {
	reader  io.Reader
	writers []io.Writer
	mark    int64
}

func NewTeeReadNopCloser(reader io.Reader, writers ...io.Writer) io.ReadCloser {
	return &TeeReadNopCloser{
		reader:  reader,
		writers: writers,
		mark:    -1,
	}
}

func (t *TeeReadNopCloser) AddWriter(writer io.Writer) {
	t.writers = append(t.writers, writer)
}

func (t *TeeReadNopCloser) Read(p []byte) (int, error) {
	n, err := t.reader.Read(p)
	if n > 0 {
		for _, w := range t.writers {
			if nn, err := w.Write(p[:n]); err != nil {
				return nn, err
			}
		}
	}
	return n, err
}

func (t *TeeReadNopCloser) IsSeekable() bool {
	_, ok := t.reader.(io.Seeker)
	return ok
}

func (t *TeeReadNopCloser) Seek(offset int64, whence int) (int64, error) {
	if s, ok := t.reader.(io.Seeker); ok {
		return s.Seek(offset, whence)
	}
	return 0, nil
}

func (t *TeeReadNopCloser) Close() error {
	return nil
}

// marks the currenct offset in this reader, . A subsequent call to
// the Reset method repositions this reader at the last marked position
// so that subsequent reads re-read the same bytes.
func (t *TeeReadNopCloser) Mark() {
	if s, ok := t.reader.(io.Seeker); ok {
		if pos, err := s.Seek(0, io.SeekCurrent); err == nil {
			t.mark = pos
		}
	}
}

func (t *TeeReadNopCloser) Reset() error {
	if !t.IsSeekable() {
		return fmt.Errorf("Mark/Reset not support")
	}
	if t.mark < 0 {
		return fmt.Errorf("Mark is not called yet")
	}
	// seek to the last marked position
	if s, ok := t.reader.(io.Seeker); ok {
		if _, err := s.Seek(t.mark, io.SeekStart); err != nil {
			return err
		}
	}
	// reset writers
	type reseter interface {
		Reset()
	}
	for _, w := range t.writers {
		if wr, ok := w.(reseter); ok {
			wr.Reset()
		}
	}
	return nil
}
