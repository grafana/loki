package loghttp

import (
	"bytes"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/util/json"
)

// Entry represents a log entry.  It includes a log message and the time it occurred at.
type Entry struct {
	Timestamp          time.Time
	Line               string
	StructuredMetadata labels.Labels
	Parsed             labels.Labels
}

// UnmarshalJSON decodes an entry from [ "<ts>", "<line>", {structured metadata}? ].
func (e *Entry) UnmarshalJSON(data []byte) error {
	d := json.GetDecoder(data)
	defer d.Release()
	return e.decode(d)
}

// decode reads an entry from the next value in d.
func (e *Entry) decode(d *json.Decoder) error {
	dec := d.Decoder

	if tok, err := dec.ReadToken(); err != nil {
		return err
	} else if tok.Kind() != jsontext.KindBeginArray {
		return errors.New("entry: expected array")
	}
	for i := 0; dec.PeekKind() != jsontext.KindEndArray; i++ {
		switch i {
		case 0: // timestamp
			b, err := d.ReadString("entry: timestamp must be a string")
			if err != nil {
				return err
			}
			ts, err := strconv.ParseInt(string(b), 10, 64)
			if err != nil {
				return err
			}
			e.Timestamp = time.Unix(0, ts)
		case 1: // line
			b, err := d.ReadString("entry: line must be a string")
			if err != nil {
				return err
			}
			e.Line = string(b)
		case 2: // structured metadata
			if err := e.readMetadata(d); err != nil {
				return err
			}
		default:
			if err := dec.SkipValue(); err != nil {
				return err
			}
		}
	}
	_, err := dec.ReadToken() // consume the closing ']'
	return err
}

// readMetadata reads the third element of the entry array.
//
// Here we deserialize entries for both query responses and push requests.
//
// For push requests, we accept structured metadata as the third object in the entry array. E.g.:
// [ "<ts>", "<log line>", {"trace_id": "0242ac120002", "user_id": "superUser123"}]
//
// For query responses, we accept structured metadata and parsed labels in the third object in the entry array. E.g.:
// [ "<ts>", "<log line>", { "structuredMetadata": {"trace_id": "0242ac120002", "user_id": "superUser123"}, "parsed": {"msg": "text"}}]
//
// Therefore, we need to check if the third object contains the "structuredMetadata" or "parsed" fields. If it does,
// we deserialize the inner objects into the structured metadata and parsed labels respectively.
// If it doesn't, we deserialize the object into the structured metadata labels.
func (e *Entry) readMetadata(d *json.Decoder) error {
	if d.PeekKind() != jsontext.KindBeginObject {
		return errors.New("entry: structured metadata must be an object")
	}
	structuredMetadataBuilder := labels.NewScratchBuilder(0)
	var parsed labels.Labels
	err := d.EachField(func(key []byte) error {
		if d.PeekKind() == jsontext.KindBeginObject {
			switch string(key) {
			case "structuredMetadata":
				structuredMetadataBuilder.Reset()
				return readLabelsInto(d, &structuredMetadataBuilder)
			case "parsed":
				parsedBuilder := labels.NewScratchBuilder(0)
				if err := readLabelsInto(d, &parsedBuilder); err != nil {
					return err
				}
				parsedBuilder.Sort()
				parsed = parsedBuilder.Labels()
				return nil
			default:
				return d.SkipValue()
			}
		}
		// Deliberately lenient: any non-object value is kept as its raw text.
		name := string(key) // copy: reading the value reuses the buffer
		val, err := d.ReadScalar()
		if err != nil {
			return fmt.Errorf("could not parse structured metadata or parsed fields: %w", err)
		}
		structuredMetadataBuilder.Add(name, val)
		return nil
	})
	if err != nil {
		return err
	}
	structuredMetadataBuilder.Sort()
	e.StructuredMetadata = structuredMetadataBuilder.Labels()
	e.Parsed = parsed
	return nil
}

// readLabelsInto reads an object whose values are strings or numbers, adding each member to b.
// The caller is responsible for sorting b.
func readLabelsInto(d *json.Decoder, b *labels.ScratchBuilder) error {
	return d.EachField(func(key []byte) error {
		name := string(key) // copy: reading the value reuses the buffer
		if k := d.PeekKind(); k != jsontext.KindString && k != jsontext.KindNumber {
			return fmt.Errorf("could not parse label value. Expected string or number, got %s", k)
		}
		val, err := d.ReadScalar()
		if err != nil {
			return err
		}
		b.Add(name, val)
		return nil
	})
}

// MarshalJSON encodes the entry as [ "<ts>", "<line>", {structured metadata}? ].
// The structured metadata object is omitted when empty.
func (e Entry) MarshalJSON() ([]byte, error) {
	buf := bytes.NewBuffer(make([]byte, 0, len(e.Line)+64))
	// Escape <, > and & (Loki has done this since 2019); replace invalid UTF-8 rather than fail.
	enc := jsontext.NewEncoder(buf, jsontext.EscapeForHTML(true), jsontext.AllowInvalidUTF8(true))
	_ = enc.WriteToken(jsontext.BeginArray)
	_ = enc.WriteToken(jsontext.String(strconv.FormatInt(e.Timestamp.UnixNano(), 10)))
	_ = enc.WriteToken(jsontext.String(e.Line))
	if !e.StructuredMetadata.IsEmpty() {
		_ = enc.WriteToken(jsontext.BeginObject)
		e.StructuredMetadata.Range(func(l labels.Label) {
			_ = enc.WriteToken(jsontext.String(l.Name))
			_ = enc.WriteToken(jsontext.String(l.Value))
		})
		_ = enc.WriteToken(jsontext.EndObject)
	}
	if err := enc.WriteToken(jsontext.EndArray); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil // the encoder terminates top-level values with a newline
}
