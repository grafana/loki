package azurelog

import (
	"bytes"
	"encoding/json"
	"github.com/Shopify/sarama"
)

type eventHubMessage struct {
	Records []json.RawMessage `json:"records"`
}

func messageParser(message *sarama.ConsumerMessage) ([]string, error) {
	// fix json as mentioned here:
	// https://learn.microsoft.com/en-us/answers/questions/1001797/invalid-json-logs-produced-for-function-apps?fbclid=IwAR3pK8Nj60GFBtKemqwfpiZyf3rerjowPH_j_qIuNrw_uLDesYvC4mTkfgs
	body := bytes.ReplaceAll(message.Value, []byte(`'`), []byte(`"`))

	data := &eventHubMessage{}
	err := json.Unmarshal(body, data)
	if err != nil {
		return nil, err
	}

	result := make([]string, 0, len(data.Records))
	for _, m := range data.Records {
		result = append(result, string(m))
	}

	return result, nil
}
