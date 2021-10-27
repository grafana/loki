package merger

import (
	"errors"
	"time"

	"github.com/go-openapi/swag"
	v2 "github.com/prometheus/alertmanager/api/v2"
	v2_models "github.com/prometheus/alertmanager/api/v2/models"
)

// V2Silences implements the Merger interface for GET /v2/silences. It returns the union of silences
// over all the responses. When a silence with the same ID exists in multiple responses, the silence
// most recently updated silence is returned (newest UpdatedAt timestamp).
type V2Silences struct{}

func (V2Silences) MergeResponses(in [][]byte) ([]byte, error) {
	silences := make(v2_models.GettableSilences, 0)
	for _, body := range in {
		parsed := make(v2_models.GettableSilences, 0)
		if err := swag.ReadJSON(body, &parsed); err != nil {
			return nil, err
		}
		silences = append(silences, parsed...)
	}

	merged, err := mergeV2Silences(silences)
	if err != nil {
		return nil, err
	}

	return swag.WriteJSON(merged)
}

func mergeV2Silences(in v2_models.GettableSilences) (v2_models.GettableSilences, error) {
	// Select the most recently updated silences for each silence ID.
	silences := make(map[string]*v2_models.GettableSilence)
	for _, silence := range in {
		if silence.ID == nil {
			return nil, errors.New("unexpected nil id")
		}
		if silence.UpdatedAt == nil {
			return nil, errors.New("unexpected nil updatedAt")
		}

		key := *silence.ID
		if current, ok := silences[key]; ok {
			if time.Time(*silence.UpdatedAt).After(time.Time(*current.UpdatedAt)) {
				silences[key] = silence
			}
		} else {
			silences[key] = silence
		}
	}

	result := make(v2_models.GettableSilences, 0, len(silences))
	for _, silence := range silences {
		result = append(result, silence)
	}

	// Re-use Alertmanager sorting for silences.
	v2.SortSilences(result)

	return result, nil
}
