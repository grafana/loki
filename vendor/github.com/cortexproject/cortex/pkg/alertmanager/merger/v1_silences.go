package merger

import (
	"encoding/json"
	"fmt"

	v2_models "github.com/prometheus/alertmanager/api/v2/models"
)

// V1Silences implements the Merger interface for GET /v1/silences. Unlike for alerts, the API
// definitions for silences are almost identical between v1 and v2. The differences are that the
// fields in the JSON output are ordered differently, and the timestamps have more precision in v1,
// but these differences should not be problematic to clients. Therefore, the implementation
// re-uses the v2 types, with additional handling for the enclosing status/data fields.
type V1Silences struct{}

func (V1Silences) MergeResponses(in [][]byte) ([]byte, error) {
	type bodyType struct {
		Status string                     `json:"status"`
		Data   v2_models.GettableSilences `json:"data"`
	}

	silences := make(v2_models.GettableSilences, 0)
	for _, body := range in {
		parsed := bodyType{}
		if err := json.Unmarshal(body, &parsed); err != nil {
			return nil, err
		}
		if parsed.Status != statusSuccess {
			return nil, fmt.Errorf("unable to merge response of status: %s", parsed.Status)
		}
		silences = append(silences, parsed.Data...)
	}

	merged, err := mergeV2Silences(silences)
	if err != nil {
		return nil, err
	}
	body := bodyType{
		Status: statusSuccess,
		Data:   merged,
	}

	return json.Marshal(body)
}
