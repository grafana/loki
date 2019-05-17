package chunk

import (
	"sort"
	"unsafe"

	jsoniter "github.com/json-iterator/go"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
)

func init() {
	jsoniter.RegisterTypeDecoderFunc("labels.Labels", decodeLabels)
	jsoniter.RegisterTypeEncoderFunc("labels.Labels", encodeLabels, labelsIsEmpty)
	jsoniter.RegisterTypeDecoderFunc("model.Time", decodeModelTime)
	jsoniter.RegisterTypeEncoderFunc("model.Time", encodeModelTime, modelTimeIsEmpty)
}

// Override Prometheus' labels.Labels decoder which goes via a map
func decodeLabels(ptr unsafe.Pointer, iter *jsoniter.Iterator) {
	labelsPtr := (*labels.Labels)(ptr)
	*labelsPtr = make(labels.Labels, 0, 10)
	iter.ReadMapCB(func(iter *jsoniter.Iterator, key string) bool {
		value := iter.ReadString()
		*labelsPtr = append(*labelsPtr, labels.Label{Name: key, Value: value})
		return true
	})
	// Labels are always sorted, but earlier Cortex using a map would
	// output in any order so we have to sort on read in
	sort.Sort(*labelsPtr)
}

// Override Prometheus' labels.Labels encoder which goes via a map
func encodeLabels(ptr unsafe.Pointer, stream *jsoniter.Stream) {
	labelsPtr := (*labels.Labels)(ptr)
	stream.WriteObjectStart()
	for i, v := range *labelsPtr {
		if i != 0 {
			stream.WriteMore()
		}
		stream.WriteString(v.Name)
		stream.WriteRaw(`:`)
		stream.WriteString(v.Value)
	}
	stream.WriteObjectEnd()
}

func labelsIsEmpty(ptr unsafe.Pointer) bool {
	labelsPtr := (*labels.Labels)(ptr)
	return len(*labelsPtr) == 0
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
