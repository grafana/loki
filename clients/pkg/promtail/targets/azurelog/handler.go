package azurelog

import (
	"bytes"
	"encoding/json"
	"strings"
	"time"

	"github.com/Shopify/sarama"
	"github.com/go-kit/log/level"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/model/relabel"

	"github.com/grafana/loki/clients/pkg/promtail/api"
	"github.com/grafana/loki/clients/pkg/promtail/targets/kafka"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/util"
)

type eventHubMessage struct {
	Records []any `json:"records"`
}

func messageHandler(message *sarama.ConsumerMessage, t kafka.KafkaTarget) {
	defer t.Session().MarkMessage(message, "")

	mk := string(message.Key)
	if len(mk) == 0 {
		mk = defaultKafkaMessageKey
	}

	// TODO: Possibly need to format after merging with discovered labels because we can specify multiple labels in source labels
	// https://github.com/grafana/loki/pull/4745#discussion_r750022234
	lbs := format([]labels.Label{{
		Name:  labelKeyKafkaMessageKey,
		Value: mk,
	}}, t.RelabelConfig())

	out := t.Labels().Clone()
	if len(lbs) > 0 {
		out = out.Merge(lbs)
	}

	// fix json as mentioned here:
	// https://learn.microsoft.com/en-us/answers/questions/1001797/invalid-json-logs-produced-for-function-apps?fbclid=IwAR3pK8Nj60GFBtKemqwfpiZyf3rerjowPH_j_qIuNrw_uLDesYvC4mTkfgs
	body := bytes.ReplaceAll(message.Value, []byte(`'`), []byte(`"`))

	data := &eventHubMessage{}
	err := json.Unmarshal(body, data)
	if err != nil {
		level.Debug(t.Logger()).Log("msg", "error when unmarshalling", "message", string(body), "err", err)
		return
	}

	for _, m := range data.Records {
		b, err := json.Marshal(m)
		if err != nil {
			level.Debug(t.Logger()).Log("msg", "marshal log line error", "err", err)
			continue
		}

		t.Client().Chan() <- api.Entry{
			Entry: logproto.Entry{
				Line:      string(b),
				Timestamp: timestamp(t.UseIncomingTimestamp(), message.Timestamp),
			},
			Labels: out,
		}
	}
}

const (
	defaultKafkaMessageKey  = "none"
	labelKeyKafkaMessageKey = "__meta_kafka_message_key"
)

func format(lbs labels.Labels, cfg []*relabel.Config) model.LabelSet {
	if len(lbs) == 0 {
		return nil
	}
	processed, _ := relabel.Process(lbs, cfg...)
	labelOut := model.LabelSet(util.LabelsToMetric(processed))
	for k := range labelOut {
		if strings.HasPrefix(string(k), "__") {
			delete(labelOut, k)
		}
	}
	return labelOut
}

func timestamp(useIncoming bool, incoming time.Time) time.Time {
	if useIncoming {
		return incoming
	}
	return time.Now()
}
