package dataset

import (
	"bufio"
	"errors"
	"fmt"
	"io"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
)

const decodeBufferSize = 128

func iterMemPage(p *MemPage, valueType datasetmd.ValueType, compressionType datasetmd.CompressionType) result.Seq[Value] {
	return result.Iter(func(yield func(Value) bool) error {
		presenceReader, valuesReader, err := p.reader(compressionType)
		if err != nil {
			return fmt.Errorf("opening page for reading: %w", err)
		}
		defer valuesReader.Close()

		presenceDec := newBitmapDecoder(bufio.NewReader(presenceReader))
		valuesDec, ok := newValueDecoder(valueType, p.Info.Encoding, bufio.NewReader(valuesReader))
		if !ok {
			return fmt.Errorf("no decoder available for %s/%s", valueType, p.Info.Encoding)
		}

		var iB, iV = decodeBufferSize, decodeBufferSize                                // current index of buffers for precence values and values
		var nB, nV = decodeBufferSize, decodeBufferSize                                // size of buffers for precence values and values
		bufB, bufV := make([]Value, decodeBufferSize), make([]Value, decodeBufferSize) // buffers for precence values and values

		for {
			var value Value

			// There are not decoded presence values in the buffer, so read a new
			// batch up to decodeBufferSize values and reset the current index of the
			// value inside the buffer.
			if iB >= nB-1 {
				nB, err = presenceDec.Decode(bufB[:decodeBufferSize])
				bufB = bufB[:nB]
				iB = 0
				if nB == 0 || errors.Is(err, io.EOF) {
					return nil
				} else if err != nil {
					return fmt.Errorf("decoding presence bitmap: %w", err)
				}
			} else {
				iB++
			}

			if bufB[iB].Uint64() == 0 { //nolint:revive
				// If the presence bitmap says our row has no value, we need to yield a
				// nil value, so do nothing.
			} else if bufB[iB].Uint64() == 1 {
				// If the presence bitmap says our row has a value, we decode it.

				// There are no decoded values in the buffer, so read a new batch up to
				// decodeBufferSize values and reset the current index of the value inside the buffer.
				if iV >= nV-1 {
					nV, err = valuesDec.Decode(bufV[:decodeBufferSize])
					bufV = bufV[:nV]
					iV = 0
					if nV == 0 || errors.Is(err, io.EOF) {
						return fmt.Errorf("too few values to decode: expected at least %d, got %d", len(bufB), nV)
					} else if err != nil {
						return fmt.Errorf("decoding value: %w", err)
					}
				} else {
					iV++
				}
				value = bufV[iV]
			}

			if !yield(value) {
				return nil
			}
		}
	})
}
