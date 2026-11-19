package unmarshal

import (
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/grafana/jsonparser"

	"github.com/grafana/loki/v3/pkg/loghttp"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/util/json"
)

// DecodePushRequest directly decodes json to a logproto.PushRequest
func DecodePushRequest(b io.Reader, r *logproto.PushRequest) error {
	dec := json.NewDecoder(b)

	*r = logproto.PushRequest{}
	if null, err := skipNull(dec); null || err != nil {
		return err
	}
	var streams []logproto.Stream
	err := dec.EachField(func(key []byte) error {
		if string(key) != "streams" {
			// return error when parsing unknown field
			return errors.New("reading PushRequest: found unknown field: " + string(key))
		}
		var err error
		streams, err = decodeStreams(dec)
		return err
	})
	if err != nil {
		return err
	}
	r.Streams = streams
	return nil
}

// skipNull consumes the next value if it is a null, and reports whether it did.
func skipNull(dec *json.Decoder) (bool, error) {
	if dec.PeekKind() != jsontext.KindNull {
		return false, nil
	}
	return true, dec.SkipValue()
}

func decodeStreams(dec *json.Decoder) ([]logproto.Stream, error) {
	if null, err := skipNull(dec); null || err != nil {
		return nil, err
	}
	var streams []logproto.Stream
	err := dec.EachElement(func() error {
		streams = append(streams, logproto.Stream{})
		return decodeStream(dec, &streams[len(streams)-1])
	})
	if err != nil {
		return nil, err
	}
	return streams, nil
}

func decodeStream(dec *json.Decoder, s *logproto.Stream) error {
	if null, err := skipNull(dec); null || err != nil {
		return err
	}
	return dec.EachField(func(key []byte) error {
		var err error
		switch string(key) {
		case "stream":
			s.Labels, err = decodeLabels(dec)
		case "values":
			s.Entries, err = decodeEntries(dec, s.Entries)
		default:
			err = errors.New("reading Stream: found unknown field: " + string(key))
		}
		return err
	})
}

func decodeLabels(dec *json.Decoder) (string, error) {
	if k := dec.PeekKind(); k != jsontext.KindBeginObject {
		return "", fmt.Errorf("reading Stream: stream must be an object (got %s)", k)
	}
	labels := make(loghttp.LabelSet)
	err := dec.EachField(func(key []byte) error {
		name := string(key) // copy: reading the value reuses the buffer
		if dec.PeekKind() == jsontext.KindString {
			val, err := dec.ReadScalar()
			labels[name] = val
			return err
		}
		// Keep the raw text of non-string values (e.g. "null", "5"), for backwards-compatibility.
		val, err := dec.ReadValue()
		labels[name] = string(val)
		return err
	})
	if err != nil {
		return "", err
	}
	return labels.String(), nil
}

func decodeEntries(dec *json.Decoder, entries []logproto.Entry) ([]logproto.Entry, error) {
	if null, err := skipNull(dec); null || err != nil {
		return entries, err
	}
	err := dec.EachElement(func() error {
		if null, err := skipNull(dec); null || err != nil {
			return err
		}
		entries = append(entries, logproto.Entry{})
		return decodeEntry(dec, &entries[len(entries)-1])
	})
	if err != nil {
		return nil, err
	}
	return entries, nil
}

// decodeEntry reads a [timestamp, line, structuredMetadata?] array into e.
func decodeEntry(dec *json.Decoder, e *logproto.Entry) error {
	if k := dec.PeekKind(); k != jsontext.KindBeginArray {
		return fmt.Errorf("reading Entry: expected array (got %s)", k)
	}
	i := 0
	return dec.EachElement(func() error {
		n := i
		i++
		switch n {
		case 0: // timestamp
			b, err := dec.ReadString("reading Entry: timestamp must be a string")
			if err != nil {
				return err
			}
			ts, err := jsonparser.ParseInt(b)
			if err != nil {
				return fmt.Errorf("reading Entry: %w", err)
			}
			e.Timestamp = time.Unix(0, ts)
		case 1: // line
			b, err := dec.ReadString("reading Entry: line must be a string")
			if err != nil {
				return err
			}
			e.Line = string(b)
		case 2: // structuredMetadata
			if k := dec.PeekKind(); k != jsontext.KindBeginObject {
				return fmt.Errorf("reading Entry: structured metadata must be an object (got %s)", k)
			}
			return dec.EachField(func(key []byte) error {
				name := string(key) // copy: reading the value reuses the buffer
				b, err := dec.ReadString("reading Entry: structured metadata value must be a string")
				if err != nil {
					return err
				}
				e.StructuredMetadata = append(e.StructuredMetadata, logproto.LabelAdapter{Name: name, Value: string(b)})
				return nil
			})
		default:
			return dec.SkipValue()
		}
		return nil
	})
}
