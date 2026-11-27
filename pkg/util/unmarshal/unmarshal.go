package unmarshal

import (
	jsonv1 "encoding/json"
	"encoding/json/v2"

	"github.com/grafana/loki/v3/pkg/loghttp"
)

// WebsocketReader knows how to read message to a websocket connection.
type WebsocketReader interface {
	ReadMessage() (int, []byte, error)
}

// ReadTailResponseJSON unmarshals the loghttp.TailResponse from a websocket reader.
func ReadTailResponseJSON(r *loghttp.TailResponse, reader WebsocketReader) error {
	_, data, err := reader.ReadMessage()
	if err != nil {
		return err
	}
	return json.Unmarshal(data, r, jsonv1.DefaultOptionsV1())
}
