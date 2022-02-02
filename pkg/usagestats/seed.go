package usagestats

import (
	"time"

	jsoniter "github.com/json-iterator/go"
	prom "github.com/prometheus/prometheus/web/api/v1"
)

type ClusterSeed struct {
	UID                    string    `json:"UID"`
	CreatedAt              time.Time `json:"created_at"`
	prom.PrometheusVersion `json:"version"`
}

var JSONCodec = jsonCodec{}

type jsonCodec struct{}

func (jsonCodec) Decode(data []byte) (interface{}, error) {
	var seed ClusterSeed
	if err := jsoniter.ConfigFastest.Unmarshal(data, &seed); err != nil {
		return nil, err
	}
	return &seed, nil
}

func (jsonCodec) Encode(obj interface{}) ([]byte, error) {
	return jsoniter.ConfigFastest.Marshal(obj)
}
func (jsonCodec) CodecID() string { return "usagestats.jsonCodec" }
