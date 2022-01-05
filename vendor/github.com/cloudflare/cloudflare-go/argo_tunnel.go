package cloudflare

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/pkg/errors"
)

// ArgoTunnel is the struct definition of a tunnel.
type ArgoTunnel struct {
	ID          string                 `json:"id,omitempty"`
	Name        string                 `json:"name,omitempty"`
	Secret      string                 `json:"tunnel_secret,omitempty"`
	CreatedAt   *time.Time             `json:"created_at,omitempty"`
	DeletedAt   *time.Time             `json:"deleted_at,omitempty"`
	Connections []ArgoTunnelConnection `json:"connections,omitempty"`
}

// ArgoTunnelConnection represents the connections associated with a tunnel.
type ArgoTunnelConnection struct {
	ColoName           string `json:"colo_name"`
	UUID               string `json:"uuid"`
	IsPendingReconnect bool   `json:"is_pending_reconnect"`
}

// ArgoTunnelsDetailResponse is used for representing the API response payload for
// multiple tunnels.
type ArgoTunnelsDetailResponse struct {
	Result []ArgoTunnel `json:"result"`
	Response
}

// ArgoTunnelDetailResponse is used for representing the API response payload for
// a single tunnel.
type ArgoTunnelDetailResponse struct {
	Result ArgoTunnel `json:"result"`
	Response
}

// ArgoTunnels lists all tunnels.
//
// API reference: https://api.cloudflare.com/#argo-tunnel-list-argo-tunnels
func (api *API) ArgoTunnels(ctx context.Context, accountID string) ([]ArgoTunnel, error) {
	uri := fmt.Sprintf("/accounts/%s/tunnels", accountID)

	res, err := api.makeRequestContextWithHeaders(ctx, http.MethodGet, uri, nil, argoV1Header())
	if err != nil {
		return []ArgoTunnel{}, err
	}

	var argoDetailsResponse ArgoTunnelsDetailResponse
	err = json.Unmarshal(res, &argoDetailsResponse)
	if err != nil {
		return []ArgoTunnel{}, errors.Wrap(err, errUnmarshalError)
	}
	return argoDetailsResponse.Result, nil
}

// ArgoTunnel returns a single Argo tunnel.
//
// API reference: https://api.cloudflare.com/#argo-tunnel-get-argo-tunnel
func (api *API) ArgoTunnel(ctx context.Context, accountID, tunnelUUID string) (ArgoTunnel, error) {
	uri := fmt.Sprintf("/accounts/%s/tunnels/%s", accountID, tunnelUUID)

	res, err := api.makeRequestContextWithHeaders(ctx, http.MethodGet, uri, nil, argoV1Header())
	if err != nil {
		return ArgoTunnel{}, err
	}

	var argoDetailsResponse ArgoTunnelDetailResponse
	err = json.Unmarshal(res, &argoDetailsResponse)
	if err != nil {
		return ArgoTunnel{}, errors.Wrap(err, errUnmarshalError)
	}
	return argoDetailsResponse.Result, nil
}

// CreateArgoTunnel creates a new tunnel for the account.
//
// API reference: https://api.cloudflare.com/#argo-tunnel-create-argo-tunnel
func (api *API) CreateArgoTunnel(ctx context.Context, accountID, name, secret string) (ArgoTunnel, error) {
	uri := fmt.Sprintf("/accounts/%s/tunnels", accountID)

	tunnel := ArgoTunnel{Name: name, Secret: secret}

	res, err := api.makeRequestContextWithHeaders(ctx, http.MethodPost, uri, tunnel, argoV1Header())
	if err != nil {
		return ArgoTunnel{}, err
	}

	var argoDetailsResponse ArgoTunnelDetailResponse
	err = json.Unmarshal(res, &argoDetailsResponse)
	if err != nil {
		return ArgoTunnel{}, errors.Wrap(err, errUnmarshalError)
	}

	return argoDetailsResponse.Result, nil
}

// DeleteArgoTunnel removes a single Argo tunnel.
//
// API reference: https://api.cloudflare.com/#argo-tunnel-delete-argo-tunnel
func (api *API) DeleteArgoTunnel(ctx context.Context, accountID, tunnelUUID string) error {
	uri := fmt.Sprintf("/accounts/%s/tunnels/%s", accountID, tunnelUUID)

	res, err := api.makeRequestContextWithHeaders(ctx, http.MethodDelete, uri, nil, argoV1Header())
	if err != nil {
		return err
	}

	var argoDetailsResponse ArgoTunnelDetailResponse
	err = json.Unmarshal(res, &argoDetailsResponse)
	if err != nil {
		return errors.Wrap(err, errUnmarshalError)
	}

	return nil
}

// CleanupArgoTunnelConnections deletes any inactive connections on a tunnel.
//
// API reference: https://api.cloudflare.com/#argo-tunnel-clean-up-argo-tunnel-connections
func (api *API) CleanupArgoTunnelConnections(ctx context.Context, accountID, tunnelUUID string) error {
	uri := fmt.Sprintf("/accounts/%s/tunnels/%s/connections", accountID, tunnelUUID)

	res, err := api.makeRequestContextWithHeaders(ctx, http.MethodDelete, uri, nil, argoV1Header())
	if err != nil {
		return err
	}

	var argoDetailsResponse ArgoTunnelDetailResponse
	err = json.Unmarshal(res, &argoDetailsResponse)
	if err != nil {
		return errors.Wrap(err, errUnmarshalError)
	}

	return nil
}

// The early implementation of Argo Tunnel endpoints didn't conform to the V4
// API standard response structure. This has been remedied going forward however
// to support older clients this isn't yet the default. An explicit `Accept`
// header is used to get the V4 compatible version.
func argoV1Header() http.Header {
	header := make(http.Header)
	header.Set("Accept", "application/json;version=1")

	return header
}
