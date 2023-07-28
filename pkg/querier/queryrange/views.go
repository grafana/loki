package queryrange

import (
	"sort"

	"github.com/cespare/xxhash/v2"
	"github.com/richardartoul/molecule"
	"github.com/richardartoul/molecule/src/codec"
)

type LokiSeriesResponseView struct {
	buffer *codec.Buffer
}

func (v *LokiSeriesResponseView) GetSeriesView() (*SeriesIdentifierView, error) {
	var view *SeriesIdentifierView

	return view, nil
}

func (v *LokiSeriesResponseView) ForEachSeries(fn func(view *SeriesIdentifierView) error) error {
	return molecule.MessageEach(v.buffer, func(fieldNum int32, value molecule.Value) (bool, error) {
		if fieldNum == 2 {
			data, err := value.AsBytesUnsafe()
			if err != nil {
				return false, err
			}

			b := codec.NewBuffer(data)
			err = molecule.PackedRepeatedEach(b, codec.FieldType_MESSAGE, func(v molecule.Value) (bool, error) {
				series, err := v.AsBytesUnsafe()
				if err != nil {
					return false, err
				}
				view := &SeriesIdentifierView{buffer: codec.NewBuffer(series)}
				err = fn(view)
				if err != nil {
					return false, err
				}
				return true, nil
			})

			if err != nil {
				return false, err
			}

			return false, nil
		}
		return true, nil
	})
}

type SeriesIdentifierView struct {
	buffer *codec.Buffer
}

func (v *SeriesIdentifierView) Hash(b []byte, keyLabelPairs []string) (uint64, []string, error) {
	keyLabelPairs = keyLabelPairs[:0]
	err := molecule.MessageEach(v.buffer, func(fieldNum int32, data molecule.Value) (bool, error) {
		if fieldNum == 1 {
			entry, err := data.AsBytesUnsafe()
			if err != nil {
				return false, err
			}

			pair := ""
			err = molecule.MessageEach(codec.NewBuffer(entry), func(fieldNum int32, labelOrKey molecule.Value) (bool, error) {
				s, err := labelOrKey.AsStringUnsafe()
				if err != nil {
					return false, err
				}
				pair += s
				pair += string([]byte{'\xff'})
				return true, nil
			})
			keyLabelPairs = append(keyLabelPairs, pair)

			return true, err
		}

		return true, nil
	})

	if err != nil {
		return 0, nil, err
	}

	sort.Strings(keyLabelPairs)

	// Use xxhash.Sum64(b) for fast path as it's faster.
	b = b[:0]
	for i, pair := range keyLabelPairs {
		if len(b)+len(pair) >= cap(b) {
			// If labels entry is 1KB+ do not allocate whole entry.
			h := xxhash.New()
			_, _ = h.Write(b)
			for _, pair := range keyLabelPairs[i:] {
				_, _ = h.WriteString(pair)
			}
			return h.Sum64(), keyLabelPairs, nil
		}

		b = append(b, pair...)
	}
	return xxhash.Sum64(b), keyLabelPairs, nil
}
