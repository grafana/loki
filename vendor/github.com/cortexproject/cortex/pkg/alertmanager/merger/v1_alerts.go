package merger

import (
	"encoding/json"
	"fmt"
	"sort"

	v1 "github.com/prometheus/alertmanager/api/v1"
)

const (
	statusSuccess = "success"
)

// V1Alerts implements the Merger interface for GET /v1/alerts. It returns the union of alerts over
// all the responses. When the same alert exists in multiple responses, the alert instance in the
// earliest response is returned in the final response. We cannot use the UpdatedAt timestamp as
// for V2Alerts, because the v1 API does not provide it.
type V1Alerts struct{}

func (V1Alerts) MergeResponses(in [][]byte) ([]byte, error) {
	type bodyType struct {
		Status string      `json:"status"`
		Data   []*v1.Alert `json:"data"`
	}

	alerts := make([]*v1.Alert, 0)
	for _, body := range in {
		parsed := bodyType{}
		if err := json.Unmarshal(body, &parsed); err != nil {
			return nil, err
		}
		if parsed.Status != statusSuccess {
			return nil, fmt.Errorf("unable to merge response of status: %s", parsed.Status)
		}
		alerts = append(alerts, parsed.Data...)
	}

	merged, err := mergeV1Alerts(alerts)
	if err != nil {
		return nil, err
	}
	body := bodyType{
		Status: statusSuccess,
		Data:   merged,
	}

	return json.Marshal(body)
}

func mergeV1Alerts(in []*v1.Alert) ([]*v1.Alert, error) {
	// Select an arbitrary alert for each distinct alert.
	alerts := make(map[string]*v1.Alert)
	for _, alert := range in {
		key := alert.Fingerprint
		if _, ok := alerts[key]; !ok {
			alerts[key] = alert
		}
	}

	result := make([]*v1.Alert, 0, len(alerts))
	for _, alert := range alerts {
		result = append(result, alert)
	}

	// Mimic Alertmanager which returns alerts ordered by fingerprint (as string).
	sort.Slice(result, func(i, j int) bool {
		return result[i].Fingerprint < result[j].Fingerprint
	})

	return result, nil
}
