package cloudflare

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/pkg/errors"
)

var validSettingValues = []string{"on", "off"}

// ArgoFeatureSetting is the structure of the API object for the
// argo smart routing and tiered caching settings.
type ArgoFeatureSetting struct {
	Editable   bool      `json:"editable,omitempty"`
	ID         string    `json:"id,omitempty"`
	ModifiedOn time.Time `json:"modified_on,omitempty"`
	Value      string    `json:"value"`
}

// ArgoDetailsResponse is the API response for the argo smart routing
// and tiered caching response.
type ArgoDetailsResponse struct {
	Result ArgoFeatureSetting `json:"result"`
	Response
}

// ArgoSmartRouting returns the current settings for smart routing.
//
// API reference: https://api.cloudflare.com/#argo-smart-routing-get-argo-smart-routing-setting
func (api *API) ArgoSmartRouting(ctx context.Context, zoneID string) (ArgoFeatureSetting, error) {
	uri := fmt.Sprintf("/zones/%s/argo/smart_routing", zoneID)

	res, err := api.makeRequestContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return ArgoFeatureSetting{}, err
	}

	var argoDetailsResponse ArgoDetailsResponse
	err = json.Unmarshal(res, &argoDetailsResponse)
	if err != nil {
		return ArgoFeatureSetting{}, errors.Wrap(err, errUnmarshalError)
	}
	return argoDetailsResponse.Result, nil
}

// UpdateArgoSmartRouting updates the setting for smart routing.
//
// API reference: https://api.cloudflare.com/#argo-smart-routing-patch-argo-smart-routing-setting
func (api *API) UpdateArgoSmartRouting(ctx context.Context, zoneID, settingValue string) (ArgoFeatureSetting, error) {
	if !contains(validSettingValues, settingValue) {
		return ArgoFeatureSetting{}, errors.New(fmt.Sprintf("invalid setting value '%s'. must be 'on' or 'off'", settingValue))
	}

	uri := fmt.Sprintf("/zones/%s/argo/smart_routing", zoneID)

	res, err := api.makeRequestContext(ctx, http.MethodPatch, uri, ArgoFeatureSetting{Value: settingValue})
	if err != nil {
		return ArgoFeatureSetting{}, err
	}

	var argoDetailsResponse ArgoDetailsResponse
	err = json.Unmarshal(res, &argoDetailsResponse)
	if err != nil {
		return ArgoFeatureSetting{}, errors.Wrap(err, errUnmarshalError)
	}
	return argoDetailsResponse.Result, nil
}

// ArgoTieredCaching returns the current settings for tiered caching.
//
// API reference: TBA
func (api *API) ArgoTieredCaching(ctx context.Context, zoneID string) (ArgoFeatureSetting, error) {
	uri := fmt.Sprintf("/zones/%s/argo/tiered_caching", zoneID)

	res, err := api.makeRequestContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return ArgoFeatureSetting{}, err
	}

	var argoDetailsResponse ArgoDetailsResponse
	err = json.Unmarshal(res, &argoDetailsResponse)
	if err != nil {
		return ArgoFeatureSetting{}, errors.Wrap(err, errUnmarshalError)
	}
	return argoDetailsResponse.Result, nil
}

// UpdateArgoTieredCaching updates the setting for tiered caching.
//
// API reference: TBA
func (api *API) UpdateArgoTieredCaching(ctx context.Context, zoneID, settingValue string) (ArgoFeatureSetting, error) {
	if !contains(validSettingValues, settingValue) {
		return ArgoFeatureSetting{}, errors.New(fmt.Sprintf("invalid setting value '%s'. must be 'on' or 'off'", settingValue))
	}

	uri := fmt.Sprintf("/zones/%s/argo/tiered_caching", zoneID)

	res, err := api.makeRequestContext(ctx, http.MethodPatch, uri, ArgoFeatureSetting{Value: settingValue})
	if err != nil {
		return ArgoFeatureSetting{}, err
	}

	var argoDetailsResponse ArgoDetailsResponse
	err = json.Unmarshal(res, &argoDetailsResponse)
	if err != nil {
		return ArgoFeatureSetting{}, errors.Wrap(err, errUnmarshalError)
	}
	return argoDetailsResponse.Result, nil
}

func contains(s []string, e string) bool {
	for _, a := range s {
		if a == e {
			return true
		}
	}
	return false
}
