package request

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/grafana/loki/pkg/logproto"
)

func ParseLabelQuery(r *http.Request) (*logproto.LabelRequest, error) {
	name, ok := mux.Vars(r)["name"]
	req := &logproto.LabelRequest{
		Values: ok,
		Name:   name,
	}

	start, end, err := bounds(r)
	if err != nil {
		return nil, err
	}
	req.Start = &start
	req.End = &end
	return req, nil
}
