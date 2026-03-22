package cloudflare

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/pkg/errors"
)

// APIToken is the full API token.
type APIToken struct {
	ID         string             `json:"id,omitempty"`
	Name       string             `json:"name"`
	Status     string             `json:"status,omitempty"`
	IssuedOn   *time.Time         `json:"issued_on,omitempty"`
	ModifiedOn *time.Time         `json:"modified_on,omitempty"`
	NotBefore  *time.Time         `json:"not_before,omitempty"`
	ExpiresOn  *time.Time         `json:"expires_on,omitempty"`
	Policies   []APITokenPolicies `json:"policies"`
	Condition  *APITokenCondition `json:"condition,omitempty"`
	Value      string             `json:"value,omitempty"`
}

// APITokenPermissionGroups is the permission groups associated with API tokens.
type APITokenPermissionGroups struct {
	ID     string   `json:"id"`
	Name   string   `json:"name,omitempty"`
	Scopes []string `json:"scopes,omitempty"`
}

// APITokenPolicies are policies attached to an API token.
type APITokenPolicies struct {
	ID               string                     `json:"id,omitempty"`
	Effect           string                     `json:"effect"`
	Resources        map[string]interface{}     `json:"resources"`
	PermissionGroups []APITokenPermissionGroups `json:"permission_groups"`
}

// APITokenRequestIPCondition is the struct for adding an IP restriction to an
// API token.
type APITokenRequestIPCondition struct {
	In    []string `json:"in,omitempty"`
	NotIn []string `json:"not_in,omitempty"`
}

// APITokenCondition is the outer structure for request conditions (currently
// only IPs).
type APITokenCondition struct {
	RequestIP *APITokenRequestIPCondition `json:"request.ip,omitempty"`
}

// APITokenResponse is the API response for a single API token.
type APITokenResponse struct {
	Response
	Result APIToken `json:"result"`
}

// APITokenListResponse is the API response for multiple API tokens.
type APITokenListResponse struct {
	Response
	Result []APIToken `json:"result"`
}

// APITokenRollResponse is the API response when rolling the token.
type APITokenRollResponse struct {
	Response
	Result string `json:"result"`
}

// APITokenVerifyResponse is the API response for verifying a token.
type APITokenVerifyResponse struct {
	Response
	Result APITokenVerifyBody `json:"result"`
}

// APITokenPermissionGroupsResponse is the API response for the available
// permission groups.
type APITokenPermissionGroupsResponse struct {
	Response
	Result []APITokenPermissionGroups `json:"result"`
}

// APITokenVerifyBody is the API body for verifying a token.
type APITokenVerifyBody struct {
	ID        string    `json:"id"`
	Status    string    `json:"status"`
	NotBefore time.Time `json:"not_before"`
	ExpiresOn time.Time `json:"expires_on"`
}

// GetAPIToken returns a single API token based on the ID.
//
// API reference: https://api.cloudflare.com/#user-api-tokens-token-details
func (api *API) GetAPIToken(ctx context.Context, tokenID string) (APIToken, error) {
	uri := fmt.Sprintf("/user/tokens/%s", tokenID)

	res, err := api.makeRequestContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return APIToken{}, err
	}

	var apiTokenResponse APITokenResponse
	err = json.Unmarshal(res, &apiTokenResponse)
	if err != nil {
		return APIToken{}, errors.Wrap(err, errUnmarshalError)
	}

	return apiTokenResponse.Result, nil
}

// APITokens returns all available API tokens.
//
// API reference: https://api.cloudflare.com/#user-api-tokens-list-tokens
func (api *API) APITokens(ctx context.Context) ([]APIToken, error) {
	res, err := api.makeRequestContext(ctx, http.MethodGet, "/user/tokens", nil)
	if err != nil {
		return []APIToken{}, err
	}

	var apiTokenListResponse APITokenListResponse
	err = json.Unmarshal(res, &apiTokenListResponse)
	if err != nil {
		return []APIToken{}, errors.Wrap(err, errUnmarshalError)
	}

	return apiTokenListResponse.Result, nil
}

// CreateAPIToken creates a new token. Returns the API token that has been
// generated.
//
// The token value itself is only shown once (post create) and will present as
// `Value` from this method. If you fail to capture it at this point, you will
// need to roll the token in order to get a new value.
//
// API reference: https://api.cloudflare.com/#user-api-tokens-create-token
func (api *API) CreateAPIToken(ctx context.Context, token APIToken) (APIToken, error) {
	res, err := api.makeRequestContext(ctx, http.MethodPost, "/user/tokens", token)
	if err != nil {
		return APIToken{}, err
	}

	var createTokenAPIResponse APITokenResponse
	err = json.Unmarshal(res, &createTokenAPIResponse)
	if err != nil {
		return APIToken{}, errors.Wrap(err, errUnmarshalError)
	}

	return createTokenAPIResponse.Result, nil
}

// UpdateAPIToken updates an existing API token.
//
// API reference: https://api.cloudflare.com/#user-api-tokens-update-token
func (api *API) UpdateAPIToken(ctx context.Context, tokenID string, token APIToken) (APIToken, error) {
	res, err := api.makeRequestContext(ctx, http.MethodPut, "/user/tokens/"+tokenID, token)
	if err != nil {
		return APIToken{}, err
	}

	var updatedTokenResponse APITokenResponse
	err = json.Unmarshal(res, &updatedTokenResponse)
	if err != nil {
		return APIToken{}, errors.Wrap(err, errUnmarshalError)
	}

	return updatedTokenResponse.Result, nil
}

// RollAPIToken rolls the credential associated with the token.
//
// API reference: https://api.cloudflare.com/#user-api-tokens-roll-token
func (api *API) RollAPIToken(ctx context.Context, tokenID string) (string, error) {
	uri := fmt.Sprintf("/user/tokens/%s/value", tokenID)

	res, err := api.makeRequestContext(ctx, http.MethodPut, uri, nil)
	if err != nil {
		return "", err
	}

	var apiTokenRollResponse APITokenRollResponse
	err = json.Unmarshal(res, &apiTokenRollResponse)
	if err != nil {
		return "", errors.Wrap(err, errUnmarshalError)
	}

	return apiTokenRollResponse.Result, nil
}

// VerifyAPIToken tests the validity of the token.
//
// API reference: https://api.cloudflare.com/#user-api-tokens-verify-token
func (api *API) VerifyAPIToken(ctx context.Context) (APITokenVerifyBody, error) {
	res, err := api.makeRequestContext(ctx, http.MethodGet, "/user/tokens/verify", nil)
	if err != nil {
		return APITokenVerifyBody{}, err
	}

	var apiTokenVerifyResponse APITokenVerifyResponse
	err = json.Unmarshal(res, &apiTokenVerifyResponse)
	if err != nil {
		return APITokenVerifyBody{}, errors.Wrap(err, errUnmarshalError)
	}

	return apiTokenVerifyResponse.Result, nil
}

// DeleteAPIToken deletes a single API token.
//
// API reference: https://api.cloudflare.com/#user-api-tokens-delete-token
func (api *API) DeleteAPIToken(ctx context.Context, tokenID string) error {
	_, err := api.makeRequestContext(ctx, http.MethodDelete, "/user/tokens/"+tokenID, nil)
	if err != nil {
		return err
	}

	return nil
}

// ListAPITokensPermissionGroups returns all available API token permission groups.
//
// API reference: https://api.cloudflare.com/#permission-groups-list-permission-groups
func (api *API) ListAPITokensPermissionGroups(ctx context.Context) ([]APITokenPermissionGroups, error) {
	var r APITokenPermissionGroupsResponse
	res, err := api.makeRequestContext(ctx, http.MethodGet, "/user/tokens/permission_groups", nil)
	if err != nil {
		return []APITokenPermissionGroups{}, err
	}

	err = json.Unmarshal(res, &r)
	if err != nil {
		return []APITokenPermissionGroups{}, errors.Wrap(err, errUnmarshalError)
	}

	return r.Result, nil
}
