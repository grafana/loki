// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2021 Datadog, Inc.

package ddsketch

import (
	"errors"
	"io"
	"math"

	enc "github.com/DataDog/sketches-go/ddsketch/encoding"
	"github.com/DataDog/sketches-go/ddsketch/mapping"
	"github.com/DataDog/sketches-go/ddsketch/pb/sketchpb"
	"github.com/DataDog/sketches-go/ddsketch/stat"
	"github.com/DataDog/sketches-go/ddsketch/store"
)

var (
	ErrUntrackableNaN     = errors.New("input value is NaN and cannot be tracked by the sketch")
	ErrUntrackableTooLow  = errors.New("input value is too low and cannot be tracked by the sketch")
	ErrUntrackableTooHigh = errors.New("input value is too high and cannot be tracked by the sketch")
	ErrNegativeCount      = errors.New("count cannot be negative")
	errEmptySketch        = errors.New("no such element exists")
	errUnknownFlag        = errors.New("unknown encoding flag")
)

// Unexported to prevent usage and avoid the cost of dynamic dispatch
type quantileSketch interface {
	RelativeAccuracy() float64
	IsEmpty() bool
	GetCount() float64
	GetZeroCount() float64
	GetSum() float64
	GetPositiveValueStore() store.Store
	GetNegativeValueStore() store.Store
	GetMinValue() (float64, error)
	GetMaxValue() (float64, error)
	GetValueAtQuantile(quantile float64) (float64, error)
	GetValuesAtQuantiles(quantiles []float64) ([]float64, error)
	ForEach(f func(value, count float64) (stop bool))
	Add(value float64) error
	AddWithCount(value, count float64) error
	// MergeWith
	// ChangeMapping
	Reweight(factor float64) error
	Clear()
	// Copy
	Encode(b *[]byte, omitIndexMapping bool)
	DecodeAndMergeWith(b []byte) error
}

var _ quantileSketch = (*DDSketch)(nil)
var _ quantileSketch = (*DDSketchWithExactSummaryStatistics)(nil)

type DDSketch struct {
	mapping.IndexMapping
	positiveValueStore store.Store
	negativeValueStore store.Store
	zeroCount          float64
}

func NewDDSketchFromStoreProvider(indexMapping mapping.IndexMapping, storeProvider store.Provider) *DDSketch {
	return NewDDSketch(indexMapping, storeProvider(), storeProvider())
}

func NewDDSketch(indexMapping mapping.IndexMapping, positiveValueStore store.Store, negativeValueStore store.Store) *DDSketch {
	return &DDSketch{
		IndexMapping:       indexMapping,
		positiveValueStore: positiveValueStore,
		negativeValueStore: negativeValueStore,
	}
}

func NewDefaultDDSketch(relativeAccuracy float64) (*DDSketch, error) {
	m, err := mapping.NewDefaultMapping(relativeAccuracy)
	if err != nil {
		return nil, err
	}
	return NewDDSketchFromStoreProvider(m, store.DefaultProvider), nil
}

// Constructs an instance of DDSketch that offers constant-time insertion and whose size grows indefinitely
// to accommodate for the range of input values.
func LogUnboundedDenseDDSketch(relativeAccuracy float64) (*DDSketch, error) {
	indexMapping, err := mapping.NewLogarithmicMapping(relativeAccuracy)
	if err != nil {
		return nil, err
	}
	return NewDDSketch(indexMapping, store.NewDenseStore(), store.NewDenseStore()), nil
}

// Constructs an instance of DDSketch that offers constant-time insertion and whose size grows until the
// maximum number of bins is reached, at which point bins with lowest indices are collapsed, which causes the
// relative accuracy guarantee to be lost on lowest quantiles if values are all positive, or the mid-range
// quantiles for values closest to zero if values include negative numbers.
func LogCollapsingLowestDenseDDSketch(relativeAccuracy float64, maxNumBins int) (*DDSketch, error) {
	indexMapping, err := mapping.NewLogarithmicMapping(relativeAccuracy)
	if err != nil {
		return nil, err
	}
	return NewDDSketch(indexMapping, store.NewCollapsingLowestDenseStore(maxNumBins), store.NewCollapsingLowestDenseStore(maxNumBins)), nil
}

// Constructs an instance of DDSketch that offers constant-time insertion and whose size grows until the
// maximum number of bins is reached, at which point bins with highest indices are collapsed, which causes the
// relative accuracy guarantee to be lost on highest quantiles if values are all positive, or the lowest and
// highest quantiles if values include negative numbers.
func LogCollapsingHighestDenseDDSketch(relativeAccuracy float64, maxNumBins int) (*DDSketch, error) {
	indexMapping, err := mapping.NewLogarithmicMapping(relativeAccuracy)
	if err != nil {
		return nil, err
	}
	return NewDDSketch(indexMapping, store.NewCollapsingHighestDenseStore(maxNumBins), store.NewCollapsingHighestDenseStore(maxNumBins)), nil
}

// Adds a value to the sketch.
func (s *DDSketch) Add(value float64) error {
	return s.AddWithCount(value, float64(1))
}

// Adds a value to the sketch with a float64 count.
func (s *DDSketch) AddWithCount(value, count float64) error {
	if count < 0 {
		return ErrNegativeCount
	}

	if value > s.MinIndexableValue() {
		if value > s.MaxIndexableValue() {
			return ErrUntrackableTooHigh
		}
		s.positiveValueStore.AddWithCount(s.Index(value), count)
	} else if value < -s.MinIndexableValue() {
		if value < -s.MaxIndexableValue() {
			return ErrUntrackableTooLow
		}
		s.negativeValueStore.AddWithCount(s.Index(-value), count)
	} else if math.IsNaN(value) {
		return ErrUntrackableNaN
	} else {
		s.zeroCount += count
	}
	return nil
}

// Return a (deep) copy of this sketch.
func (s *DDSketch) Copy() *DDSketch {
	return &DDSketch{
		IndexMapping:       s.IndexMapping,
		positiveValueStore: s.positiveValueStore.Copy(),
		negativeValueStore: s.negativeValueStore.Copy(),
		zeroCount:          s.zeroCount,
	}
}

// Clear empties the sketch while allowing reusing already allocated memory.
func (s *DDSketch) Clear() {
	s.positiveValueStore.Clear()
	s.negativeValueStore.Clear()
	s.zeroCount = 0
}

// Return the value at the specified quantile. Return a non-nil error if the quantile is invalid
// or if the sketch is empty.
func (s *DDSketch) GetValueAtQuantile(quantile float64) (float64, error) {
	if quantile < 0 || quantile > 1 {
		return math.NaN(), errors.New("The quantile must be between 0 and 1.")
	}

	count := s.GetCount()
	if count == 0 {
		return math.NaN(), errEmptySketch
	}

	// Use an explicit floating point conversion (as per Go specification) to make sure that no
	// "fused multiply and add" (FMA) operation is used in the following code subtracting values
	// from `rank`. Not doing so can lead to inconsistent rounding and return value for this
	// function, depending on the architecture and whether FMA operations are used or not by the
	// compiler.
	rank := float64(quantile * (count - 1))

	negativeValueCount := s.negativeValueStore.TotalCount()
	if rank < negativeValueCount {
		return -s.Value(s.negativeValueStore.KeyAtRank(negativeValueCount - 1 - rank)), nil
	} else if rank < s.zeroCount+negativeValueCount {
		return 0, nil
	} else {
		return s.Value(s.positiveValueStore.KeyAtRank(rank - s.zeroCount - negativeValueCount)), nil
	}
}

// Return the values at the respective specified quantiles. Return a non-nil error if any of the quantiles
// is invalid or if the sketch is empty.
func (s *DDSketch) GetValuesAtQuantiles(quantiles []float64) ([]float64, error) {
	values := make([]float64, len(quantiles))
	for i, q := range quantiles {
		val, err := s.GetValueAtQuantile(q)
		if err != nil {
			return nil, err
		}
		values[i] = val
	}
	return values, nil
}

// Return the total number of values that have been added to this sketch.
func (s *DDSketch) GetCount() float64 {
	return s.zeroCount + s.positiveValueStore.TotalCount() + s.negativeValueStore.TotalCount()
}

// GetZeroCount returns the number of zero values that have been added to this sketch.
// Note: values that are very small (lower than MinIndexableValue if positive, or higher than -MinIndexableValue if negative)
// are also mapped to the zero bucket.
func (s *DDSketch) GetZeroCount() float64 {
	return s.zeroCount
}

// Return true iff no value has been added to this sketch.
func (s *DDSketch) IsEmpty() bool {
	return s.zeroCount == 0 && s.positiveValueStore.IsEmpty() && s.negativeValueStore.IsEmpty()
}

// Return the maximum value that has been added to this sketch. Return a non-nil error if the sketch
// is empty.
func (s *DDSketch) GetMaxValue() (float64, error) {
	if !s.positiveValueStore.IsEmpty() {
		maxIndex, _ := s.positiveValueStore.MaxIndex()
		return s.Value(maxIndex), nil
	} else if s.zeroCount > 0 {
		return 0, nil
	} else {
		minIndex, err := s.negativeValueStore.MinIndex()
		if err != nil {
			return math.NaN(), err
		}
		return -s.Value(minIndex), nil
	}
}

// Return the minimum value that has been added to this sketch. Returns a non-nil error if the sketch
// is empty.
func (s *DDSketch) GetMinValue() (float64, error) {
	if !s.negativeValueStore.IsEmpty() {
		maxIndex, _ := s.negativeValueStore.MaxIndex()
		return -s.Value(maxIndex), nil
	} else if s.zeroCount > 0 {
		return 0, nil
	} else {
		minIndex, err := s.positiveValueStore.MinIndex()
		if err != nil {
			return math.NaN(), err
		}
		return s.Value(minIndex), nil
	}
}

// GetSum returns an approximation of the sum of the values that have been added to the sketch. If the
// values that have been added to the sketch all have the same sign, the approximation error has
// the relative accuracy guarantees of the mapping used for this sketch.
func (s *DDSketch) GetSum() (sum float64) {
	s.ForEach(func(value float64, count float64) (stop bool) {
		sum += value * count
		return false
	})
	return sum
}

// GetPositiveValueStore returns the store.Store object that contains the positive
// values of the sketch.
func (s *DDSketch) GetPositiveValueStore() store.Store {
	return s.positiveValueStore
}

// GetNegativeValueStore returns the store.Store object that contains the negative
// values of the sketch.
func (s *DDSketch) GetNegativeValueStore() store.Store {
	return s.negativeValueStore
}

// ForEach applies f on the bins of the sketches until f returns true.
// There is no guarantee on the bin iteration order.
func (s *DDSketch) ForEach(f func(value, count float64) (stop bool)) {
	if s.zeroCount != 0 && f(0, s.zeroCount) {
		return
	}
	stopped := false
	s.positiveValueStore.ForEach(func(index int, count float64) bool {
		stopped = f(s.IndexMapping.Value(index), count)
		return stopped
	})
	if stopped {
		return
	}
	s.negativeValueStore.ForEach(func(index int, count float64) bool {
		return f(-s.IndexMapping.Value(index), count)
	})
}

// Merges the other sketch into this one. After this operation, this sketch encodes the values that
// were added to both this and the other sketches.
func (s *DDSketch) MergeWith(other *DDSketch) error {
	if !s.IndexMapping.Equals(other.IndexMapping) {
		return errors.New("Cannot merge sketches with different index mappings.")
	}
	s.positiveValueStore.MergeWith(other.positiveValueStore)
	s.negativeValueStore.MergeWith(other.negativeValueStore)
	s.zeroCount += other.zeroCount
	return nil
}

// Generates a protobuf representation of this DDSketch.
func (s *DDSketch) ToProto() *sketchpb.DDSketch {
	return &sketchpb.DDSketch{
		Mapping:        s.IndexMapping.ToProto(),
		PositiveValues: s.positiveValueStore.ToProto(),
		NegativeValues: s.negativeValueStore.ToProto(),
		ZeroCount:      s.zeroCount,
	}
}

func (s *DDSketch) EncodeProto(w io.Writer) {
	builder := sketchpb.NewDDSketchBuilder(w)

	builder.SetMapping(func(indexMappingBuilder *sketchpb.IndexMappingBuilder) {
		s.IndexMapping.EncodeProto(indexMappingBuilder)
	})

	builder.SetZeroCount(s.zeroCount)
	builder.SetNegativeValues(func(storeBuilder *sketchpb.StoreBuilder) {
		s.negativeValueStore.EncodeProto(storeBuilder)
	})

	builder.SetPositiveValues(func(storeBuilder *sketchpb.StoreBuilder) {
		s.positiveValueStore.EncodeProto(storeBuilder)
	})
}

// FromProto builds a new instance of DDSketch based on the provided protobuf representation, using a Dense store.
func FromProto(pb *sketchpb.DDSketch) (*DDSketch, error) {
	return FromProtoWithStoreProvider(pb, store.DenseStoreConstructor)
}

func FromProtoWithStoreProvider(pb *sketchpb.DDSketch, storeProvider store.Provider) (*DDSketch, error) {
	positiveValueStore := storeProvider()
	if pb.PositiveValues != nil {
		store.MergeWithProto(positiveValueStore, pb.PositiveValues)
	}
	negativeValueStore := storeProvider()
	if pb.NegativeValues != nil {
		store.MergeWithProto(negativeValueStore, pb.NegativeValues)
	}
	m, err := mapping.FromProto(pb.Mapping)
	if err != nil {
		return nil, err
	}
	return &DDSketch{
		IndexMapping:       m,
		positiveValueStore: positiveValueStore,
		negativeValueStore: negativeValueStore,
		zeroCount:          pb.ZeroCount,
	}, nil
}

// Encode serializes the sketch and appends the serialized content to the provided []byte.
// If the capacity of the provided []byte is large enough, Encode does not allocate memory space.
// When the index mapping is known at the time of deserialization, omitIndexMapping can be set to true to avoid encoding it and to make the serialized content smaller.
// The encoding format is described in the encoding/flag module.
func (s *DDSketch) Encode(b *[]byte, omitIndexMapping bool) {
	if s.zeroCount != 0 {
		enc.EncodeFlag(b, enc.FlagZeroCountVarFloat)
		enc.EncodeVarfloat64(b, s.zeroCount)
	}

	if !omitIndexMapping {
		s.IndexMapping.Encode(b)
	}

	s.positiveValueStore.Encode(b, enc.FlagTypePositiveStore)
	s.negativeValueStore.Encode(b, enc.FlagTypeNegativeStore)
}

// DecodeDDSketch deserializes a sketch.
// Stores are built using storeProvider. The store type needs not match the
// store that the serialized sketch initially used. However, using the same
// store type may make decoding faster. In the absence of high performance
// requirements, store.BufferedPaginatedStoreConstructor is a sound enough
// choice of store provider.
// To avoid memory allocations, it is possible to use a store provider that
// reuses stores, by calling Clear() on previously used stores before providing
// the store.
// If the serialized data does not contain the index mapping, you need to
// specify the index mapping that was used in the sketch that was encoded.
// Otherwise, you can use nil and the index mapping will be decoded from the
// serialized data.
// It is possible to decode with this function an encoded
// DDSketchWithExactSummaryStatistics, but the exact summary statistics will be
// lost.
func DecodeDDSketch(b []byte, storeProvider store.Provider, indexMapping mapping.IndexMapping) (*DDSketch, error) {
	s := &DDSketch{
		IndexMapping:       indexMapping,
		positiveValueStore: storeProvider(),
		negativeValueStore: storeProvider(),
		zeroCount:          float64(0),
	}
	err := s.DecodeAndMergeWith(b)
	return s, err
}

// DecodeAndMergeWith deserializes a sketch and merges its content in the
// receiver sketch.
// If the serialized content contains an index mapping that differs from the one
// of the receiver, DecodeAndMergeWith returns an error.
func (s *DDSketch) DecodeAndMergeWith(bb []byte) error {
	return s.decodeAndMergeWith(bb, func(b *[]byte, flag enc.Flag) error {
		switch flag {
		case enc.FlagCount, enc.FlagSum, enc.FlagMin, enc.FlagMax:
			// Exact summary stats are ignored.
			if len(*b) < 8 {
				return io.EOF
			}
			*b = (*b)[8:]
			return nil
		default:
			return errUnknownFlag
		}
	})
}

func (s *DDSketch) decodeAndMergeWith(bb []byte, fallbackDecode func(b *[]byte, flag enc.Flag) error) error {
	b := &bb
	for len(*b) > 0 {
		flag, err := enc.DecodeFlag(b)
		if err != nil {
			return err
		}
		switch flag.Type() {
		case enc.FlagTypePositiveStore:
			s.positiveValueStore.DecodeAndMergeWith(b, flag.SubFlag())
		case enc.FlagTypeNegativeStore:
			s.negativeValueStore.DecodeAndMergeWith(b, flag.SubFlag())
		case enc.FlagTypeIndexMapping:
			decodedIndexMapping, err := mapping.Decode(b, flag)
			if err != nil {
				return err
			}
			if s.IndexMapping != nil && !s.IndexMapping.Equals(decodedIndexMapping) {
				return errors.New("index mapping mismatch")
			}
			s.IndexMapping = decodedIndexMapping
		default:
			switch flag {

			case enc.FlagZeroCountVarFloat:
				decodedZeroCount, err := enc.DecodeVarfloat64(b)
				if err != nil {
					return err
				}
				s.zeroCount += decodedZeroCount

			default:
				err := fallbackDecode(b, flag)
				if err != nil {
					return err
				}
			}
		}
	}

	if s.IndexMapping == nil {
		return errors.New("missing index mapping")
	}
	return nil
}

// ChangeMapping changes the store to a new mapping.
// it doesn't change s but returns a newly created sketch.
// positiveStore and negativeStore must be different stores, and be empty when the function is called.
// It is not the conversion that minimizes the loss in relative
// accuracy, but it avoids artefacts like empty bins that make the histograms look bad.
// scaleFactor allows to scale out / in all values. (changing units for eg)
func (s *DDSketch) ChangeMapping(newMapping mapping.IndexMapping, positiveStore store.Store, negativeStore store.Store, scaleFactor float64) *DDSketch {
	if scaleFactor == 1 && s.IndexMapping.Equals(newMapping) {
		return s.Copy()
	}
	changeStoreMapping(s.IndexMapping, newMapping, s.positiveValueStore, positiveStore, scaleFactor)
	changeStoreMapping(s.IndexMapping, newMapping, s.negativeValueStore, negativeStore, scaleFactor)
	newSketch := NewDDSketch(newMapping, positiveStore, negativeStore)
	newSketch.zeroCount = s.zeroCount
	return newSketch
}

func changeStoreMapping(oldMapping, newMapping mapping.IndexMapping, oldStore, newStore store.Store, scaleFactor float64) {
	oldStore.ForEach(func(index int, count float64) (stop bool) {
		inLowerBound := oldMapping.LowerBound(index) * scaleFactor
		inHigherBound := oldMapping.LowerBound(index+1) * scaleFactor
		inSize := inHigherBound - inLowerBound
		for outIndex := newMapping.Index(inLowerBound); newMapping.LowerBound(outIndex) < inHigherBound; outIndex++ {
			outLowerBound := newMapping.LowerBound(outIndex)
			outHigherBound := newMapping.LowerBound(outIndex + 1)
			lowerIntersectionBound := math.Max(outLowerBound, inLowerBound)
			higherIntersectionBound := math.Min(outHigherBound, inHigherBound)
			intersectionSize := higherIntersectionBound - lowerIntersectionBound
			proportion := intersectionSize / inSize
			newStore.AddWithCount(outIndex, proportion*count)
		}
		return false
	})
}

// Reweight multiplies all values from the sketch by w, but keeps the same global distribution.
// w has to be strictly greater than 0.
func (s *DDSketch) Reweight(w float64) error {
	if w <= 0 {
		return errors.New("can't reweight by a negative factor")
	}
	if w == 1 {
		return nil
	}
	s.zeroCount *= w
	if err := s.positiveValueStore.Reweight(w); err != nil {
		return err
	}
	if err := s.negativeValueStore.Reweight(w); err != nil {
		return err
	}
	return nil
}

// DDSketchWithExactSummaryStatistics returns exact count, sum, min and max, as
// opposed to DDSketch, which may return approximate values for those
// statistics. Because of the need to track them exactly, adding and merging
// operations are slightly more exepensive than those of DDSketch.
type DDSketchWithExactSummaryStatistics struct {
	*DDSketch
	summaryStatistics *stat.SummaryStatistics
}

func NewDefaultDDSketchWithExactSummaryStatistics(relativeAccuracy float64) (*DDSketchWithExactSummaryStatistics, error) {
	sketch, err := NewDefaultDDSketch(relativeAccuracy)
	if err != nil {
		return nil, err
	}
	return &DDSketchWithExactSummaryStatistics{
		DDSketch:          sketch,
		summaryStatistics: stat.NewSummaryStatistics(),
	}, nil
}

func NewDDSketchWithExactSummaryStatistics(mapping mapping.IndexMapping, storeProvider store.Provider) *DDSketchWithExactSummaryStatistics {
	return &DDSketchWithExactSummaryStatistics{
		DDSketch:          NewDDSketchFromStoreProvider(mapping, storeProvider),
		summaryStatistics: stat.NewSummaryStatistics(),
	}
}

// NewDDSketchWithExactSummaryStatisticsFromData constructs DDSketchWithExactSummaryStatistics from the provided sketch and exact summary statistics.
func NewDDSketchWithExactSummaryStatisticsFromData(sketch *DDSketch, summaryStatistics *stat.SummaryStatistics) (*DDSketchWithExactSummaryStatistics, error) {
	if sketch.IsEmpty() != (summaryStatistics.Count() == 0) {
		return nil, errors.New("sketch and summary statistics do not match")
	}
	return &DDSketchWithExactSummaryStatistics{
		DDSketch:          sketch,
		summaryStatistics: summaryStatistics,
	}, nil
}

func (s *DDSketchWithExactSummaryStatistics) IsEmpty() bool {
	return s.summaryStatistics.Count() == 0
}

func (s *DDSketchWithExactSummaryStatistics) GetCount() float64 {
	return s.summaryStatistics.Count()
}

// GetZeroCount returns the number of zero values that have been added to this sketch.
// Note: values that are very small (lower than MinIndexableValue if positive, or higher than -MinIndexableValue if negative)
// are also mapped to the zero bucket.
func (s *DDSketchWithExactSummaryStatistics) GetZeroCount() float64 {
	return s.DDSketch.zeroCount
}

func (s *DDSketchWithExactSummaryStatistics) GetSum() float64 {
	return s.summaryStatistics.Sum()
}

// GetPositiveValueStore returns the store.Store object that contains the positive
// values of the sketch.
func (s *DDSketchWithExactSummaryStatistics) GetPositiveValueStore() store.Store {
	return s.DDSketch.positiveValueStore
}

// GetNegativeValueStore returns the store.Store object that contains the negative
// values of the sketch.
func (s *DDSketchWithExactSummaryStatistics) GetNegativeValueStore() store.Store {
	return s.DDSketch.negativeValueStore
}

func (s *DDSketchWithExactSummaryStatistics) GetMinValue() (float64, error) {
	if s.DDSketch.IsEmpty() {
		return math.NaN(), errEmptySketch
	}
	return s.summaryStatistics.Min(), nil
}

func (s *DDSketchWithExactSummaryStatistics) GetMaxValue() (float64, error) {
	if s.DDSketch.IsEmpty() {
		return math.NaN(), errEmptySketch
	}
	return s.summaryStatistics.Max(), nil
}

func (s *DDSketchWithExactSummaryStatistics) GetValueAtQuantile(quantile float64) (float64, error) {
	value, err := s.DDSketch.GetValueAtQuantile(quantile)
	min := s.summaryStatistics.Min()
	if value < min {
		return min, err
	}
	max := s.summaryStatistics.Max()
	if value > max {
		return max, err
	}
	return value, err
}

func (s *DDSketchWithExactSummaryStatistics) GetValuesAtQuantiles(quantiles []float64) ([]float64, error) {
	values, err := s.DDSketch.GetValuesAtQuantiles(quantiles)
	min := s.summaryStatistics.Min()
	max := s.summaryStatistics.Max()
	for i := range values {
		if values[i] < min {
			values[i] = min
		} else if values[i] > max {
			values[i] = max
		}
	}
	return values, err
}

func (s *DDSketchWithExactSummaryStatistics) ForEach(f func(value, count float64) (stop bool)) {
	s.DDSketch.ForEach(f)
}

func (s *DDSketchWithExactSummaryStatistics) Clear() {
	s.DDSketch.Clear()
	s.summaryStatistics.Clear()
}

func (s *DDSketchWithExactSummaryStatistics) Add(value float64) error {
	err := s.DDSketch.Add(value)
	if err != nil {
		return err
	}
	s.summaryStatistics.Add(value, 1)
	return nil
}

func (s *DDSketchWithExactSummaryStatistics) AddWithCount(value, count float64) error {
	if count == 0 {
		return nil
	}
	err := s.DDSketch.AddWithCount(value, count)
	if err != nil {
		return err
	}
	s.summaryStatistics.Add(value, count)
	return nil
}

func (s *DDSketchWithExactSummaryStatistics) MergeWith(o *DDSketchWithExactSummaryStatistics) error {
	err := s.DDSketch.MergeWith(o.DDSketch)
	if err != nil {
		return err
	}
	s.summaryStatistics.MergeWith(o.summaryStatistics)
	return nil
}

func (s *DDSketchWithExactSummaryStatistics) Copy() *DDSketchWithExactSummaryStatistics {
	return &DDSketchWithExactSummaryStatistics{
		DDSketch:          s.DDSketch.Copy(),
		summaryStatistics: s.summaryStatistics.Copy(),
	}
}

func (s *DDSketchWithExactSummaryStatistics) Reweight(factor float64) error {
	err := s.DDSketch.Reweight(factor)
	if err != nil {
		return err
	}
	s.summaryStatistics.Reweight(factor)
	return nil
}

func (s *DDSketchWithExactSummaryStatistics) ChangeMapping(newMapping mapping.IndexMapping, storeProvider store.Provider, scaleFactor float64) *DDSketchWithExactSummaryStatistics {
	summaryStatisticsCopy := s.summaryStatistics.Copy()
	summaryStatisticsCopy.Rescale(scaleFactor)
	return &DDSketchWithExactSummaryStatistics{
		DDSketch:          s.DDSketch.ChangeMapping(newMapping, storeProvider(), storeProvider(), scaleFactor),
		summaryStatistics: summaryStatisticsCopy,
	}
}

func (s *DDSketchWithExactSummaryStatistics) Encode(b *[]byte, omitIndexMapping bool) {
	if s.summaryStatistics.Count() != 0 {
		enc.EncodeFlag(b, enc.FlagCount)
		enc.EncodeVarfloat64(b, s.summaryStatistics.Count())
	}
	if s.summaryStatistics.Sum() != 0 {
		enc.EncodeFlag(b, enc.FlagSum)
		enc.EncodeFloat64LE(b, s.summaryStatistics.Sum())
	}
	if s.summaryStatistics.Min() != math.Inf(1) {
		enc.EncodeFlag(b, enc.FlagMin)
		enc.EncodeFloat64LE(b, s.summaryStatistics.Min())
	}
	if s.summaryStatistics.Max() != math.Inf(-1) {
		enc.EncodeFlag(b, enc.FlagMax)
		enc.EncodeFloat64LE(b, s.summaryStatistics.Max())
	}
	s.DDSketch.Encode(b, omitIndexMapping)
}

// DecodeDDSketchWithExactSummaryStatistics deserializes a sketch.
// Stores are built using storeProvider. The store type needs not match the
// store that the serialized sketch initially used. However, using the same
// store type may make decoding faster. In the absence of high performance
// requirements, store.DefaultProvider is a sound enough choice of store
// provider.
// To avoid memory allocations, it is possible to use a store provider that
// reuses stores, by calling Clear() on previously used stores before providing
// the store.
// If the serialized data does not contain the index mapping, you need to
// specify the index mapping that was used in the sketch that was encoded.
// Otherwise, you can use nil and the index mapping will be decoded from the
// serialized data.
// It is not possible to decode with this function an encoded DDSketch (unless
// it is empty), because it does not track exact summary statistics
func DecodeDDSketchWithExactSummaryStatistics(b []byte, storeProvider store.Provider, indexMapping mapping.IndexMapping) (*DDSketchWithExactSummaryStatistics, error) {
	s := &DDSketchWithExactSummaryStatistics{
		DDSketch: &DDSketch{
			IndexMapping:       indexMapping,
			positiveValueStore: storeProvider(),
			negativeValueStore: storeProvider(),
			zeroCount:          float64(0),
		},
		summaryStatistics: stat.NewSummaryStatistics(),
	}
	err := s.DecodeAndMergeWith(b)
	return s, err
}

func (s *DDSketchWithExactSummaryStatistics) DecodeAndMergeWith(bb []byte) error {
	err := s.DDSketch.decodeAndMergeWith(bb, func(b *[]byte, flag enc.Flag) error {
		switch flag {
		case enc.FlagCount:
			count, err := enc.DecodeVarfloat64(b)
			if err != nil {
				return err
			}
			s.summaryStatistics.AddToCount(count)
			return nil
		case enc.FlagSum:
			sum, err := enc.DecodeFloat64LE(b)
			if err != nil {
				return err
			}
			s.summaryStatistics.AddToSum(sum)
			return nil
		case enc.FlagMin, enc.FlagMax:
			stat, err := enc.DecodeFloat64LE(b)
			if err != nil {
				return err
			}
			s.summaryStatistics.Add(stat, 0)
			return nil
		default:
			return errUnknownFlag
		}
	})
	if err != nil {
		return err
	}
	// It is assumed that if the count is encoded, other exact summary
	// statistics are encoded as well, which is the case if Encode is used.
	if s.summaryStatistics.Count() == 0 && !s.DDSketch.IsEmpty() {
		return errors.New("missing exact summary statistics")
	}
	return nil
}
