package queryrange

import (
	"fmt"
	"io"
	"sort"

	"github.com/cespare/xxhash/v2"
	"github.com/gogo/protobuf/protoc-gen-gogo/descriptor"
	jsoniter "github.com/json-iterator/go"
	"github.com/richardartoul/molecule"
	"github.com/richardartoul/molecule/src/codec"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
)

// Pull fiel numbers from protobuf message descriptions.
var (
	queryResponse               *QueryResponse
	_, queryResponseDescription = descriptor.ForMessage(queryResponse)
	seriesResponseFieldNumber   = queryResponseDescription.GetFieldDescriptor("series").GetNumber()

	seriesResponse               *LokiSeriesResponse
	_, seriesResponseDescription = descriptor.ForMessage(seriesResponse)
	dataFieldNumber              = seriesResponseDescription.GetFieldDescriptor("Data").GetNumber()

	seriesIdentifier               *logproto.SeriesIdentifier
	_, seriesIdentifierDescription = descriptor.ForMessage(seriesIdentifier)
	labelsFieldNumber              = seriesIdentifierDescription.GetFieldDescriptor("labels").GetNumber()
)

// GetLokiSeriesResponseView returns a view on the series response of a
// QueryResponse. Returns an error if the message was empty. Note: the method
// does not verify that the reply is a properly encoded QueryResponse protobuf.
func GetLokiSeriesResponseView(data []byte) (view *LokiSeriesResponseView, err error) {
	b := codec.NewBuffer(data)
	err = molecule.MessageEach(b, func(fieldNum int32, value molecule.Value) (bool, error) {
		if fieldNum == seriesResponseFieldNumber {
			if len(value.Bytes) > 0 {
				// We might be able to avoid an allocation and
				// copy here by using value.Bytes
				data, err = value.AsBytesSafe()
				if err != nil {
					return false, fmt.Errorf("could not allocate message bytes: %w", err)
				}
				view = &LokiSeriesResponseView{buffer: data}
			}

		}
		return true, nil
	})

	if err == nil && view == nil {
		err = fmt.Errorf("loki series response message was empty")
	}

	return
}

// LokiSeriesResponseView holds the raw bytes of a LokiSeriesResponse protobuf
// message. It is decoded lazily view ForEachSeries.
type LokiSeriesResponseView struct {
	buffer  []byte
	headers []queryrangebase.PrometheusResponseHeader
}

var _ queryrangebase.Response = &LokiSeriesResponseView{}

func (v *LokiSeriesResponseView) GetHeaders() []*queryrangebase.PrometheusResponseHeader {
	return convertPrometheusResponseHeadersToPointers(v.headers)
}

func (v *LokiSeriesResponseView) WithHeaders(h []queryrangebase.PrometheusResponseHeader) queryrangebase.Response {
	v.headers = h
	return v
}

func (v *LokiSeriesResponseView) SetHeader(name, value string) {
	v.headers = setHeader(v.headers, name, value)
}

// Implement proto.Message
func (v *LokiSeriesResponseView) Reset()         {}
func (v *LokiSeriesResponseView) String() string { return "" }
func (v *LokiSeriesResponseView) ProtoMessage()  {}

// ForEachSeries iterates of the []logproto.SeriesIdentifier slice and pass a
// view on each identifier to the callback supplied.
func (v *LokiSeriesResponseView) ForEachSeries(fn func(view *SeriesIdentifierView) error) error {
	return molecule.MessageEach(codec.NewBuffer(v.buffer), func(fieldNum int32, value molecule.Value) (bool, error) {
		if fieldNum == dataFieldNumber {
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

// SeriesIdentifierView holds the raw bytes of a logproto.SeriesIdentifier
// protobuf message.
type SeriesIdentifierView struct {
	buffer []byte
}

// ForEachLabel iterates over each name-value label pair of the identifier map.
// Note: the strings passed to the supplied callback are unsafe views on the
// underlying data.
func (v *SeriesIdentifierView) ForEachLabel(fn func(string, string) error) error {
	pair := make([]string, 0, 2)
	return molecule.MessageEach(codec.NewBuffer(v.buffer), func(fieldNum int32, data molecule.Value) (bool, error) {
		if fieldNum == 1 {
			entry, err := data.AsBytesUnsafe()
			if err != nil {
				return false, err
			}

			err = molecule.MessageEach(codec.NewBuffer(entry), func(_ int32, labelOrKey molecule.Value) (bool, error) {
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

			if len(pair) != 2 {
				return false, fmt.Errorf("unexpected label pair length, go (%d), want (2)", len(pair))
			}

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

// This is the separator define in the Prometheus Labels.Hash function.
var sep = string([]byte{'\xff'})

// HashFast is a faster version of the Hash method that uses an unsafe string of
// the name value label pairs. It does not have to allocate strings and is not
// using the separator. Thus it is not equivalent to the original Prometheus
// label hash function.
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

// Hash is adapted from SeriesIdentifier.Hash and produces the same hash for the
// same input as the original Prometheus hash method.
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

// MergedSeriesResponseView holds references to all series responses that should
// be merged before serialization to JSON. The de-duplication happens during the
// ForEachUniqueSeries iteration.
type MergedSeriesResponseView struct {
	responses []*LokiSeriesResponseView
	headers   []queryrangebase.PrometheusResponseHeader
}

var _ queryrangebase.Response = &MergedSeriesResponseView{}

func (v *MergedSeriesResponseView) GetHeaders() []*queryrangebase.PrometheusResponseHeader {
	return convertPrometheusResponseHeadersToPointers(v.headers)
}

func (v *MergedSeriesResponseView) WithHeaders(headers []queryrangebase.PrometheusResponseHeader) queryrangebase.Response {
	v.headers = headers
	return v
}

func (v *MergedSeriesResponseView) SetHeader(name, value string) {
	v.headers = setHeader(v.headers, name, value)
}

// Implement proto.Message
func (v *MergedSeriesResponseView) Reset()         {}
func (v *MergedSeriesResponseView) String() string { return "" }
func (v *MergedSeriesResponseView) ProtoMessage()  {}

// ForEachUniqueSeries iterates over all unique series identifiers of all series
// responses. It uses the HashFast method before passing the identifier view to
// the supplied callback.
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

// Materialize produces a LokiSeriesResponse instance that is a deserialized
// probobuf message.
func (v *MergedSeriesResponseView) Materialize() (*LokiSeriesResponse, error) {
	mat := &LokiSeriesResponse{}
	err := v.ForEachUniqueSeries(func(series *SeriesIdentifierView) error {
		identifier := logproto.SeriesIdentifier{Labels: make([]logproto.SeriesIdentifier_LabelsEntry, 0)}
		err := series.ForEachLabel(func(name, value string) error {
			identifier.Labels = append(identifier.Labels, logproto.SeriesIdentifier_LabelsEntry{Key: name, Value: value})
			return nil
		})
		if err != nil {
			return fmt.Errorf("error stepping through labels of series: %w", err)
		}

		mat.Data = append(mat.Data, identifier)
		return nil
	})
	return mat, err
}

// WriteSeriesResponseViewJSON writes a JSON response to the supplied write that
// is equivalent to marshal.WriteSeriesResponseJSON.
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
