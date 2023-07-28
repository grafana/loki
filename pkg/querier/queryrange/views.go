package queryrange

import (
	"sort"

	"github.com/cespare/xxhash/v2"
	"github.com/richardartoul/molecule"
	"github.com/richardartoul/molecule/codec"
)

type LokiSeriesResponseView struct {
	buffer codec.Buffer
}

func (v *LokiSeriesResponseView) GetSeriesView() (*SeriesView, error) {
	var view *SeriesView

	return view, nil
}

func (v *LokiSeriesResponseView) ForEachSeries(fn func(view *SeriesView) error) error {
	return molecule.MessageEach(v.buffer, func(fieldNum int32, value molecule.Value) (bool, error) {
		if fieldNum == 1 {
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
				view := &SeriesView{buffer: codec.NewBuffer(series)}
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

type SeriesView struct {
	buffer codec.Buffer
}

func (v *SeriesView) Hash(b []byte, keysForLabels []string) (uint64, []string) {
	keysForLabels = keysForLabels[:0]
	molecule.MessageEach(v.buffer, func(fieldNum int32, value molecule.Value) (bool, error) { 
		if fieldNum == 1 {
			labelEntries, err := value.AsBytesUnsafe()
			if err != nil {
				return false, err
			}		
			return false, nil
		}
		return true, nil
	})

	for k := range id.Labels {
		keysForLabels = append(keysForLabels, k)
	}
	sort.Strings(keysForLabels)

	// Use xxhash.Sum64(b) for fast path as it's faster.
	b = b[:0]
	for i, name := range keysForLabels {
		value := id.Labels[name]
		if len(b)+len(name)+len(value)+2 >= cap(b) {
			// If labels entry is 1KB+ do not allocate whole entry.
			h := xxhash.New()
			_, _ = h.Write(b)
			for _, name := range keysForLabels[i:] {
				value := id.Labels[name]
				_, _ = h.WriteString(name)
				_, _ = h.Write(seps)
				_, _ = h.WriteString(value)
				_, _ = h.Write(seps)
			}
			return h.Sum64(), keysForLabels
		}

		b = append(b, name...)
		b = append(b, seps[0])
		b = append(b, value...)
		b = append(b, seps[0])
	}
	return xxhash.Sum64(b), keysForLabels
}
