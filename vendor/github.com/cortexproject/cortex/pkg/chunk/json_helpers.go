package chunk

import (
	"unsafe"

	jsoniter "github.com/json-iterator/go"
	"github.com/prometheus/common/model"
)

func init() {
	jsoniter.RegisterTypeDecoderFunc("model.Metric", decodeMetric)
	jsoniter.RegisterTypeDecoderFunc("model.Time", decodeModelTime)
	jsoniter.RegisterTypeEncoderFunc("model.Time", encodeModelTime, modelTimeIsEmpty)
}

// decoding model.Metric via ReadMapCB is faster than the generic jsoniter
// decoder because the latter allocates memory for each string via reflect.
func decodeMetric(ptr unsafe.Pointer, iter *jsoniter.Iterator) {
	mapPtr := (*model.Metric)(ptr)
	*mapPtr = make(model.Metric, 10)
	iter.ReadMapCB(func(iter *jsoniter.Iterator, key string) bool {
		value := iter.ReadString()
		(*mapPtr)[model.LabelName(key)] = model.LabelValue(value)
		return true
	})
}

// Decode via jsoniter's float64 routine is faster than getting the string data and decoding as two integers
func decodeModelTime(ptr unsafe.Pointer, iter *jsoniter.Iterator) {
	pt := (*model.Time)(ptr)
	f := iter.ReadFloat64()
	*pt = model.Time(int64(f * 1000))
}

// Write out the timestamp as an int divided by 1000. This is ~3x faster than converting to a float.
// Adapted from https://github.com/prometheus/prometheus/blob/cc39021b2bb6f829c7a626e4bdce2f338d1b76db/web/api/v1/api.go#L829
func encodeModelTime(ptr unsafe.Pointer, stream *jsoniter.Stream) {
	pt := (*model.Time)(ptr)
	t := int64(*pt)
	if t < 0 {
		stream.WriteRaw(`-`)
		t = -t
	}
	stream.WriteInt64(t / 1000)
	fraction := t % 1000
	if fraction != 0 {
		stream.WriteRaw(`.`)
		if fraction < 100 {
			stream.WriteRaw(`0`)
		}
		if fraction < 10 {
			stream.WriteRaw(`0`)
		}
		stream.WriteInt64(fraction)
	}
}

func modelTimeIsEmpty(ptr unsafe.Pointer) bool {
	return false
}
