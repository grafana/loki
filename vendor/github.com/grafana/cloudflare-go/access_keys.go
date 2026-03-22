package cloudflare

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/pkg/errors"
	"net/http"
	"time"
)

type AccessKeysConfig struct {
	KeyRotationIntervalDays int       `json:"key_rotation_interval_days"`
	LastKeyRotationAt       time.Time `json:"last_key_rotation_at"`
	DaysUntilNextRotation   int       `json:"days_until_next_rotation"`
}

type AccessKeysConfigUpdateRequest struct {
	KeyRotationIntervalDays int `json:"key_rotation_interval_days"`
}

type accessKeysConfigResponse struct {
	Response
	Result AccessKeysConfig `json:"result"`
}

// AccessKeysConfig returns the Access Keys Configuration for an account.
//
// API reference: https://api.cloudflare.com/#access-keys-configuration-get-access-keys-configuration
func (api *API) AccessKeysConfig(ctx context.Context, accountID string) (AccessKeysConfig, error) {
	uri := fmt.Sprintf("/%s/%s/access/keys", AccountRouteRoot, accountID)

	return api.accessKeysRequest(ctx, http.MethodGet, uri, nil)
}

// UpdateAccessKeysConfig updates the Access Keys Configuration for an account.
//
// API reference: https://api.cloudflare.com/#access-keys-configuration-update-access-keys-configuration
func (api *API) UpdateAccessKeysConfig(ctx context.Context, accountID string, request AccessKeysConfigUpdateRequest) (AccessKeysConfig, error) {
	uri := fmt.Sprintf("/%s/%s/access/keys", AccountRouteRoot, accountID)

	return api.accessKeysRequest(ctx, http.MethodPut, uri, request)
}

// RotateAccessKeys rotates the Access Keys for an account and returns the updated Access Keys Configuration
//
// API reference: https://api.cloudflare.com/#access-keys-configuration-rotate-access-keys
func (api *API) RotateAccessKeys(ctx context.Context, accountID string) (AccessKeysConfig, error) {
	uri := fmt.Sprintf("/%s/%s/access/keys/rotate", AccountRouteRoot, accountID)
	return api.accessKeysRequest(ctx, http.MethodPost, uri, nil)
}

func (api *API) accessKeysRequest(ctx context.Context, method, uri string, params interface{}) (AccessKeysConfig, error) {
	res, err := api.makeRequestContext(ctx, method, uri, params)
	if err != nil {
		return AccessKeysConfig{}, err
	}

	var keysConfigResponse accessKeysConfigResponse
	if err := json.Unmarshal(res, &keysConfigResponse); err != nil {
		return AccessKeysConfig{}, errors.Wrap(err, errUnmarshalError)
	}
	return keysConfigResponse.Result, nil
}
