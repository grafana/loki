package queryrange

import (
	"io"
	"sort"

	"github.com/cespare/xxhash/v2"
	jsoniter "github.com/json-iterator/go"
	"github.com/richardartoul/molecule"
	"github.com/richardartoul/molecule/src/codec"

	"github.com/grafana/loki/pkg/querier/queryrange/queryrangebase"
)

type LokiSeriesResponseView struct {
	buffer  []byte
	headers []*queryrangebase.PrometheusResponseHeader
}

var _ queryrangebase.Response = &LokiSeriesResponseView{}

func (v *LokiSeriesResponseView) GetHeaders() []*queryrangebase.PrometheusResponseHeader {
	return v.headers
}

// Implement proto.Message
func (v *LokiSeriesResponseView) Reset()         {}
func (v *LokiSeriesResponseView) String() string { return "" }
func (v *LokiSeriesResponseView) ProtoMessage()  {}

func (v *LokiSeriesResponseView) ForEachSeries(fn func(view *SeriesIdentifierView) error) error {
	return molecule.MessageEach(codec.NewBuffer(v.buffer), func(fieldNum int32, value molecule.Value) (bool, error) {
		if fieldNum == 2 {
			identifier, err := value.AsBytesUnsafe()
			if err != nil {
				return false, err
			}

			view := &SeriesIdentifierView{buffer: identifier}
			err = fn(view)
			if err != nil {
				return false, err
			}
		}
		return true, nil
	})
}

type SeriesIdentifierView struct {
	buffer []byte
}

func (v *SeriesIdentifierView) ForEachLabel(fn func(string, string) error) error {
	pair := make([]string, 0, 2)
	return molecule.MessageEach(codec.NewBuffer(v.buffer), func(fieldNum int32, data molecule.Value) (bool, error) {
		if fieldNum == 1 {
			entry, err := data.AsBytesUnsafe()
			if err != nil {
				return false, err
			}

			err = molecule.MessageEach(codec.NewBuffer(entry), func(fieldNum int32, labelOrKey molecule.Value) (bool, error) {
				s, err := labelOrKey.AsStringUnsafe()
				if err != nil {
					return false, err
				}
				pair = append(pair, s)
				return true, nil
			})
			if err != nil {
				return false, err
			}

			// TODO(karsten): check length of pair

			err = fn(pair[0], pair[1])
			if err != nil {
				return false, err
			}

			pair = pair[:0]

			return true, nil
		}

		return true, nil
	})
}

var sep = string([]byte{'\xff'})

func (v *SeriesIdentifierView) HashFast(b []byte, keyLabelPairs []string) (uint64, []string, error) {
	keyLabelPairs = keyLabelPairs[:0]
	err := molecule.MessageEach(codec.NewBuffer(v.buffer), func(fieldNum int32, data molecule.Value) (bool, error) {
		if fieldNum == 1 {
			entry, err := data.AsStringUnsafe()
			if err != nil {
				return false, err
			}

			keyLabelPairs = append(keyLabelPairs, entry)

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

func (v *SeriesIdentifierView) Hash(b []byte, keyLabelPairs []string) (uint64, []string, error) {
	keyLabelPairs = keyLabelPairs[:0]
	err := v.ForEachLabel(func(name, value string) error {
		pair := name + sep + value + sep
		keyLabelPairs = append(keyLabelPairs, pair)
		return nil
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

type MergedSeriesResponseView struct {
	responses []*LokiSeriesResponseView
	headers   []*queryrangebase.PrometheusResponseHeader
}

var _ queryrangebase.Response = &MergedSeriesResponseView{}

func (v *MergedSeriesResponseView) GetHeaders() []*queryrangebase.PrometheusResponseHeader {
	return v.headers
}

// Implement proto.Message
func (v *MergedSeriesResponseView) Reset()         {}
func (v *MergedSeriesResponseView) String() string { return "" }
func (v *MergedSeriesResponseView) ProtoMessage()  {}

func (v *MergedSeriesResponseView) ForEachUniqueSeries(fn func(*SeriesIdentifierView) error) error {
	uniqueSeries := make(map[uint64]struct{})
	b := make([]byte, 0, 1024)
	keyBuffer := make([]string, 0, 32)
	var key uint64
	var err error
	for _, response := range v.responses {
		err = response.ForEachSeries(func(series *SeriesIdentifierView) error {
			key, keyBuffer, err = series.HashFast(b, keyBuffer)
			if err != nil {
				return err
			}

			if _, duplicate := uniqueSeries[key]; !duplicate {
				err = fn(series)
				if err != nil {
					return err
				}
				uniqueSeries[key] = struct{}{}
			}

			return nil
		})
		if err != nil {
			return err
		}
	}

	return nil
}

func WriteSeriesResponseViewJSON(v *MergedSeriesResponseView, w io.Writer) error {
	s := jsoniter.ConfigFastest.BorrowStream(w)
	defer jsoniter.ConfigFastest.ReturnStream(s)

	s.WriteObjectStart()
	s.WriteObjectField("status")
	s.WriteString("success")

	s.WriteMore()
	s.WriteObjectField("data")
	s.WriteArrayStart()

	firstSeriesWrite := true
	firstLabelWrite := true
	err := v.ForEachUniqueSeries(func(id *SeriesIdentifierView) error {
		if firstSeriesWrite {
			firstSeriesWrite = false
		} else {
			s.WriteMore()
		}
		s.WriteObjectStart()

		firstLabelWrite = true
		err := id.ForEachLabel(func(name, value string) error {
			if firstLabelWrite {
				firstLabelWrite = false
			} else {
				s.WriteMore()
			}

			s.WriteObjectField(name)
			s.WriteString(value)

			return nil
		})
		if err != nil {
			return err
		}

		s.WriteObjectEnd()
		s.Flush()
		return nil
	})
	if err != nil {
		return err
	}

	s.WriteArrayEnd()
	s.WriteObjectEnd()
	s.WriteRaw("\n")
	return s.Flush()
}
