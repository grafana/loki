package cloudflare

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/pkg/errors"
)

// WorkersPutSecretRequest provides parameters for creating and updating secrets
type WorkersPutSecretRequest struct {
	Name string            `json:"name"`
	Text string            `json:"text"`
	Type WorkerBindingType `json:"type"`
}

// WorkersSecret contains the name and type of the secret
type WorkersSecret struct {
	Name string `json:"name"`
	Type string `json:"secret_text"`
}

// WorkersPutSecretResponse is the response received when creating or updating a secret
type WorkersPutSecretResponse struct {
	Response
	Result WorkersSecret `json:"result"`
}

// WorkersListSecretsResponse is the response received when listing secrets
type WorkersListSecretsResponse struct {
	Response
	Result []WorkersSecret `json:"result"`
}

// SetWorkersSecret creates or updates a secret
// API reference: https://api.cloudflare.com/
func (api *API) SetWorkersSecret(ctx context.Context, script string, req *WorkersPutSecretRequest) (WorkersPutSecretResponse, error) {
	uri := fmt.Sprintf("/accounts/%s/workers/scripts/%s/secrets", api.AccountID, script)
	res, err := api.makeRequestContext(ctx, http.MethodPut, uri, req)
	if err != nil {
		return WorkersPutSecretResponse{}, err
	}

	result := WorkersPutSecretResponse{}
	if err := json.Unmarshal(res, &result); err != nil {
		return result, errors.Wrap(err, errUnmarshalError)
	}

	return result, err
}

// DeleteWorkersSecret deletes a secret
// API reference: https://api.cloudflare.com/
func (api *API) DeleteWorkersSecret(ctx context.Context, script, secretName string) (Response, error) {
	uri := fmt.Sprintf("/accounts/%s/workers/scripts/%s/secrets/%s", api.AccountID, script, secretName)
	res, err := api.makeRequestContext(ctx, http.MethodDelete, uri, nil)
	if err != nil {
		return Response{}, err
	}

	result := Response{}
	if err := json.Unmarshal(res, &result); err != nil {
		return result, errors.Wrap(err, errUnmarshalError)
	}

	return result, err
}

// ListWorkersSecrets lists secrets for a given worker
// API reference: https://api.cloudflare.com/
func (api *API) ListWorkersSecrets(ctx context.Context, script string) (WorkersListSecretsResponse, error) {
	uri := fmt.Sprintf("/accounts/%s/workers/scripts/%s/secrets", api.AccountID, script)
	res, err := api.makeRequestContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return WorkersListSecretsResponse{}, err
	}

	result := WorkersListSecretsResponse{}
	if err := json.Unmarshal(res, &result); err != nil {
		return result, errors.Wrap(err, errUnmarshalError)
	}

	return result, err
}
