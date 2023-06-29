package loghttp

import (
	"strconv"
	"time"
	"unsafe"

	"github.com/buger/jsonparser"
	jsoniter "github.com/json-iterator/go"
	"github.com/modern-go/reflect2"

	"github.com/grafana/loki/pkg/logproto"
)

func init() {
	jsoniter.RegisterExtension(&jsonExtension{})
}

// Entry represents a log entry.  It includes a log message and the time it occurred at.
type Entry struct {
	Timestamp time.Time
	Line      string
	Labels    LabelSet
}

func (e Entry) ToProto() logproto.Entry {
	// If there are no labels, we return empty string instead of '{}'.
	var labels string
	if len(e.Labels) > 0 {
		labels = e.Labels.String()
	}

	return logproto.Entry{
		Timestamp: e.Timestamp,
		Line:      e.Line,
		Labels:    labels,
	}
}

func (e *Entry) UnmarshalJSON(data []byte) error {
	var (
		i          int
		parseError error
	)
	_, err := jsonparser.ArrayEach(data, func(value []byte, t jsonparser.ValueType, _ int, _ error) {
		// assert that both items in array are of type string
		switch i {
		case 0: // timestamp
			if t != jsonparser.String {
				parseError = jsonparser.MalformedStringError
				return
			}
			ts, err := jsonparser.ParseInt(value)
			if err != nil {
				parseError = err
				return
			}
			e.Timestamp = time.Unix(0, ts)
		case 1: // value
			if t != jsonparser.String {
				parseError = jsonparser.MalformedStringError
				return
			}
			v, err := jsonparser.ParseString(value)
			if err != nil {
				parseError = err
				return
			}
			e.Line = v
		case 2: // labels
			if t != jsonparser.Object {
				parseError = jsonparser.MalformedObjectError
				return
			}
			e.Labels = make(LabelSet)
			if err := jsonparser.ObjectEach(value, func(key []byte, value []byte, dataType jsonparser.ValueType, _ int) error {
				if dataType != jsonparser.String {
					return jsonparser.MalformedStringError
				}
				e.Labels[yoloString(key)] = yoloString(value)
				return nil
			}); err != nil {
				parseError = err
				return
			}
		}
		i++
	})
	if parseError != nil {
		return parseError
	}
	return err
}

type jsonExtension struct {
	jsoniter.DummyExtension
}

type sliceEntryDecoder struct{}

func (sliceEntryDecoder) Decode(ptr unsafe.Pointer, iter *jsoniter.Iterator) {
	*((*[]Entry)(ptr)) = (*((*[]Entry)(ptr)))[:0]
	iter.ReadArrayCB(func(iter *jsoniter.Iterator) bool {
		i := 0
		var ts time.Time
		var line string
		var labels LabelSet
		ok := iter.ReadArrayCB(func(iter *jsoniter.Iterator) bool {
			var ok bool
			switch i {
			case 0:
				ts, ok = readTimestamp(iter)
				i++
				return ok
			case 1:
				line = iter.ReadString()
				i++
				if iter.Error != nil {
					return false
				}
				return true
			case 2:
				labels = make(LabelSet)
				iter.ReadMapCB(func(iter *jsoniter.Iterator, labelName string) bool {
					labelValue := iter.ReadString()
					labels[labelName] = labelValue
					return true
				})
				i++
				if iter.Error != nil {
					return false
				}
				return true
			default:
				iter.ReportError("error reading entry", "array must contains 2 values")
				return false
			}
		})
		if ok {
			*((*[]Entry)(ptr)) = append(*((*[]Entry)(ptr)), Entry{
				Timestamp: ts,
				Line:      line,
				Labels:    labels,
			})
			return true
		}
		return false
	})
}

func readTimestamp(iter *jsoniter.Iterator) (time.Time, bool) {
	s := iter.ReadString()
	if iter.Error != nil {
		return time.Time{}, false
	}
	t, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		iter.ReportError("error reading entry timestamp", err.Error())
		return time.Time{}, false

	}
	return time.Unix(0, t), true
}

type EntryEncoder struct{}

func (EntryEncoder) IsEmpty(_ unsafe.Pointer) bool {
	// we don't omit-empty with log entries.
	return false
}

func (EntryEncoder) Encode(ptr unsafe.Pointer, stream *jsoniter.Stream) {
	e := *((*Entry)(ptr))
	stream.WriteArrayStart()
	stream.WriteRaw(`"`)
	stream.WriteRaw(strconv.FormatInt(e.Timestamp.UnixNano(), 10))
	stream.WriteRaw(`"`)
	stream.WriteMore()
	stream.WriteStringWithHTMLEscaped(e.Line)
	if len(e.Labels) > 0 {
		stream.WriteMore()
		stream.WriteObjectStart()
		for lName, lValue := range e.Labels {
			stream.WriteObjectField(lName)
			stream.WriteString(lValue)
		}
		stream.WriteObjectEnd()
	}
	stream.WriteArrayEnd()
}

func (e *jsonExtension) CreateDecoder(typ reflect2.Type) jsoniter.ValDecoder {
	if typ == reflect2.TypeOf([]Entry{}) {
		return sliceEntryDecoder{}
	}
	return nil
}

func (e *jsonExtension) CreateEncoder(typ reflect2.Type) jsoniter.ValEncoder {
	if typ == reflect2.TypeOf(Entry{}) {
		return EntryEncoder{}
	}
	return nil
}
