package cloudflare

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/pkg/errors"
)

// Railgun represents a Railgun's properties.
type Railgun struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	Status         string    `json:"status"`
	Enabled        bool      `json:"enabled"`
	ZonesConnected int       `json:"zones_connected"`
	Build          string    `json:"build"`
	Version        string    `json:"version"`
	Revision       string    `json:"revision"`
	ActivationKey  string    `json:"activation_key"`
	ActivatedOn    time.Time `json:"activated_on"`
	CreatedOn      time.Time `json:"created_on"`
	ModifiedOn     time.Time `json:"modified_on"`
	UpgradeInfo    struct {
		LatestVersion string `json:"latest_version"`
		DownloadLink  string `json:"download_link"`
	} `json:"upgrade_info"`
}

// RailgunListOptions represents the parameters used to list railguns.
type RailgunListOptions struct {
	Direction string
}

// railgunResponse represents the response from the Create Railgun and the Railgun Details endpoints.
type railgunResponse struct {
	Response
	Result Railgun `json:"result"`
}

// railgunsResponse represents the response from the List Railguns endpoint.
type railgunsResponse struct {
	Response
	Result []Railgun `json:"result"`
}

// CreateRailgun creates a new Railgun.
//
// API reference: https://api.cloudflare.com/#railgun-create-railgun
func (api *API) CreateRailgun(ctx context.Context, name string) (Railgun, error) {
	uri := fmt.Sprintf("%s/railguns", api.userBaseURL(""))
	params := struct {
		Name string `json:"name"`
	}{
		Name: name,
	}
	res, err := api.makeRequestContext(ctx, http.MethodPost, uri, params)
	if err != nil {
		return Railgun{}, err
	}
	var r railgunResponse
	if err := json.Unmarshal(res, &r); err != nil {
		return Railgun{}, errors.Wrap(err, errUnmarshalError)
	}
	return r.Result, nil
}

// ListRailguns lists Railguns connected to an account.
//
// API reference: https://api.cloudflare.com/#railgun-list-railguns
func (api *API) ListRailguns(ctx context.Context, options RailgunListOptions) ([]Railgun, error) {
	v := url.Values{}
	if options.Direction != "" {
		v.Set("direction", options.Direction)
	}
	uri := fmt.Sprintf("%s/railguns?%s", api.userBaseURL(""), v.Encode())
	res, err := api.makeRequestContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return nil, err
	}
	var r railgunsResponse
	if err := json.Unmarshal(res, &r); err != nil {
		return nil, errors.Wrap(err, errUnmarshalError)
	}
	return r.Result, nil
}

// RailgunDetails returns the details for a Railgun.
//
// API reference: https://api.cloudflare.com/#railgun-railgun-details
func (api *API) RailgunDetails(ctx context.Context, railgunID string) (Railgun, error) {
	uri := fmt.Sprintf("%s/railguns/%s", api.userBaseURL(""), railgunID)
	res, err := api.makeRequestContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return Railgun{}, err
	}
	var r railgunResponse
	if err := json.Unmarshal(res, &r); err != nil {
		return Railgun{}, errors.Wrap(err, errUnmarshalError)
	}
	return r.Result, nil
}

// RailgunZones returns the zones that are currently using a Railgun.
//
// API reference: https://api.cloudflare.com/#railgun-get-zones-connected-to-a-railgun
func (api *API) RailgunZones(ctx context.Context, railgunID string) ([]Zone, error) {
	uri := fmt.Sprintf("%s/railguns/%s/zones", api.userBaseURL(""), railgunID)
	res, err := api.makeRequestContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return nil, err
	}
	var r ZonesResponse
	if err := json.Unmarshal(res, &r); err != nil {
		return nil, errors.Wrap(err, errUnmarshalError)
	}
	return r.Result, nil
}

// enableRailgun enables (true) or disables (false) a Railgun for all zones connected to it.
//
// API reference: https://api.cloudflare.com/#railgun-enable-or-disable-a-railgun
func (api *API) enableRailgun(ctx context.Context, railgunID string, enable bool) (Railgun, error) {
	uri := fmt.Sprintf("%s/railguns/%s", api.userBaseURL(""), railgunID)
	params := struct {
		Enabled bool `json:"enabled"`
	}{
		Enabled: enable,
	}
	res, err := api.makeRequestContext(ctx, http.MethodPatch, uri, params)
	if err != nil {
		return Railgun{}, err
	}
	var r railgunResponse
	if err := json.Unmarshal(res, &r); err != nil {
		return Railgun{}, errors.Wrap(err, errUnmarshalError)
	}
	return r.Result, nil
}

// EnableRailgun enables a Railgun for all zones connected to it.
//
// API reference: https://api.cloudflare.com/#railgun-enable-or-disable-a-railgun
func (api *API) EnableRailgun(ctx context.Context, railgunID string) (Railgun, error) {
	return api.enableRailgun(ctx, railgunID, true)
}

// DisableRailgun enables a Railgun for all zones connected to it.
//
// API reference: https://api.cloudflare.com/#railgun-enable-or-disable-a-railgun
func (api *API) DisableRailgun(ctx context.Context, railgunID string) (Railgun, error) {
	return api.enableRailgun(ctx, railgunID, false)
}

// DeleteRailgun disables and deletes a Railgun.
//
// API reference: https://api.cloudflare.com/#railgun-delete-railgun
func (api *API) DeleteRailgun(ctx context.Context, railgunID string) error {
	uri := fmt.Sprintf("%s/railguns/%s", api.userBaseURL(""), railgunID)
	if _, err := api.makeRequestContext(ctx, http.MethodDelete, uri, nil); err != nil {
		return err
	}
	return nil
}

// ZoneRailgun represents the status of a Railgun on a zone.
type ZoneRailgun struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Enabled   bool   `json:"enabled"`
	Connected bool   `json:"connected"`
}

// zoneRailgunResponse represents the response from the Zone Railgun Details endpoint.
type zoneRailgunResponse struct {
	Response
	Result ZoneRailgun `json:"result"`
}

// zoneRailgunsResponse represents the response from the Zone Railgun endpoint.
type zoneRailgunsResponse struct {
	Response
	Result []ZoneRailgun `json:"result"`
}

// RailgunDiagnosis represents the test results from testing railgun connections
// to a zone.
type RailgunDiagnosis struct {
	Method          string `json:"method"`
	HostName        string `json:"host_name"`
	HTTPStatus      int    `json:"http_status"`
	Railgun         string `json:"railgun"`
	URL             string `json:"url"`
	ResponseStatus  string `json:"response_status"`
	Protocol        string `json:"protocol"`
	ElapsedTime     string `json:"elapsed_time"`
	BodySize        string `json:"body_size"`
	BodyHash        string `json:"body_hash"`
	MissingHeaders  string `json:"missing_headers"`
	ConnectionClose bool   `json:"connection_close"`
	Cloudflare      string `json:"cloudflare"`
	CFRay           string `json:"cf-ray"`
	// NOTE: Cloudflare's online API documentation does not yet have definitions
	// for the following fields. See: https://api.cloudflare.com/#railgun-connections-for-a-zone-test-railgun-connection/
	CFWANError    string `json:"cf-wan-error"`
	CFCacheStatus string `json:"cf-cache-status"`
}

// railgunDiagnosisResponse represents the response from the Test Railgun Connection endpoint.
type railgunDiagnosisResponse struct {
	Response
	Result RailgunDiagnosis `json:"result"`
}

// ZoneRailguns returns the available Railguns for a zone.
//
// API reference: https://api.cloudflare.com/#railguns-for-a-zone-get-available-railguns
func (api *API) ZoneRailguns(ctx context.Context, zoneID string) ([]ZoneRailgun, error) {
	uri := fmt.Sprintf("/zones/%s/railguns", zoneID)
	res, err := api.makeRequestContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return nil, err
	}
	var r zoneRailgunsResponse
	if err := json.Unmarshal(res, &r); err != nil {
		return nil, errors.Wrap(err, errUnmarshalError)
	}
	return r.Result, nil
}

// ZoneRailgunDetails returns the configuration for a given Railgun.
//
// API reference: https://api.cloudflare.com/#railguns-for-a-zone-get-railgun-details
func (api *API) ZoneRailgunDetails(ctx context.Context, zoneID, railgunID string) (ZoneRailgun, error) {
	uri := fmt.Sprintf("/zones/%s/railguns/%s", zoneID, railgunID)
	res, err := api.makeRequestContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return ZoneRailgun{}, err
	}
	var r zoneRailgunResponse
	if err := json.Unmarshal(res, &r); err != nil {
		return ZoneRailgun{}, errors.Wrap(err, errUnmarshalError)
	}
	return r.Result, nil
}

// TestRailgunConnection tests a Railgun connection for a given zone.
//
// API reference: https://api.cloudflare.com/#railgun-connections-for-a-zone-test-railgun-connection
func (api *API) TestRailgunConnection(ctx context.Context, zoneID, railgunID string) (RailgunDiagnosis, error) {
	uri := fmt.Sprintf("/zones/%s/railguns/%s/diagnose", zoneID, railgunID)
	res, err := api.makeRequestContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return RailgunDiagnosis{}, err
	}
	var r railgunDiagnosisResponse
	if err := json.Unmarshal(res, &r); err != nil {
		return RailgunDiagnosis{}, errors.Wrap(err, errUnmarshalError)
	}
	return r.Result, nil
}

// connectZoneRailgun connects (true) or disconnects (false) a Railgun for a given zone.
//
// API reference: https://api.cloudflare.com/#railguns-for-a-zone-connect-or-disconnect-a-railgun
func (api *API) connectZoneRailgun(ctx context.Context, zoneID, railgunID string, connect bool) (ZoneRailgun, error) {
	uri := fmt.Sprintf("/zones/%s/railguns/%s", zoneID, railgunID)
	params := struct {
		Connected bool `json:"connected"`
	}{
		Connected: connect,
	}
	res, err := api.makeRequestContext(ctx, http.MethodPatch, uri, params)
	if err != nil {
		return ZoneRailgun{}, err
	}
	var r zoneRailgunResponse
	if err := json.Unmarshal(res, &r); err != nil {
		return ZoneRailgun{}, errors.Wrap(err, errUnmarshalError)
	}
	return r.Result, nil
}

// ConnectZoneRailgun connects a Railgun for a given zone.
//
// API reference: https://api.cloudflare.com/#railguns-for-a-zone-connect-or-disconnect-a-railgun
func (api *API) ConnectZoneRailgun(ctx context.Context, zoneID, railgunID string) (ZoneRailgun, error) {
	return api.connectZoneRailgun(ctx, zoneID, railgunID, true)
}

// DisconnectZoneRailgun disconnects a Railgun for a given zone.
//
// API reference: https://api.cloudflare.com/#railguns-for-a-zone-connect-or-disconnect-a-railgun
func (api *API) DisconnectZoneRailgun(ctx context.Context, zoneID, railgunID string) (ZoneRailgun, error) {
	return api.connectZoneRailgun(ctx, zoneID, railgunID, false)
}
