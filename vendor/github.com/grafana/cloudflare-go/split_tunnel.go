package cloudflare

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/pkg/errors"
)

// SplitTunnelResponse represents the response from the get split
// tunnel endpoints.
type SplitTunnelResponse struct {
	Response
	Result []SplitTunnel `json:"result"`
}

// SplitTunnel represents the individual tunnel struct.
type SplitTunnel struct {
	Address     string `json:"address,omitempty"`
	Host        string `json:"host,omitempty"`
	Description string `json:"description,omitempty"`
}

// ListSplitTunnel returns all include or exclude split tunnel  within an account.
//
// API reference for include: https://api.cloudflare.com/#device-policy-get-split-tunnel-include-list
// API reference for exclude: https://api.cloudflare.com/#device-policy-get-split-tunnel-exclude-list
func (api *API) ListSplitTunnels(ctx context.Context, accountID string, mode string) ([]SplitTunnel, error) {
	uri := fmt.Sprintf("/%s/%s/devices/policy/%s", AccountRouteRoot, accountID, mode)

	res, err := api.makeRequestContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return []SplitTunnel{}, err
	}

	var splitTunnelResponse SplitTunnelResponse
	err = json.Unmarshal(res, &splitTunnelResponse)
	if err != nil {
		return []SplitTunnel{}, errors.Wrap(err, errUnmarshalError)
	}

	return splitTunnelResponse.Result, nil
}

// UpdateSplitTunnel updates the existing split tunnel policy.
//
// API reference for include: https://api.cloudflare.com/#device-policy-set-split-tunnel-include-list
// API reference for exclude: https://api.cloudflare.com/#device-policy-set-split-tunnel-exclude-list
func (api *API) UpdateSplitTunnel(ctx context.Context, accountID string, mode string, tunnels []SplitTunnel) ([]SplitTunnel, error) {
	uri := fmt.Sprintf("/%s/%s/devices/policy/%s", AccountRouteRoot, accountID, mode)

	res, err := api.makeRequestContext(ctx, http.MethodPut, uri, tunnels)
	if err != nil {
		return []SplitTunnel{}, err
	}

	var splitTunnelResponse SplitTunnelResponse
	err = json.Unmarshal(res, &splitTunnelResponse)
	if err != nil {
		return []SplitTunnel{}, errors.Wrap(err, errUnmarshalError)
	}

	return splitTunnelResponse.Result, nil
}
