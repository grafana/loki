package util

import (
	"bytes"
	"fmt"
	"io"
)

func ReadSeeker(r io.Reader) (io.ReadSeeker, error) {
	if rs, ok := r.(io.ReadSeeker); ok {
		return rs, nil
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("Error in ReadSeeker ReadAll(): %w", err)
	}
	return bytes.NewReader(data), nil
}
