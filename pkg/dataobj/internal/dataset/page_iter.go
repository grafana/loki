package dataset

import (
	"bufio"
	"errors"
	"fmt"
	"io"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
)

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

		for {
			var value Value

			present, err := presenceDec.Decode()
			if errors.Is(err, io.EOF) {
				return nil
			} else if err != nil {
				return fmt.Errorf("decoding presence bitmap: %w", err)
			} else if present.Type() != datasetmd.VALUE_TYPE_UINT64 {
				return fmt.Errorf("unexpected presence type %s", present.Type())
			}

			// value is currently nil. If the presence bitmap says our row has a
			// value, we decode it into value.
			if present.Uint64() == 1 {
				value, err = valuesDec.Decode()
				if err != nil {
					return fmt.Errorf("decoding value: %w", err)
				}
			}

			if !yield(value) {
				return nil
			}
		}
	})
}
