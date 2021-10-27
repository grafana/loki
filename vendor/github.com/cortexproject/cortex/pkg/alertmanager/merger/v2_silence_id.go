package merger

import (
	"errors"

	"github.com/go-openapi/swag"
	v2_models "github.com/prometheus/alertmanager/api/v2/models"
)

// V2SilenceID implements the Merger interface for GET /v2/silence/{id}. It returns the most
// recently updated silence (newest UpdatedAt timestamp).
type V2SilenceID struct{}

func (V2SilenceID) MergeResponses(in [][]byte) ([]byte, error) {
	silences := make(v2_models.GettableSilences, 0)
	for _, body := range in {
		parsed := &v2_models.GettableSilence{}
		if err := swag.ReadJSON(body, parsed); err != nil {
			return nil, err
		}
		silences = append(silences, parsed)
	}

	merged, err := mergeV2Silences(silences)
	if err != nil {
		return nil, err
	}

	if len(merged) != 1 {
		return nil, errors.New("unexpected mismatched silence ids")
	}

	return swag.WriteJSON(merged[0])
}
