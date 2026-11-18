package loghttp

import (
	"bytes"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/grafana/dskit/httpgrpc"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/querier/plan"
	"github.com/grafana/loki/v3/pkg/util/json"
)

const (
	maxDelayForInTailing = 5
)

// TailResponse represents the http json response to a tail query
type TailResponse struct {
	Streams        []Stream        `json:"streams,omitempty"`
	DroppedStreams []DroppedStream `json:"dropped_entries,omitempty"`
}

// DroppedStream represents a dropped stream in tail call
type DroppedStream struct {
	Timestamp time.Time
	Labels    LabelSet
}

// MarshalJSON implements json.Marshaller
func (s *DroppedStream) MarshalJSON() ([]byte, error) {
	buf := bytes.NewBuffer(make([]byte, 0, 64))
	if err := s.MarshalJSONTo(jsontext.NewEncoder(buf, jsontext.AllowInvalidUTF8(true), jsontext.AllowDuplicateNames(true))); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil // the encoder terminates top-level values with a newline
}

// MarshalJSONTo implements json.MarshalerTo, writing directly to enc.
func (s *DroppedStream) MarshalJSONTo(enc *jsontext.Encoder) error {
	e := json.Latching(enc)
	e.Tokens(jsontext.BeginObject, jsontext.String("timestamp"), jsontext.String(strconv.FormatInt(s.Timestamp.UnixNano(), 10)))
	if len(s.Labels) > 0 {
		e.Tokens(jsontext.String("labels"), jsontext.BeginObject)
		for _, k := range slices.Sorted(maps.Keys(s.Labels)) {
			e.Tokens(jsontext.String(k), jsontext.String(s.Labels[k]))
		}
		e.Tokens(jsontext.EndObject)
	}
	e.Tokens(jsontext.EndObject)
	return e.Err()
}

// UnmarshalJSON implements json.UnMarshaller
func (s *DroppedStream) UnmarshalJSON(data []byte) error {
	d := json.GetDecoder(data)
	defer d.Release()

	var (
		timestamp string
		labels    LabelSet
	)
	err := d.EachField(func(key []byte) error {
		switch string(key) {
		case "timestamp":
			if d.PeekKind() != jsontext.KindString {
				return errors.New("dropped stream: timestamp must be a string")
			}
			var err error
			timestamp, err = d.ReadScalar()
			return err
		case "labels":
			if d.PeekKind() == jsontext.KindNull {
				return d.SkipValue()
			}
			return labels.decode(d)
		default:
			return d.SkipValue()
		}
	})
	if err != nil {
		return err
	}

	t, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return err
	}

	s.Timestamp = time.Unix(0, t)
	s.Labels = labels

	return nil
}

// ParseTailQuery parses a TailRequest request from an http request.
func ParseTailQuery(r *http.Request) (*logproto.TailRequest, error) {
	var err error
	qs := query(r)
	parsed, err := syntax.ParseExpr(qs)
	if err != nil {
		return nil, err
	}
	req := logproto.TailRequest{
		Query: qs,
		Plan: &plan.QueryPlan{
			AST: parsed,
		},
	}

	req.Query, err = parseRegexQuery(r)
	if err != nil {
		return nil, httpgrpc.Errorf(http.StatusBadRequest, "%s", err.Error())
	}

	req.Limit, err = limit(r)
	if err != nil {
		return nil, err
	}

	req.Start, _, err = bounds(r)
	if err != nil {
		return nil, err
	}
	req.DelayFor, err = tailDelay(r)
	if err != nil {
		return nil, err
	}
	if req.DelayFor > maxDelayForInTailing {
		return nil, fmt.Errorf("delay_for can't be greater than %d", maxDelayForInTailing)
	}
	return &req, nil
}
